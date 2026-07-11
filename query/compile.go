package query

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/robinvdvleuten/beancount/ast"
	"github.com/robinvdvleuten/beancount/query/bql"
	"github.com/shopspring/decimal"
)

// CompileError represents a semantic error found while compiling a query.
type CompileError struct {
	Pos     ast.Position
	Message string
}

func (e *CompileError) Error() string {
	return e.Message
}

// GetPosition implements the positioned-error interface used by the CLI
// error renderer.
func (e *CompileError) GetPosition() ast.Position {
	return e.Pos
}

func compileErrorf(node bql.Node, format string, args ...any) *CompileError {
	return &CompileError{Pos: node.Pos(), Message: fmt.Sprintf(format, args...)}
}

// cexpr is a compiled expression: a typed, evaluatable tree node.
type cexpr interface {
	typ() DType
	eval(row *Row) any
}

// Compiled is a fully resolved SELECT ready for execution. GroupBy, OrderBy,
// and PivotBy reference targets by index; hidden targets were appended
// during resolution and are not rendered.
type Compiled struct {
	Targets   []CompiledTarget
	Where     cexpr
	From      *CompiledFrom
	GroupBy   []int
	OrderBy   []int
	OrderDesc bool
	PivotBy   []int
	Limit     *int64
	Distinct  bool
	HasAgg    bool
	Aggs      []*cAgg
	// UsesBalance is set when the running balance column is referenced,
	// so the executor can skip per-row inventory snapshots otherwise.
	UsesBalance bool
}

// CompiledTarget is one output column (or hidden sort/group key).
type CompiledTarget struct {
	Name   string
	Type   DType
	Hidden bool
	IsAgg  bool // the expression contains an aggregate function
	expr   cexpr
	key    string // canonical expression key for structural matching
}

// CompiledFrom is the compiled FROM clause: an entry-level filter plus
// summarization transforms applied by the executor.
type CompiledFrom struct {
	Expr    cexpr
	OpenOn  *ast.Date
	Close   bool
	CloseOn *ast.Date
	Clear   bool
}

// Compile resolves and type-checks a parsed BQL statement against the query
// environments. BALANCES and JOURNAL desugar to their SELECT expansions;
// PRINT compiles separately via CompilePrint.
func Compile(ctx *Context, stmt bql.Statement) (*Compiled, error) {
	sel, ok := Desugar(stmt).(*bql.Select)
	if !ok {
		return nil, fmt.Errorf("statement %T is not supported yet", stmt)
	}
	c := &compiler{ctx: ctx}
	return c.compileSelect(sel)
}

type compiler struct {
	ctx         *Context
	env         *environment
	aggs        []*cAgg
	usesBalance bool
}

