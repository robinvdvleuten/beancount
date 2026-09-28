package query

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/internal/pydecimal"
	"github.com/robinvdvleuten/beancount/internal/pyrepr"
	"github.com/robinvdvleuten/beancount/query/bql"
	"github.com/shopspring/decimal"
)

// compileError represents a semantic error found while compiling a query.
type compileError struct {
	Pos     ast.Position
	Message string
}

func (e *compileError) Error() string {
	return e.Message
}

// GetPosition reports where in the query text the error is.
func (e *compileError) GetPosition() ast.Position {
	return e.Pos
}

func compileErrorf(node bql.Node, format string, args ...any) *compileError {
	return &compileError{Pos: node.Pos(), Message: fmt.Sprintf(format, args...)}
}

// cexpr is a compiled expression: a typed, evaluatable tree node.
type cexpr interface {
	typ() dtype
	eval(row *evalRow) any
}

// compiledSelect is a fully resolved SELECT ready for execution. GroupBy and
// OrderBy reference targets by index; hidden targets were appended
// during resolution and are not rendered.
type compiledSelect struct {
	Targets   []compiledTarget
	Where     cexpr
	From      *compiledFrom
	GroupBy   []int
	OrderBy   []int
	OrderDesc bool
	Limit     *int64
	Distinct  bool
	HasAgg    bool
	Aggs      []*cAgg
	// UsesBalance is set when the running balance column is referenced,
	// so the executor can skip per-row inventory snapshots otherwise.
	UsesBalance bool
}

// compiledTarget is one output column (or hidden sort/group key).
type compiledTarget struct {
	Name   string
	Type   dtype
	Hidden bool
	IsAgg  bool // the expression contains an aggregate function
	expr   cexpr
	key    string // canonical expression key for structural matching
}

// compile resolves and type-checks a parsed BQL statement against the query
// environments. PRINT compiles to its FROM clause; every other statement
// compiles as a SELECT, BALANCES and JOURNAL as the SELECT they desugar to.
func compile(qctx *Context, parsed bql.Statement) (statement, error) {
	c := &compiler{ctx: qctx}
	if p, ok := parsed.(*bql.Print); ok {
		compiled, err := c.compilePrint(p)
		if err != nil {
			return nil, err
		}
		return compiled, nil
	}
	compiled, err := c.compileSelect(desugar(parsed).(*bql.Select))
	if err != nil {
		return nil, err
	}
	return compiled, nil
}

type compiler struct {
	ctx         *Context
	env         *environment
	aggs        []*cAgg
	usesBalance bool
}

func (c *compiler) compileSelect(sel *bql.Select) (*compiledSelect, error) {
	compiled := &compiledSelect{Distinct: sel.Distinct, Limit: sel.Limit}

	from, err := c.compileFrom(sel.From)
	if err != nil {
		return nil, err
	}
	compiled.From = from

	c.env = targetsEnv

	// Targets: expand the wildcard or compile the explicit list.
	targets := sel.Targets
	if sel.Wildcard {
		targets = make([]bql.Target, len(wildcardColumns))
		for i, name := range wildcardColumns {
			targets[i] = bql.Target{Expr: &bql.Ident{Name: name}}
		}
	}
	allocated := make(map[string]bool, len(targets))
	for _, target := range targets {
		expr, err := c.compileExpr(target.Expr)
		if err != nil {
			return nil, err
		}
		name := target.As
		if name == "" {
			name = deriveName(target.Expr)
		}
		name = uniqueName(name, allocated)
		allocated[name] = true
		compiled.Targets = append(compiled.Targets, compiledTarget{
			Name:  name,
			Type:  expr.typ(),
			IsAgg: c.isAggregate(target.Expr),
			expr:  expr,
			key:   exprKey(target.Expr),
		})
	}
	// Like bean-query, check aggregate placement once every target compiled.
	for _, target := range targets {
		columns, aggs := c.columnsAndAggregates(target.Expr)
		if columns > 0 && len(aggs) > 0 {
			return nil, compileErrorf(target.Expr, "Mixed aggregates and non-aggregates are not allowed.")
		}
		for _, agg := range aggs {
			for _, arg := range agg.Args {
				if c.isAggregate(arg) {
					return nil, compileErrorf(agg, "Aggregates of aggregates are not allowed.")
				}
			}
		}
	}

	if sel.Where != nil {
		c.env = whereEnv
		expr, err := c.compileExpr(sel.Where)
		if err != nil {
			return nil, err
		}
		compiled.Where = expr
		c.env = targetsEnv
	}

	if err := c.resolveGroupBy(sel, compiled); err != nil {
		return nil, err
	}
	if err := c.resolveOrderBy(sel, compiled); err != nil {
		return nil, err
	}

	compiled.HasAgg = false
	for _, target := range compiled.Targets {
		if target.IsAgg {
			compiled.HasAgg = true
			break
		}
	}
	compiled.Aggs = c.aggs
	compiled.UsesBalance = c.usesBalance

	if err := checkGroupCoverage(sel, compiled); err != nil {
		return nil, err
	}
	// bean-query parses PIVOT BY but rejects it as its last check.
	if len(sel.PivotBy) > 0 {
		return nil, compileErrorf(sel.PivotBy[0], "The PIVOT BY clause is not supported yet.")
	}
	return compiled, nil
}

