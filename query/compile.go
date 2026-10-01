package query

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/robinvdvleuten/beancount/internal/pyrepr"
	"github.com/robinvdvleuten/beancount/query/bql"
	"github.com/shopspring/decimal"
)

// compileErrorf reports an error beanquery raises at node, whose source
// text its shell underlines. Run adds the statement's text. A node that
// BALANCES or JOURNAL desugars to has no source text, so its error is
// reported without one.
func compileErrorf(node bql.Node, format string, args ...any) *Error {
	start, end := node.Span()
	return &Error{message: fmt.Sprintf(format, args...), hasNode: start < end, start: start, end: end}
}

// statementErrorf reports an error beanquery raises without a node.
func statementErrorf(format string, args ...any) *Error {
	return &Error{message: fmt.Sprintf(format, args...)}
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
	Targets []compiledTarget
	// Where filters the posting rows: FROM's expression and WHERE, joined
	// like beanquery's EvalAnd([c_from_expr, c_where]).
	Where cexpr
	// From holds the FROM transforms (OPEN, CLOSE, CLEAR); a SELECT's
	// expression is joined into Where.
	From    *compiledFrom
	GroupBy []int
	// Having is the index of the hidden HAVING target, or -1.
	Having  int
	OrderBy []orderKey
	// Pivot holds the two PIVOT BY targets, or nothing.
	Pivot    []int
	Limit    *int64
	Distinct bool
	HasAgg   bool
	Aggs     []*cAgg
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
	ctx  *Context
	env  *environment
	aggs []*cAgg
}

func (c *compiler) compileSelect(sel *bql.Select) (*compiledSelect, error) {
	compiled := &compiledSelect{Distinct: sel.Distinct, Limit: sel.Limit, Having: -1}

	// Like beanquery, a SELECT's FROM expression compiles against the
	// postings table, before the targets.
	from, err := c.compileFrom(sel.From, targetsEnv)
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
	for _, target := range targets {
		expr, err := c.compileExpr(target.Expr)
		if err != nil {
			return nil, err
		}
		compiled.Targets = append(compiled.Targets, compiledTarget{
			Name:  targetName(target),
			Type:  expr.typ(),
			IsAgg: c.isAggregate(target.Expr),
			expr:  expr,
			key:   exprKey(target.Expr),
		})
		// Like beanquery, check each target's aggregates once it compiles.
		columns, aggs := c.columnsAndAggregates(target.Expr)
		if columns > 0 && len(aggs) > 0 {
			return nil, statementErrorf("mixed aggregates and non-aggregates are not allowed")
		}
		for _, agg := range aggs {
			for _, arg := range agg.Args {
				if c.isAggregate(arg) {
					return nil, statementErrorf("aggregates of aggregates are not allowed")
				}
			}
		}
	}

	if sel.Where != nil {
		expr, err := c.compileExpr(sel.Where)
		if err != nil {
			return nil, err
		}
		if c.isAggregate(sel.Where) {
			return nil, statementErrorf("aggregates are not allowed in WHERE clause")
		}
		compiled.Where = expr
	}
	// Like beanquery, FROM's expression filters the posting rows, ahead of
	// WHERE in one conjunction.
	if from != nil && from.Expr != nil {
		if compiled.Where == nil {
			compiled.Where = from.Expr
		} else {
			compiled.Where = &cAnd{l: from.Expr, r: compiled.Where}
		}
	}

	if err := c.resolveGroupBy(sel, compiled); err != nil {
		return nil, err
	}
	// Like beanquery, the targets, GROUP BY and HAVING make a query an
	// aggregate query, and ORDER BY does not: an aggregate there in a
	// query that does not group is never computed and orders as NULL.
	compiled.HasAgg = slices.ContainsFunc(compiled.Targets, func(target compiledTarget) bool { return target.IsAgg })
	if err := c.resolveOrderBy(sel, compiled); err != nil {
		return nil, err
	}
	compiled.Aggs = c.aggs

	if err := checkGroupCoverage(sel, compiled); err != nil {
		return nil, err
	}
	if err := c.resolvePivotBy(sel, compiled); err != nil {
		return nil, err
	}
	return compiled, nil
}