func (c *compiler) compileSelect(sel *bql.Select) (*Compiled, error) {
	compiled := &Compiled{Distinct: sel.Distinct, Limit: sel.Limit}

	// FROM compiles against the entry environment, everything else against
	// the posting environment.
	if sel.From != nil {
		from := &CompiledFrom{
			OpenOn:  sel.From.OpenOn,
			Close:   sel.From.Close,
			CloseOn: sel.From.CloseOn,
			Clear:   sel.From.Clear,
		}
		if sel.From.Expr != nil {
			c.env = filterEnv
			expr, err := c.compileExpr(sel.From.Expr, false)
			if err != nil {
				return nil, err
			}
			from.Expr = expr
		}
		compiled.From = from
	}

	c.env = targetsEnv

	// Targets: expand the wildcard or compile the explicit list.
	targets := sel.Targets
	if sel.Wildcard {
		targets = make([]bql.Target, len(wildcardColumns))
		for i, name := range wildcardColumns {
			targets[i] = bql.Target{Expr: &bql.Ident{Name: name}}
		}
	}
	for _, target := range targets {
		before := len(c.aggs)
		expr, err := c.compileExpr(target.Expr, true)
		if err != nil {
			return nil, err
		}
		name := target.As
		if name == "" {
			name = deriveName(target.Expr)
		}
		compiled.Targets = append(compiled.Targets, CompiledTarget{
			Name:  name,
			Type:  expr.typ(),
			IsAgg: len(c.aggs) > before,
			expr:  expr,
			key:   exprKey(target.Expr),
		})
	}

	if sel.Where != nil {
		before := len(c.aggs)
		expr, err := c.compileExpr(sel.Where, false)
		if err != nil {
			return nil, err
		}
		if len(c.aggs) > before {
			return nil, compileErrorf(sel.Where, "Aggregates are disallowed in WHERE clause.")
		}
		compiled.Where = expr
	}

	if err := c.resolveGroupBy(sel, compiled); err != nil {
		return nil, err
	}
	if err := c.resolveOrderBy(sel, compiled); err != nil {
		return nil, err
	}
	if err := c.resolvePivotBy(sel, compiled); err != nil {
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
	return compiled, nil
}

// resolveGroupBy maps GROUP BY items to target indices, appending hidden
// targets for expressions that are not in the select list. Without an
// explicit GROUP BY, an aggregate query implicitly groups by all
// non-aggregate targets (official behavior).
func (c *compiler) resolveGroupBy(sel *bql.Select, compiled *Compiled) error {
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
		idx, err := c.resolveTargetRef(item, compiled, false)
		if err != nil {
			return err
		}
		if compiled.Targets[idx].IsAgg {
			return compileErrorf(item, "GROUP-BY expressions may not be aggregates.")
		}
		compiled.GroupBy = append(compiled.GroupBy, idx)
	}
	return nil
}

// resolveOrderBy maps ORDER BY expressions to target indices, appending
// hidden targets as needed. A single trailing direction applies to the
// whole list.
func (c *compiler) resolveOrderBy(sel *bql.Select, compiled *Compiled) error {
	compiled.OrderDesc = sel.OrderDesc
	for _, item := range sel.OrderBy {
		idx, err := c.resolveTargetRef(item, compiled, true)
		if err != nil {
			return err
		}
		compiled.OrderBy = append(compiled.OrderBy, idx)
	}
	return nil
}

func (c *compiler) resolvePivotBy(sel *bql.Select, compiled *Compiled) error {
	for _, item := range sel.PivotBy {
		idx, err := c.resolveTargetRef(item, compiled, false)
		if err != nil {
			return err
		}
		compiled.PivotBy = append(compiled.PivotBy, idx)
	}
	return nil
}

// resolveTargetRef resolves a clause item to a target index. Integer
// literals are 1-based indices into the visible targets; identifiers match
// aliases; other expressions match targets structurally or are appended as
// hidden targets.
func (c *compiler) resolveTargetRef(item bql.Expr, compiled *Compiled, allowAgg bool) (int, error) {
	if lit, ok := item.(*bql.Int); ok {
		visible := 0
		for _, target := range compiled.Targets {
			if !target.Hidden {
				visible++
			}
		}
		// Range-check the int64 literal before narrowing to int.
		if lit.Value < 1 || lit.Value > int64(visible) {
			return 0, compileErrorf(item, "Invalid GROUP-BY column index %d", lit.Value)
		}
		return int(lit.Value) - 1, nil
	}

	if ident, ok := item.(*bql.Ident); ok {
		for i, target := range compiled.Targets {
			if !target.Hidden && target.Name == ident.Name {
				return i, nil
			}
		}
	}

	key := exprKey(item)
	for i, target := range compiled.Targets {
		if target.key == key {
			return i, nil
		}
	}

	before := len(c.aggs)
	expr, err := c.compileExpr(item, allowAgg)
	if err != nil {
		return 0, err
	}
	compiled.Targets = append(compiled.Targets, CompiledTarget{
		Name:   deriveName(item),
		Type:   expr.typ(),
		Hidden: true,
		IsAgg:  len(c.aggs) > before,
		expr:   expr,
		key:    key,
	})
	return len(compiled.Targets) - 1, nil
}