// resolveGroupBy maps GROUP BY items to target indices, appending hidden
// targets for expressions that are not in the select list. Without an
// explicit GROUP BY, an aggregate query implicitly groups by all
// non-aggregate targets (official behavior).
func (c *compiler) resolveGroupBy(sel *bql.Select, compiled *compiledSelect) error {
	if sel.Having != nil {
		return compileErrorf(sel.Having, "The HAVING clause is not supported yet.")
	}
	if len(sel.GroupBy) == 0 {
		hasAgg := false
		for _, target := range compiled.Targets {
			if target.IsAgg {
				hasAgg = true
				break
			}
		}
		if hasAgg {
			for i, target := range compiled.Targets {
				if !target.IsAgg {
					compiled.GroupBy = append(compiled.GroupBy, i)
				}
			}
		}
		return nil
	}

	for _, item := range sel.GroupBy {
		idx, err := c.resolveGroupByItem(item, compiled)
		if err != nil {
			return err
		}
		// bean-query quotes an index as a bare number: its grammar keeps
		// a top-level GROUP BY integer as a Python int.
		ref := pyExprRepr(item)
		if lit, ok := item.(*bql.Int); ok {
			ref = strconv.FormatInt(lit.Value, 10)
		}
		if compiled.Targets[idx].IsAgg {
			return compileErrorf(item, "GROUP-BY expressions may not reference aggregates: '%s'.", ref)
		}
		if compiled.Targets[idx].Type == tInventory {
			return compileErrorf(item, "GROUP-BY a non-hashable type is not supported: '%s'.", ref)
		}
		compiled.GroupBy = append(compiled.GroupBy, idx)
	}
	return nil
}

// resolveGroupByItem resolves a GROUP BY item like bean-query: an index or
// a target name refers to that target; any other expression, which may
// not be an aggregate, matches a target structurally or becomes a hidden
// one.
func (c *compiler) resolveGroupByItem(item bql.Expr, compiled *compiledSelect) (int, error) {
	switch ref := item.(type) {
	case *bql.Int:
		return c.targetIndex(ref, compiled, "GROUP-BY")
	case *bql.Ident:
		if idx, ok := targetNamed(ref.Name, compiled); ok {
			return idx, nil
		}
	}
	if c.isAggregate(item) {
		return 0, compileErrorf(item, "GROUP-BY expressions may not be aggregates: '%s'.", pyExprRepr(item))
	}
	return c.resolveTargetRef(item, compiled, "GROUP-BY")
}

// resolveOrderBy maps ORDER BY expressions to target indices, appending
// hidden targets as needed. A single trailing direction applies to the
// whole list.
func (c *compiler) resolveOrderBy(sel *bql.Select, compiled *compiledSelect) error {
	compiled.OrderDesc = sel.OrderDesc
	for _, item := range sel.OrderBy {
		idx, err := c.resolveTargetRef(item, compiled, "ORDER-BY")
		if err != nil {
			return err
		}
		compiled.OrderBy = append(compiled.OrderBy, idx)
	}
	return nil
}