// resolvePivotBy resolves PIVOT BY's two items like beanquery's
// _compile_pivot_by: an index or a name (the last visible target with it),
// the two distinct, and the second a GROUP BY column. A query without GROUP BY
// has none, where beanquery fails with a Python TypeError (KNOWN_GAPS.md).
func (c *compiler) resolvePivotBy(sel *bql.Select, compiled *compiledSelect) error {
	if len(sel.PivotBy) == 0 {
		return nil
	}
	for _, item := range sel.PivotBy {
		var idx int
		switch ref := item.(type) {
		case *bql.ColumnIndex:
			// Like beanquery, an index counts every target, hidden
			// ones included.
			if ref.Value < 1 || ref.Value > int64(len(compiled.Targets)) {
				return statementErrorf("invalid PIVOT BY column index %d", ref.Value)
			}
			idx = int(ref.Value - 1)
		case *bql.Ident:
			var ok bool
			if idx, ok = targetNamed(ref.Name, compiled); !ok {
				return statementErrorf("PIVOT BY column %s is not in the targets list", pyExprRepr(ref))
			}
		}
		compiled.Pivot = append(compiled.Pivot, idx)
	}
	if compiled.Pivot[0] == compiled.Pivot[1] {
		return statementErrorf("the two PIVOT BY columns cannot be the same column")
	}
	if !slices.Contains(compiled.GroupBy, compiled.Pivot[1]) {
		return statementErrorf("the second PIVOT BY column must be a GROUP BY column")
	}
	// beanquery pivots the visible columns, and fails with a Python
	// IndexError on a hidden one (KNOWN_GAPS.md).
	for _, idx := range compiled.Pivot {
		if compiled.Targets[idx].Hidden {
			return statementErrorf("invalid PIVOT BY column index %d", idx+1)
		}
	}
	return nil
}

// resolveGroupBy maps GROUP BY items to target indices, appending hidden
// targets for expressions that are not in the select list. Without an
// explicit GROUP BY, an aggregate query implicitly groups by all
// non-aggregate targets (official behavior).
func (c *compiler) resolveGroupBy(sel *bql.Select, compiled *compiledSelect) error {
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

	// GROUP BY resolves before any hidden target is added, so every
	// target is a visible one an index may refer to.
	visible := int64(len(compiled.Targets))
	for _, item := range sel.GroupBy {
		idx, err := c.resolveGroupByItem(item, compiled, visible)
		if err != nil {
			return err
		}
		ref := pyExprRepr(item)
		if compiled.Targets[idx].IsAgg {
			return statementErrorf(`GROUP-BY expressions may not reference aggregates: "%s"`, ref)
		}
		// Sets, lists and inventories are unhashable in Python.
		if t := compiled.Targets[idx].Type; t == tInventory || t == tSet || t == tList {
			return statementErrorf(`GROUP-BY a non-hashable type is not supported: "%s"`, ref)
		}
		compiled.GroupBy = append(compiled.GroupBy, idx)
	}
	return c.resolveHaving(sel, compiled)
}

// resolveHaving compiles HAVING, which must hold an aggregate, into a
// hidden aggregate target, like beanquery.
func (c *compiler) resolveHaving(sel *bql.Select, compiled *compiledSelect) error {
	if sel.Having == nil {
		return nil
	}
	expr, err := c.compileExpr(sel.Having)
	if err != nil {
		return err
	}
	if !c.isAggregate(sel.Having) {
		return statementErrorf("the HAVING clause must be an aggregate expression")
	}
	compiled.Targets = append(compiled.Targets, compiledTarget{
		Type:   expr.typ(),
		Hidden: true,
		IsAgg:  true,
		expr:   expr,
		key:    exprKey(sel.Having),
	})
	compiled.Having = len(compiled.Targets) - 1
	return nil
}