// checkGroupCoverage enforces that grouped queries cover every visible
// non-aggregate target with a GROUP-BY key, using the official error message.
func checkGroupCoverage(sel *bql.Select, compiled *Compiled) error {
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
func (c *compiler) compileExpr(e bql.Expr, allowAgg bool) (cexpr, error) {
	switch node := e.(type) {
	case *bql.Str:
		return &cLiteral{v: node.Value, t: TString}, nil
	case *bql.Int:
		return &cLiteral{v: node.Value, t: TInt}, nil
	case *bql.Dec:
		return &cLiteral{v: node.Value, t: TDecimal}, nil
	case *bql.DateLit:
		return &cLiteral{v: node.Value, t: TDate}, nil
	case *bql.Bool:
		return &cLiteral{v: node.Value, t: TBool}, nil
	case *bql.Null:
		return &cLiteral{v: nil, t: TAny}, nil

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
		return c.compileCall(node, allowAgg)

	case *bql.Unary:
		return c.compileUnary(node, allowAgg)

	case *bql.Binary:
		return c.compileBinary(node, allowAgg)
	}
	return nil, compileErrorf(e, "unsupported expression")
}

func (c *compiler) compileCall(node *bql.Call, allowAgg bool) (cexpr, error) {
	name := strings.ToLower(node.Func)

	if aggregate, ok := aggregates[name]; ok {
		if !allowAgg {
			return nil, compileErrorf(node, "Aggregates are disallowed in this context.")
		}
		if len(node.Args) != 1 {
			return nil, compileErrorf(node, "Aggregate function '%s' takes exactly one argument.", name)
		}
		arg, err := c.compileExpr(node.Args[0], false)
		if err != nil {
			return nil, err
		}
		result, ok := aggregate.resultType(arg.typ())
		if !ok {
			return nil, compileErrorf(node, "Invalid function '%s(%s)' in %s.", name, arg.typ(), c.env.context)
		}
		agg := &cAgg{def: aggregate, arg: arg, result: result, slot: len(c.aggs)}
		c.aggs = append(c.aggs, agg)
		return agg, nil
	}

	def, ok := functions[name]
	if !ok {
		return nil, compileErrorf(node, "Invalid function '%s(%s)' in %s.", name, argTypeList(nil, node.Args), c.env.context)
	}
	args := make([]cexpr, len(node.Args))
	argTypes := make([]DType, len(node.Args))
	for i, argNode := range node.Args {
		arg, err := c.compileExpr(argNode, allowAgg)
		if err != nil {
			return nil, err
		}
		args[i] = arg
		argTypes[i] = arg.typ()
	}
	overload := def.matchOverload(argTypes)
	if overload == nil {
		return nil, compileErrorf(node, "Invalid function '%s(%s)' in %s.", name, argTypeList(argTypes, nil), c.env.context)
	}
	return &cCall{overload: overload, args: args}, nil
}

// argTypeList renders argument types for error messages, falling back to
// argument text when types are unknown.
func argTypeList(types []DType, nodes []bql.Expr) string {
	if types != nil {
		names := make([]string, len(types))
		for i, t := range types {
			names[i] = t.String()
		}
		return strings.Join(names, ",")
	}
	names := make([]string, len(nodes))
	for i, node := range nodes {
		names[i] = deriveName(node)
	}
	return strings.Join(names, ",")
}

func (c *compiler) compileUnary(node *bql.Unary, allowAgg bool) (cexpr, error) {
	x, err := c.compileExpr(node.X, allowAgg)
	if err != nil {
		return nil, err
	}
	switch node.Op {
	case bql.NOT:
		return &cNot{x: x}, nil
	case bql.MINUS:
		return &cNeg{x: x}, nil
	case bql.PLUS:
		return x, nil
	}
	return nil, compileErrorf(node, "unsupported unary operator")
}

func (c *compiler) compileBinary(node *bql.Binary, allowAgg bool) (cexpr, error) {
	l, err := c.compileExpr(node.L, allowAgg)
	if err != nil {
		return nil, err
	}
	r, err := c.compileExpr(node.R, allowAgg)
	if err != nil {
		return nil, err
	}

	t := TBool
	switch node.Op {
	case bql.PLUS, bql.MINUS, bql.ASTERISK:
		if l.typ() == TInt && r.typ() == TInt {
			t = TInt
		} else {
			t = TDecimal
		}
	case bql.SLASH:
		t = TDecimal
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
		return fmt.Sprintf("c%d", node.Value)
	case *bql.Dec:
		return "c" + sanitizeName(node.Value.String())
	case *bql.DateLit:
		return "c" + sanitizeName(node.Value.String())
	case *bql.Bool:
		if node.Value {
			return "ctrue"
		}
		return "cfalse"
	case *bql.Null:
		return "cnone"
	case *bql.Unary:
		prefix := "neg"
		if node.Op == bql.NOT {
			prefix = "not"
		}
		return prefix + "_" + deriveName(node.X)
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
	bql.IN:       "in",
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

// exprKey builds a canonical key for structural expression matching, used
// to resolve GROUP-BY and ORDER-BY items against the targets list.
func exprKey(e bql.Expr) string {
	return deriveName(e)
}

// Compiled expression nodes.

type cLiteral struct {
	v any
	t DType
}

func (c *cLiteral) typ() DType    { return c.t }
func (c *cLiteral) eval(*Row) any { return c.v }

type cColumn struct {
	def *columnDef
}

func (c *cColumn) typ() DType        { return c.def.typ }
func (c *cColumn) eval(row *Row) any { return c.def.eval(row) }

type cCall struct {
	overload *funcOverload
	args     []cexpr
}

func (c *cCall) typ() DType { return c.overload.result }

func (c *cCall) eval(row *Row) any {
	args := make([]any, len(c.args))
	for i, arg := range c.args {
		args[i] = arg.eval(row)
		// Propagate NULL through typed parameters; polymorphic (TAny)
		// parameters receive NULL and decide themselves.
		if args[i] == nil && c.overload.params[i] != TAny {
			return nil
		}
	}
	return c.overload.call(row, args)
}

// cAgg is a reference to an aggregate accumulator slot. During accumulation
// the executor feeds rows to the accumulator; during output evaluation the
// finalized value is read back from Row.AggValues.
type cAgg struct {
	def    *aggDef
	arg    cexpr
	result DType
	slot   int
}

func (c *cAgg) typ() DType { return c.result }

func (c *cAgg) eval(row *Row) any {
	if c.slot < len(row.AggValues) {
		return row.AggValues[c.slot]
	}
	return nil
}

type cNot struct {
	x cexpr
}

func (c *cNot) typ() DType        { return TBool }
func (c *cNot) eval(row *Row) any { return !truthy(c.x.eval(row)) }

type cNeg struct {
	x cexpr
}

func (c *cNeg) typ() DType { return c.x.typ() }

func (c *cNeg) eval(row *Row) any {
	switch v := c.x.eval(row).(type) {
	case int64:
		return -v
	case decimal.Decimal:
		return v.Neg()
	}
	return nil
}

type cBinary struct {
	op   bql.TokenType
	l, r cexpr
	t    DType
}

func (c *cBinary) typ() DType { return c.t }

func (c *cBinary) eval(row *Row) any {
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
		set, ok := r.(Set)
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
		return ld.Add(rd)
	case bql.MINUS:
		return ld.Sub(rd)
	case bql.ASTERISK:
		return ld.Mul(rd)
	case bql.SLASH:
		if rd.IsZero() {
			return nil
		}
		return ld.Div(rd)
	}
	return nil
}