// resolveTargetRef resolves a clause item to a target index. Integer
// literals are 1-based indices into the visible targets; identifiers match
// aliases; other expressions match targets structurally or are appended as
// hidden targets. clause names the clause in index errors.
func (c *compiler) resolveTargetRef(item bql.Expr, compiled *compiledSelect, clause string) (int, error) {
	if lit, ok := item.(*bql.Int); ok {
		return c.targetIndex(lit, compiled, clause)
	}
	if ident, ok := item.(*bql.Ident); ok {
		if idx, ok := targetNamed(ident.Name, compiled); ok {
			return idx, nil
		}
	}

	key := exprKey(item)
	for i, target := range compiled.Targets {
		if target.key == key {
			return i, nil
		}
	}

	expr, err := c.compileExpr(item)
	if err != nil {
		return 0, err
	}
	compiled.Targets = append(compiled.Targets, compiledTarget{
		Name:   deriveName(item),
		Type:   expr.typ(),
		Hidden: true,
		IsAgg:  c.isAggregate(item),
		expr:   expr,
		key:    key,
	})
	return len(compiled.Targets) - 1, nil
}

// uniqueName returns name, or name_1, name_2, … when it is allocated, like
// bean-query's find_unique_name.
func uniqueName(name string, allocated map[string]bool) string {
	unique := name
	for i := 1; allocated[unique]; i++ {
		unique = fmt.Sprintf("%s_%d", name, i)
	}
	return unique
}

// targetIndex resolves a 1-based index into the visible targets.
func (c *compiler) targetIndex(lit *bql.Int, compiled *compiledSelect, clause string) (int, error) {
	// Walk the visible targets to the index; no int64-to-int narrowing.
	var n int64
	for i, target := range compiled.Targets {
		if target.Hidden {
			continue
		}
		n++
		if n == lit.Value {
			return i, nil
		}
	}
	return 0, compileErrorf(lit, "Invalid %s column index %d.", clause, lit.Value)
}

// targetNamed finds the visible target with the given name or alias.
func targetNamed(name string, compiled *compiledSelect) (int, bool) {
	for i, target := range compiled.Targets {
		if !target.Hidden && target.Name == name {
			return i, true
		}
	}
	return 0, false
}

// columnsAndAggregates counts the column references in e and collects its
// aggregate calls, without looking inside aggregates, like bean-query's
// get_columns_and_aggregates.
func (c *compiler) columnsAndAggregates(e bql.Expr) (columns int, aggs []*bql.Call) {
	var walk func(bql.Expr)
	walk = func(e bql.Expr) {
		switch node := e.(type) {
		case *bql.Ident:
			columns++
		case *bql.Call:
			if c.env.aggregate(strings.ToLower(node.Func)) != nil {
				aggs = append(aggs, node)
				return
			}
			for _, arg := range node.Args {
				walk(arg)
			}
		case *bql.Unary:
			walk(node.X)
		case *bql.Binary:
			walk(node.L)
			walk(node.R)
		}
	}
	walk(e)
	return columns, aggs
}

// isAggregate reports whether e contains an aggregate call.
func (c *compiler) isAggregate(e bql.Expr) bool {
	_, aggs := c.columnsAndAggregates(e)
	return len(aggs) > 0
}

// checkGroupCoverage enforces that grouped queries cover every visible
// non-aggregate target with a GROUP-BY key, using the official error message.
func checkGroupCoverage(sel *bql.Select, compiled *compiledSelect) error {
	if !compiled.HasAgg && len(sel.GroupBy) == 0 {
		return nil
	}
	grouped := make(map[int]bool, len(compiled.GroupBy))
	for _, idx := range compiled.GroupBy {
		grouped[idx] = true
	}
	var missing []string
	for i, target := range compiled.Targets {
		if !target.Hidden && !target.IsAgg && !grouped[i] {
			missing = append(missing, fmt.Sprintf("%q", target.Name))
		}
	}
	if len(missing) > 0 {
		return compileErrorf(sel,
			"All non-aggregates must be covered by GROUP-BY clause in aggregate query; the following targets are missing: %s.",
			strings.Join(missing, ","))
	}
	return nil
}

// compileExpr compiles a BQL expression against the current environment.
func (c *compiler) compileExpr(e bql.Expr) (cexpr, error) {
	switch node := e.(type) {
	case *bql.Str:
		return &cLiteral{v: node.Value, t: tString}, nil
	case *bql.Int:
		return &cLiteral{v: node.Value, t: tInt}, nil
	case *bql.Dec:
		return &cLiteral{v: node.Value, t: tDecimal}, nil
	case *bql.DateLit:
		return &cLiteral{v: node.Value, t: tDate}, nil
	case *bql.Bool:
		return &cLiteral{v: node.Value, t: tBool}, nil
	case *bql.Null:
		return &cLiteral{v: nil, t: tAny}, nil

	case *bql.Ident:
		def, ok := c.env.columns[node.Name]
		if !ok {
			return nil, compileErrorf(node, "Invalid column name '%s' in %s.", node.Name, c.env.context)
		}
		if node.Name == "balance" {
			c.usesBalance = true
		}
		return &cColumn{def: def}, nil

	case *bql.Call:
		return c.compileCall(node)

	case *bql.Unary:
		return c.compileUnary(node)

	case *bql.Binary:
		return c.compileBinary(node)
	}
	return nil, compileErrorf(e, "unsupported expression")
}