// resolveGroupByItem resolves a GROUP BY item like bean-query: an index or
// a target name refers to that target; any other expression, which may
// not be an aggregate, matches a target structurally or becomes a hidden
// one.
func (c *compiler) resolveGroupByItem(item bql.Expr, compiled *compiledSelect, visible int64) (int, error) {
	switch ref := item.(type) {
	case *bql.ColumnIndex:
		return c.targetIndex(ref, compiled, "GROUP-BY", visible)
	case *bql.Ident:
		if idx, ok := targetNamed(ref.Name, compiled); ok {
			return idx, nil
		}
	}
	// Like beanquery, compile the item, reporting its own errors, before
	// checking it for aggregates.
	expr, err := c.compileExpr(item)
	if err != nil {
		return 0, err
	}
	if c.isAggregate(item) {
		return 0, statementErrorf(`GROUP-BY expressions may not be aggregates: "%s"`, pyExprRepr(item))
	}
	if idx, ok := targetMatching(item, compiled); ok {
		return idx, nil
	}
	return c.hiddenTarget(item, expr, compiled), nil
}

// orderKey is an ORDER BY term resolved to a target, with its direction.
type orderKey struct {
	target int
	desc   bool
}

// resolveOrderBy maps ORDER BY terms to target indices, appending hidden
// targets as needed. Each term keeps its own direction. Like beanquery, an
// index may not exceed the number of distinct target names, so with a
// repeated name the last targets cannot be referred to by index.
func (c *compiler) resolveOrderBy(sel *bql.Select, compiled *compiledSelect) error {
	names := make(map[string]bool, len(compiled.Targets))
	for _, target := range compiled.Targets {
		if !target.Hidden {
			names[target.Name] = true
		}
	}
	limit := int64(len(names))
	for _, term := range sel.OrderBy {
		idx, err := c.resolveTargetRef(term.Expr, compiled, limit)
		if err != nil {
			return err
		}
		compiled.OrderBy = append(compiled.OrderBy, orderKey{target: idx, desc: term.Desc})
	}
	return nil
}

// resolveTargetRef resolves an ORDER BY item to a target index. A column
// index is 1-based into the first limit visible targets; identifiers match
// aliases; other expressions match targets structurally or are appended as
// hidden targets.
func (c *compiler) resolveTargetRef(item bql.Expr, compiled *compiledSelect, limit int64) (int, error) {
	if index, ok := item.(*bql.ColumnIndex); ok {
		return c.targetIndex(index, compiled, "ORDER-BY", limit)
	}
	if ident, ok := item.(*bql.Ident); ok {
		if idx, ok := targetNamed(ident.Name, compiled); ok {
			return idx, nil
		}
	}

	if idx, ok := targetMatching(item, compiled); ok {
		return idx, nil
	}
	expr, err := c.compileExpr(item)
	if err != nil {
		return 0, err
	}
	return c.hiddenTarget(item, expr, compiled), nil
}

// targetMatching finds the target whose expression item matches
// structurally.
func targetMatching(item bql.Expr, compiled *compiledSelect) (int, bool) {
	key := exprKey(item)
	for i, target := range compiled.Targets {
		if target.key == key {
			return i, true
		}
	}
	return 0, false
}

// hiddenTarget appends item, compiled to expr, as a hidden target and
// returns its index.
func (c *compiler) hiddenTarget(item bql.Expr, expr cexpr, compiled *compiledSelect) int {
	compiled.Targets = append(compiled.Targets, compiledTarget{
		Type:   expr.typ(),
		Hidden: true,
		IsAgg:  c.isAggregate(item),
		expr:   expr,
		key:    exprKey(item),
	})
	return len(compiled.Targets) - 1
}