func (c *compiler) compileCall(node *bql.Call) (cexpr, error) {
	name := strings.ToLower(node.Func)

	// Like bean-query, compile the arguments before resolving the function.
	args := make([]cexpr, len(node.Args))
	argTypes := make([]dtype, len(node.Args))
	for i, argNode := range node.Args {
		arg, err := c.compileExpr(argNode)
		if err != nil {
			return nil, err
		}
		args[i] = arg
		argTypes[i] = arg.typ()
	}

	if aggregate := c.env.aggregate(name); aggregate != nil && len(args) == 1 {
		if result, ok := aggregate.resultType(argTypes[0]); ok {
			agg := &cAgg{def: aggregate, arg: args[0], result: result, slot: len(c.aggs)}
			c.aggs = append(c.aggs, agg)
			return agg, nil
		}
	}
	if def := c.env.function(name); def != nil {
		if overload := def.matchOverload(argTypes); overload != nil {
			return &cCall{overload: overload, args: args}, nil
		}
	}

	// No signature matches: bean-query falls back to the function's
	// by-name class, whose constructor rejects the arguments.
	if fallback := c.env.fallback(name); fallback != nil {
		if message := fallback.rejection(args); message != "" {
			return nil, compileErrorf(node, "%s", message)
		}
	}
	return nil, compileErrorf(node, "Invalid function '%s(%s)' in %s.", name, argTypeList(args), c.env.context)
}

// argTypeList renders compiled arguments' types by their Python names, as
// bean-query lists them in an invalid function's signature.
func argTypeList(args []cexpr) string {
	names := make([]string, len(args))
	for i, arg := range args {
		names[i] = arg.typ().String()
		if isNullLiteral(arg) {
			names[i] = "NoneType"
		}
	}
	return strings.Join(names, ", ")
}

func (c *compiler) compileUnary(node *bql.Unary) (cexpr, error) {
	x, err := c.compileExpr(node.X)
	if err != nil {
		return nil, err
	}
	if node.Op != bql.NOT {
		return nil, compileErrorf(node, "unsupported unary operator")
	}
	return &cNot{x: x}, nil
}

func (c *compiler) compileBinary(node *bql.Binary) (cexpr, error) {
	l, err := c.compileExpr(node.L)
	if err != nil {
		return nil, err
	}
	r, err := c.compileExpr(node.R)
	if err != nil {
		return nil, err
	}

	t := tBool
	switch node.Op {
	case bql.PLUS, bql.MINUS, bql.ASTERISK:
		if l.typ() == tInt && r.typ() == tInt {
			t = tInt
		} else {
			t = tDecimal
		}
	case bql.SLASH:
		t = tDecimal
	}
	return &cBinary{op: node.Op, l: l, r: r, t: t}, nil
}

// deriveName builds the default column name for an unaliased target,
// matching the official naming: columns keep their name, function calls
// join the function and argument names with underscores, and constants get
// a "c" prefix with punctuation replaced by underscores.
func deriveName(e bql.Expr) string {
	switch node := e.(type) {
	case *bql.Ident:
		return strings.ToLower(node.Name)
	case *bql.Call:
		parts := make([]string, 0, len(node.Args)+1)
		parts = append(parts, strings.ToLower(node.Func))
		for _, arg := range node.Args {
			parts = append(parts, deriveName(arg))
		}
		return strings.Join(parts, "_")
	case *bql.Str:
		return "c" + sanitizeName(node.Value)
	case *bql.Int:
		return "c" + sanitizeName(strconv.FormatInt(node.Value, 10))
	case *bql.Dec:
		return "c" + sanitizeName(decimalLiteral(node.Value))
	case *bql.DateLit:
		return "c" + sanitizeName(node.Value.String())
	case *bql.Bool:
		// Python's str(True) is "True"; the sanitizer turns the capital
		// into an underscore.
		if node.Value {
			return "c" + sanitizeName("True")
		}
		return "c" + sanitizeName("False")
	case *bql.Null:
		return "c" + sanitizeName("None")
	case *bql.Unary:
		return "not_" + deriveName(node.X)
	case *bql.Binary:
		return binaryOpNames[node.Op] + "_" + deriveName(node.L) + "_" + deriveName(node.R)
	}
	return "expr"
}

var binaryOpNames = map[bql.TokenType]string{
	bql.PLUS:     "add",
	bql.MINUS:    "sub",
	bql.ASTERISK: "mul",
	bql.SLASH:    "div",
	bql.EQ:       "equal",
	bql.NE:       "not_equal",
	bql.LT:       "less",
	bql.LTE:      "less_eq",
	bql.GT:       "greater",
	bql.GTE:      "greater_eq",
	bql.TILDE:    "match",
	bql.IN:       "contains",
	bql.AND:      "and",
	bql.OR:       "or",
}

// nameSanitizer collapses runs of characters outside [a-z0-9_] into a single
// underscore, without lowercasing first — 'USD' becomes "_", matching the
// official constant naming (verified against bean-query 2.3.6).
var nameSanitizer = regexp.MustCompile(`[^a-z0-9_]+`)

func sanitizeName(s string) string {
	return nameSanitizer.ReplaceAllString(s, "_")
}

// decimalLiteral renders a decimal constant with the digits it was written
// with, like Python's str(Decimal): 2500.00 stays "2500.00".
func decimalLiteral(d decimal.Decimal) string {
	return d.StringFixed(max(-d.Exponent(), 0))
}

// pyOpClasses names bean-query's parser node class for each operator.
var pyOpClasses = map[bql.TokenType]string{
	bql.AND:      "And",
	bql.OR:       "Or",
	bql.EQ:       "Equal",
	bql.GT:       "Greater",
	bql.GTE:      "GreaterEq",
	bql.LT:       "Less",
	bql.LTE:      "LessEq",
	bql.TILDE:    "Match",
	bql.IN:       "Contains",
	bql.ASTERISK: "Mul",
	bql.SLASH:    "Div",
	bql.PLUS:     "Add",
	bql.MINUS:    "Sub",
}

// pyExprRepr renders a clause item like Python's repr() of bean-query's
// parse tree, which some of its errors quote: an index is its number, a
// name is Column(name='x'), a call is Function(fname='f', operands=[...]).
func pyExprRepr(e bql.Expr) string {
	switch node := e.(type) {
	case *bql.Ident:
		return fmt.Sprintf("Column(name=%s)", pyrepr.String(node.Name))
	case *bql.Call:
		operands := make([]string, len(node.Args))
		for i, arg := range node.Args {
			operands[i] = pyExprRepr(arg)
		}
		return fmt.Sprintf("Function(fname=%s, operands=[%s])", pyrepr.String(strings.ToLower(node.Func)), strings.Join(operands, ", "))
	case *bql.Unary:
		return fmt.Sprintf("Not(operand=%s)", pyExprRepr(node.X))
	case *bql.Binary:
		if node.Op == bql.NE {
			// The parser turns a != b into Not(Equal(a, b)).
			return fmt.Sprintf("Not(operand=Equal(left=%s, right=%s))", pyExprRepr(node.L), pyExprRepr(node.R))
		}
		return fmt.Sprintf("%s(left=%s, right=%s)", pyOpClasses[node.Op], pyExprRepr(node.L), pyExprRepr(node.R))
	}
	return fmt.Sprintf("Constant(value=%s)", pyConstantRepr(e))
}

// pyConstantRepr renders a literal like Python's repr() of its value.
func pyConstantRepr(e bql.Expr) string {
	switch node := e.(type) {
	case *bql.Int:
		return strconv.FormatInt(node.Value, 10)
	case *bql.Dec:
		return fmt.Sprintf("Decimal('%s')", decimalLiteral(node.Value))
	case *bql.Str:
		return pyrepr.String(node.Value)
	case *bql.DateLit:
		t := node.Value.Time
		return fmt.Sprintf("datetime.date(%d, %d, %d)", t.Year(), t.Month(), t.Day())
	case *bql.Bool:
		if node.Value {
			return "True"
		}
		return "False"
	}
	return "None"
}

// exprKey builds a canonical key for structural expression matching, used
// to resolve GROUP-BY and ORDER-BY items against the targets list.
func exprKey(e bql.Expr) string {
	return deriveName(e)
}