// targetName names a target's column like beanquery's get_target_name: by
// its alias, a bare column by its name, and any other expression by its
// source text. Names may repeat.
func targetName(target bql.Target) string {
	if target.As != "" {
		return target.As
	}
	if ident, ok := target.Expr.(*bql.Ident); ok {
		return ident.Name
	}
	return target.Text
}

// targetIndex resolves a 1-based index into the visible targets, of which
// the first limit may be referred to. clause names the clause in errors.
func (c *compiler) targetIndex(lit *bql.ColumnIndex, compiled *compiledSelect, clause string, limit int64) (int, error) {
	// Walk the visible targets to the index; no int64-to-int narrowing.
	var n int64
	for i, target := range compiled.Targets {
		if target.Hidden {
			continue
		}
		n++
		if n == lit.Value && n <= limit {
			return i, nil
		}
	}
	return 0, statementErrorf("invalid %s column index %d", clause, lit.Value)
}

// targetNamed finds the visible target with the given name or alias, the
// last one when several share it, as beanquery's name map keeps the last.
func targetNamed(name string, compiled *compiledSelect) (int, bool) {
	for i := len(compiled.Targets) - 1; i >= 0; i-- {
		if target := compiled.Targets[i]; !target.Hidden && target.Name == name {
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
			if aggregates[strings.ToLower(node.Func)] != nil {
				aggs = append(aggs, node)
				return
			}
			for _, arg := range node.Args {
				walk(arg)
			}
		case *bql.Unary:
			walk(node.X)
		case *bql.IsNull:
			walk(node.X)
		case *bql.Binary:
			walk(node.L)
			walk(node.R)
		case *bql.Attribute:
			walk(node.X)
		case *bql.Subscript:
			walk(node.X)
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

// checkGroupCoverage enforces that grouped queries cover every non-aggregate
// target with a GROUP-BY key, using the official error message.
func checkGroupCoverage(sel *bql.Select, compiled *compiledSelect) error {
	if !compiled.HasAgg && len(sel.GroupBy) == 0 {
		return nil
	}
	grouped := make(map[int]bool, len(compiled.GroupBy))
	for _, idx := range compiled.GroupBy {
		grouped[idx] = true
	}
	// Like beanquery, a hidden target counts too, named None: ORDER BY date
	// in an aggregate query reports "None" as missing.
	var missing []string
	for i, target := range compiled.Targets {
		if !target.IsAgg && !grouped[i] {
			name := target.Name
			if target.Hidden {
				name = "None"
			}
			missing = append(missing, `"`+name+`"`)
		}
	}
	if len(missing) > 0 {
		return statementErrorf("all non-aggregates must be covered by GROUP-BY clause in aggregate query: "+
			"the following targets are missing: %s", strings.Join(missing, ","))
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
		return &cLiteral{v: nil, t: tNull}, nil
	case *bql.Asterisk:
		return &cLiteral{v: nil, t: tAsterisk}, nil
	case *bql.List:
		return &cLiteral{v: constantValue(node), t: tList}, nil

	case *bql.Ident:
		def, ok := c.env.columns[node.Name]
		if !ok {
			return nil, compileErrorf(node, `column "%s" not found in table "%s"`, node.Name, c.env.table)
		}
		return &cColumn{def: def}, nil

	case *bql.Call:
		return c.compileCall(node)

	case *bql.Unary:
		return c.compileUnary(node)

	case *bql.IsNull:
		x, err := c.compileExpr(node.X)
		if err != nil {
			return nil, err
		}
		return &cIsNull{x: x, not: node.Not}, nil

	case *bql.Binary:
		return c.compileBinary(node)

	case *bql.Attribute:
		x, err := c.compileExpr(node.X)
		if err != nil {
			return nil, err
		}
		attrs, ok := structures[x.typ()]
		if !ok {
			return nil, compileErrorf(node, "column type is not structured")
		}
		attr, ok := attrs[node.Name]
		if !ok {
			return nil, compileErrorf(node, `structured type has no attribute "%s"`, node.Name)
		}
		return &cAttribute{x: x, attr: attr}, nil

	case *bql.Subscript:
		// Only beanquery's dict columns, such as meta, take a subscript;
		// ours has none (KNOWN_GAPS.md).
		if _, err := c.compileExpr(node.X); err != nil {
			return nil, err
		}
		return nil, compileErrorf(node, "column type is not subscriptable")
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

	// Every clause registers the aggregates, like beanquery, which rejects
	// one in WHERE or FROM once the clause compiles.
	if aggregate := aggregates[name]; aggregate != nil && len(args) == 1 {
		if result, ok := aggregate.resultType(argTypes[0]); ok {
			agg := &cAgg{def: aggregate, arg: args[0], result: result, slot: len(c.aggs)}
			c.aggs = append(c.aggs, agg)
			return agg, nil
		}
	}
	if special, ok := specialFunctions[name]; ok {
		return special(node, args)
	}
	if def := functions[name]; def != nil {
		if overload := def.matchOverload(argTypes); overload != nil {
			return &cCall{overload: overload, args: args}, nil
		}
	}

	return nil, compileErrorf(node, `no function matches "%s(%s)" name and argument types`, name, argTypeList(args))
}

// specialFunctions are the functions beanquery compiles itself rather than
// looking up by argument types.
var specialFunctions = map[string]func(node *bql.Call, args []cexpr) (cexpr, error){
	"coalesce": compileCoalesce,
}

// compileCoalesce compiles coalesce(), the first non-NULL argument, which
// beanquery requires to have arguments of one type (NULL included).
func compileCoalesce(node *bql.Call, args []cexpr) (cexpr, error) {
	if len(args) == 0 {
		// beanquery fails with a Python IndexError (KNOWN_GAPS.md).
		return nil, compileErrorf(node, `no function matches "coalesce()" name and argument types`)
	}
	names := make([]string, len(args))
	uniform := true
	for i, arg := range args {
		names[i] = arg.typ().String()
		uniform = uniform && arg.typ() == args[0].typ()
	}
	if !uniform {
		return nil, compileErrorf(node, "coalesce() function arguments must have uniform type, found: %s", strings.Join(names, ", "))
	}
	return &cCoalesce{args: args}, nil
}

// cCoalesce is coalesce(): its first non-NULL argument.
type cCoalesce struct {
	args []cexpr
}

func (c *cCoalesce) typ() dtype { return c.args[0].typ() }

func (c *cCoalesce) eval(row *evalRow) any {
	for _, arg := range c.args {
		if v := arg.eval(row); v != nil {
			return v
		}
	}
	return nil
}

// argTypeList renders compiled arguments' types by their lower-cased
// Python names, as beanquery lists them in a signature no function
// matches.
func argTypeList(args []cexpr) string {
	names := make([]string, len(args))
	for i, arg := range args {
		names[i] = strings.ToLower(arg.typ().String())
	}
	return strings.Join(names, ", ")
}

func (c *compiler) compileUnary(node *bql.Unary) (cexpr, error) {
	x, err := c.compileExpr(node.X)
	if err != nil {
		return nil, err
	}
	def, ok := unaryOperators[node.Op]
	if !ok {
		return nil, compileErrorf(node, "unsupported unary operator")
	}
	sig := def.match(x.typ())
	if sig == nil {
		return nil, compileErrorf(node, `operator "%s(%s)" not supported`, def.name, operandTypeName(x.typ()))
	}
	return &cUnary{sig: sig, x: x}, nil
}

// compileBinary compiles a binary operation. AND, OR and IN take operands
// of any type; any other operator must have a signature for its operands'
// types, as in beanquery, which casts an untyped (object) operand to the
// other operand's type first, an integer promoted to a decimal.
func (c *compiler) compileBinary(node *bql.Binary) (cexpr, error) {
	l, err := c.compileExpr(node.L)
	if err != nil {
		return nil, err
	}
	r, err := c.compileExpr(node.R)
	if err != nil {
		return nil, err
	}

	if untyped, ok := untypedOperators[node.Op]; ok {
		return untyped(l, r), nil
	}
	def := operators[node.Op]
	for {
		if sig := def.match(l.typ(), r.typ()); sig != nil {
			return newOperator(sig, l, r), nil
		}
		if l.typ() == tAny && r.typ() != tAny {
			if cast := castOperand(l, r.typ()); cast != nil {
				l = cast
				continue
			}
		} else if r.typ() == tAny && l.typ() != tAny {
			if cast := castOperand(r, l.typ()); cast != nil {
				r = cast
				continue
			}
		}
		return nil, compileErrorf(node, `operator "%s(%s, %s)" not supported`, def.name, operandTypeName(l.typ()), operandTypeName(r.typ()))
	}
}

// castOperand casts an untyped operand to the type of the other, or
// returns nil when beanquery has no cast to it.
func castOperand(x cexpr, to dtype) cexpr {
	if to == tInt {
		to = tDecimal
	}
	if cast, ok := casts[to]; ok {
		return &cCast{x: x, to: to, cast: cast}
	}
	return nil
}

// operandTypeName names a type as beanquery's operator errors do
// (types.name): lower-cased, and NULL for the NULL literal.
func operandTypeName(t dtype) string {
	if t == tNull {
		return "NULL"
	}
	return strings.ToLower(t.String())
}

// decimalLiteral renders a decimal constant with the digits it was written
// with, like Python's str(Decimal): 2500.00 stays "2500.00".
func decimalLiteral(d decimal.Decimal) string {
	return d.StringFixed(max(-d.Exponent(), 0))
}

// pyOpClasses names beanquery's parser node class for each operator.
var pyOpClasses = map[bql.TokenType]string{
	bql.AND:      "And",
	bql.OR:       "Or",
	bql.EQ:       "Equal",
	bql.NE:       "NotEqual",
	bql.GT:       "Greater",
	bql.GTE:      "GreaterEq",
	bql.LT:       "Less",
	bql.LTE:      "LessEq",
	bql.TILDE:    "Match",
	bql.IN:       "In",
	bql.ASTERISK: "Mul",
	bql.SLASH:    "Div",
	bql.PLUS:     "Add",
	bql.MINUS:    "Sub",
}

// pyExprRepr renders a clause item like Python's repr() of beanquery's
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
		return fmt.Sprintf("%s(operand=%s)", unaryOperators[node.Op].pyClass, pyExprRepr(node.X))
	case *bql.Asterisk:
		return "Asterisk()"
	case *bql.ColumnIndex:
		// beanquery's grammar keeps a column index as a Python int.
		return strconv.FormatInt(node.Value, 10)
	case *bql.IsNull:
		if node.Not {
			return fmt.Sprintf("IsNotNull(operand=%s)", pyExprRepr(node.X))
		}
		return fmt.Sprintf("IsNull(operand=%s)", pyExprRepr(node.X))
	case *bql.Binary:
		if node.Op == bql.AND || node.Op == bql.OR {
			args := make([]string, 0, 2)
			for _, arg := range logicalArgs(node) {
				args = append(args, pyExprRepr(arg))
			}
			return fmt.Sprintf("%s(args=[%s])", pyOpClasses[node.Op], strings.Join(args, ", "))
		}
		return fmt.Sprintf("%s(left=%s, right=%s)", pyOpClasses[node.Op], pyExprRepr(node.L), pyExprRepr(node.R))
	case *bql.Attribute:
		return fmt.Sprintf("Attribute(operand=%s, name=%s)", pyExprRepr(node.X), pyrepr.String(node.Name))
	case *bql.Subscript:
		return fmt.Sprintf("Subscript(operand=%s, key=%s)", pyExprRepr(node.X), pyrepr.String(node.Key))
	}
	return fmt.Sprintf("Constant(value=%s)", pyConstantRepr(e))
}

// logicalArgs lists the operands of an AND or OR chain as beanquery's
// parser collects them: a chain written without parentheses is one node,
// so a left operand of the same operator starting where the chain starts
// joins it.
func logicalArgs(node *bql.Binary) []bql.Expr {
	start, _ := node.Span()
	if left, ok := node.L.(*bql.Binary); ok && left.Op == node.Op {
		if leftStart, _ := left.Span(); leftStart == start {
			return append(logicalArgs(left), node.R)
		}
	}
	return []bql.Expr{node.L, node.R}
}

// pyConstantRepr renders a constant like Python's repr() of its value.
func pyConstantRepr(e bql.Expr) string {
	return pyValueRepr(constantValue(e))
}

// constantValue is the value of a literal or a list constant.
func constantValue(e bql.Expr) any {
	switch node := e.(type) {
	case *bql.Int:
		return node.Value
	case *bql.Dec:
		return node.Value
	case *bql.Str:
		return node.Value
	case *bql.DateLit:
		return node.Value
	case *bql.Bool:
		return node.Value
	case *bql.List:
		values := make(listValue, len(node.Items))
		for i, item := range node.Items {
			values[i] = constantValue(item)
		}
		return values
	}
	return nil
}

// exprKey builds a canonical key for structural expression matching, used
// to resolve GROUP-BY and ORDER-BY items against the targets list.
func exprKey(e bql.Expr) string {
	return pyExprRepr(e)
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

// eval evaluates every argument, then, like beanquery's functions, gives
// NULL when any of them is NULL.
func (c *cCall) eval(row *evalRow) any {
	args := make([]any, len(c.args))
	for i, arg := range c.args {
		args[i] = arg.eval(row)
	}
	if slices.Contains(args, nil) {
		return nil
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

// cUnary is a typed unary operation.
type cUnary struct {
	sig *unarySignature
	x   cexpr
}

func (c *cUnary) typ() dtype { return c.sig.result }

func (c *cUnary) eval(row *evalRow) any {
	v := c.x.eval(row)
	if v == nil && !c.sig.nullSafe {
		return nil
	}
	return c.sig.eval(v)
}

// cIsNull is X IS NULL, or X IS NOT NULL.
type cIsNull struct {
	x   cexpr
	not bool
}

func (c *cIsNull) typ() dtype { return tBool }
func (c *cIsNull) eval(row *evalRow) any {
	return (c.x.eval(row) == nil) != c.not
}

// cOperator is a typed binary operation; like beanquery's, it is NULL when
// either operand is.
type cOperator struct {
	sig  *opSignature
	l, r cexpr
	// bound is the operation on a literal right operand, prepared once.
	bound func(l any) any
}

func newOperator(sig *opSignature, l, r cexpr) *cOperator {
	op := &cOperator{sig: sig, l: l, r: r}
	if lit, ok := r.(*cLiteral); ok && sig.bind != nil && lit.v != nil {
		op.bound = sig.bind(lit.v)
	}
	return op
}

func (c *cOperator) typ() dtype { return c.sig.result }

func (c *cOperator) eval(row *evalRow) any {
	l := c.l.eval(row)
	if l == nil {
		return nil
	}
	if c.bound != nil {
		return c.bound(l)
	}
	r := c.r.eval(row)
	if r == nil {
		return nil
	}
	return c.sig.eval(l, r)
}

// cCast is the cast beanquery applies to an untyped operand.
type cCast struct {
	x    cexpr
	to   dtype
	cast func(any) any
}

func (c *cCast) typ() dtype { return c.to }

func (c *cCast) eval(row *evalRow) any {
	v := c.x.eval(row)
	if v == nil {
		return nil
	}
	return c.cast(v)
}