// Compiled expression nodes.

type cLiteral struct {
	v any
	t dtype
}

func (c *cLiteral) typ() dtype        { return c.t }
func (c *cLiteral) eval(*evalRow) any { return c.v }

type cColumn struct {
	def *columnDef
}

func (c *cColumn) typ() dtype            { return c.def.typ }
func (c *cColumn) eval(row *evalRow) any { return c.def.eval(row) }

type cCall struct {
	overload *funcOverload
	args     []cexpr
}

func (c *cCall) typ() dtype { return c.overload.result }

func (c *cCall) eval(row *evalRow) any {
	args := make([]any, len(c.args))
	for i, arg := range c.args {
		args[i] = arg.eval(row)
		// Propagate NULL through typed parameters; polymorphic (tAny)
		// parameters receive NULL and decide themselves.
		if args[i] == nil && c.overload.params[i] != tAny {
			return nil
		}
	}
	return c.overload.call(row, args)
}

// cAgg is a reference to an aggregate accumulator slot. During accumulation
// the executor feeds rows to the accumulator; during output evaluation the
// finalized value is read back from evalRow.AggValues.
type cAgg struct {
	def    *aggDef
	arg    cexpr
	result dtype
	slot   int
}

func (c *cAgg) typ() dtype { return c.result }

func (c *cAgg) eval(row *evalRow) any {
	if c.slot < len(row.AggValues) {
		return row.AggValues[c.slot]
	}
	return nil
}

type cNot struct {
	x cexpr
}

func (c *cNot) typ() dtype            { return tBool }
func (c *cNot) eval(row *evalRow) any { return !truthy(c.x.eval(row)) }

type cBinary struct {
	op   bql.TokenType
	l, r cexpr
	t    dtype
}

func (c *cBinary) typ() dtype { return c.t }

func (c *cBinary) eval(row *evalRow) any {
	switch c.op {
	case bql.AND:
		return truthy(c.l.eval(row)) && truthy(c.r.eval(row))
	case bql.OR:
		return truthy(c.l.eval(row)) || truthy(c.r.eval(row))
	}

	l := c.l.eval(row)
	r := c.r.eval(row)

	switch c.op {
	case bql.EQ:
		// NULL equals NULL, matching Python's None == None.
		if l == nil || r == nil {
			return l == nil && r == nil
		}
		return compareValues(l, r) == 0
	case bql.NE:
		if l == nil || r == nil {
			return l != nil || r != nil
		}
		return compareValues(l, r) != 0
	case bql.LT, bql.LTE, bql.GT, bql.GTE:
		if l == nil || r == nil {
			return false
		}
		cmp := compareValues(l, r)
		switch c.op {
		case bql.LT:
			return cmp < 0
		case bql.LTE:
			return cmp <= 0
		case bql.GT:
			return cmp > 0
		default:
			return cmp >= 0
		}
	case bql.TILDE:
		ls, lok := l.(string)
		rs, rok := r.(string)
		if !lok || !rok {
			return false
		}
		matched, err := regexp.MatchString(rs, ls)
		return err == nil && matched
	case bql.IN:
		elem, ok := l.(string)
		if !ok {
			return false
		}
		set, ok := r.(setValue)
		if !ok {
			return false
		}
		return set.Contains(elem)
	case bql.PLUS, bql.MINUS, bql.ASTERISK, bql.SLASH:
		return c.evalArithmetic(l, r)
	}
	return nil
}

func (c *cBinary) evalArithmetic(l, r any) any {
	if li, lok := l.(int64); lok {
		if ri, rok := r.(int64); rok && c.op != bql.SLASH {
			switch c.op {
			case bql.PLUS:
				return li + ri
			case bql.MINUS:
				return li - ri
			case bql.ASTERISK:
				return li * ri
			}
		}
	}
	ld, lok := asDecimal(l)
	rd, rok := asDecimal(r)
	if !lok || !rok {
		return nil
	}
	switch c.op {
	case bql.PLUS:
		return pydecimal.Add(ld, rd)
	case bql.MINUS:
		return pydecimal.Sub(ld, rd)
	case bql.ASTERISK:
		return pydecimal.Mul(ld, rd)
	case bql.SLASH:
		if rd.IsZero() {
			return nil
		}
		return pydecimal.Quo(ld, rd)
	}
	return nil
}
