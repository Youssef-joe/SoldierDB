package sql

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// evalContext supplies the row and optional group a expression is evaluated
// against. A zero context is valid and only permits constant expressions; the
// catalog uses it to evaluate DEFAULT values.
type evalContext struct {
	table *Table
	row   []Value
	rows  [][]Value
	group bool
}

// evalExpr evaluates e in ctx.
func evalExpr(e Expr, ctx *evalContext) (Value, error) {
	switch x := e.(type) {
	case *Literal:
		return x.Value, nil

	case *ColumnRef:
		if ctx == nil || ctx.table == nil || ctx.row == nil {
			return Value{}, fmt.Errorf("sql: column %q is not available in this context", x.Name)
		}
		if x.Table != "" && !strings.EqualFold(x.Table, ctx.table.Name) {
			return Value{}, fmt.Errorf("sql: unknown table %q", x.Table)
		}
		idx, ok := ctx.table.ColumnIndex(x.Name)
		if !ok {
			return Value{}, fmt.Errorf("%w: %s", ErrColumnNotFound, x.Name)
		}
		return ctx.row[idx], nil

	case *Unary:
		operand, err := evalExpr(x.Operand, ctx)
		if err != nil {
			return Value{}, err
		}
		switch strings.ToUpper(x.Op) {
		case "NOT":
			t, ok := Truthy(operand)
			if !ok {
				return NullValue(), nil
			}
			return BoolValue(!t), nil
		case "-":
			if operand.Null {
				return NullValue(), nil
			}
			switch operand.Type {
			case IntegerType:
				return IntValue(-operand.Int), nil
			case RealType:
				return RealValue(-operand.Real), nil
			default:
				return Value{}, fmt.Errorf("sql: cannot negate %s", operand.Type)
			}
		case "+":
			return operand, nil
		default:
			return Value{}, fmt.Errorf("sql: unknown unary operator %q", x.Op)
		}

	case *Binary:
		return evalBinary(x, ctx)

	case *IsNull:
		operand, err := evalExpr(x.Operand, ctx)
		if err != nil {
			return Value{}, err
		}
		isNull := operand.Null
		if x.Not {
			isNull = !isNull
		}
		return BoolValue(isNull), nil

	case *In:
		operand, err := evalExpr(x.Operand, ctx)
		if err != nil {
			return Value{}, err
		}
		if operand.Null {
			return NullValue(), nil
		}
		sawNull := false
		for _, item := range x.List {
			value, err := evalExpr(item, ctx)
			if err != nil {
				return Value{}, err
			}
			if value.Null {
				sawNull = true
				continue
			}
			cmp, err := CompareValues(operand, value)
			if err != nil {
				return Value{}, err
			}
			if cmp == 0 {
				return BoolValue(!x.Not), nil
			}
		}
		if sawNull {
			return NullValue(), nil
		}
		return BoolValue(x.Not), nil

	case *Between:
		operand, err := evalExpr(x.Operand, ctx)
		if err != nil {
			return Value{}, err
		}
		low, err := evalExpr(x.Low, ctx)
		if err != nil {
			return Value{}, err
		}
		high, err := evalExpr(x.High, ctx)
		if err != nil {
			return Value{}, err
		}
		if operand.Null || low.Null || high.Null {
			return NullValue(), nil
		}
		lo, err := CompareValues(operand, low)
		if err != nil {
			return Value{}, err
		}
		hi, err := CompareValues(operand, high)
		if err != nil {
			return Value{}, err
		}
		inside := lo >= 0 && hi <= 0
		if x.Not {
			inside = !inside
		}
		return BoolValue(inside), nil

	case *Like:
		operand, err := evalExpr(x.Operand, ctx)
		if err != nil {
			return Value{}, err
		}
		pattern, err := evalExpr(x.Pattern, ctx)
		if err != nil {
			return Value{}, err
		}
		if operand.Null || pattern.Null {
			return NullValue(), nil
		}
		if operand.Type != TextType || pattern.Type != TextType {
			return Value{}, fmt.Errorf("sql: LIKE requires TEXT operands")
		}
		matched := likeMatch(operand.Str, pattern.Str)
		if x.Not {
			matched = !matched
		}
		return BoolValue(matched), nil

	case *FuncCall:
		if isAggregateName(x.Name) {
			if ctx == nil || !ctx.group {
				return Value{}, fmt.Errorf("sql: aggregate %s is not allowed here", x.Name)
			}
			return evalAggregate(x, ctx)
		}
		return evalScalar(x, ctx)

	default:
		return Value{}, fmt.Errorf("sql: unsupported expression %T", e)
	}
}

func evalBinary(x *Binary, ctx *evalContext) (Value, error) {
	op := strings.ToUpper(x.Op)

	left, err := evalExpr(x.Left, ctx)
	if err != nil {
		return Value{}, err
	}

	// AND/OR use three-valued logic and short-circuit when the result is
	// already determined.
	switch op {
	case "AND":
		lt, lok := Truthy(left)
		if lok && !lt {
			return BoolValue(false), nil
		}
		right, err := evalExpr(x.Right, ctx)
		if err != nil {
			return Value{}, err
		}
		rt, rok := Truthy(right)
		if rok && !rt {
			return BoolValue(false), nil
		}
		if !lok || !rok {
			return NullValue(), nil
		}
		return BoolValue(true), nil
	case "OR":
		lt, lok := Truthy(left)
		if lok && lt {
			return BoolValue(true), nil
		}
		right, err := evalExpr(x.Right, ctx)
		if err != nil {
			return Value{}, err
		}
		rt, rok := Truthy(right)
		if rok && rt {
			return BoolValue(true), nil
		}
		if !lok || !rok {
			return NullValue(), nil
		}
		return BoolValue(false), nil
	}

	right, err := evalExpr(x.Right, ctx)
	if err != nil {
		return Value{}, err
	}
	if left.Null || right.Null {
		return NullValue(), nil
	}

	switch op {
	case "=", "!=", "<", "<=", ">", ">=":
		cmp, err := CompareValues(left, right)
		if err != nil {
			return Value{}, err
		}
		switch op {
		case "=":
			return BoolValue(cmp == 0), nil
		case "!=":
			return BoolValue(cmp != 0), nil
		case "<":
			return BoolValue(cmp < 0), nil
		case "<=":
			return BoolValue(cmp <= 0), nil
		case ">":
			return BoolValue(cmp > 0), nil
		default:
			return BoolValue(cmp >= 0), nil
		}
	case "||":
		return TextValue(left.String() + right.String()), nil
	case "+", "-", "*", "/", "%":
		return evalArithmetic(op, left, right)
	default:
		return Value{}, fmt.Errorf("sql: unknown operator %q", x.Op)
	}
}

func evalArithmetic(op string, a, b Value) (Value, error) {
	if !a.numeric() || !b.numeric() {
		return Value{}, fmt.Errorf("sql: operator %s requires numeric operands", op)
	}

	if a.Type == IntegerType && b.Type == IntegerType {
		switch op {
		case "+":
			return IntValue(a.Int + b.Int), nil
		case "-":
			return IntValue(a.Int - b.Int), nil
		case "*":
			return IntValue(a.Int * b.Int), nil
		case "/":
			if b.Int == 0 {
				return Value{}, fmt.Errorf("sql: division by zero")
			}
			return IntValue(a.Int / b.Int), nil
		case "%":
			if b.Int == 0 {
				return Value{}, fmt.Errorf("sql: division by zero")
			}
			return IntValue(a.Int % b.Int), nil
		}
	}

	af, bf := a.asFloat(), b.asFloat()
	switch op {
	case "+":
		return RealValue(af + bf), nil
	case "-":
		return RealValue(af - bf), nil
	case "*":
		return RealValue(af * bf), nil
	case "/":
		if bf == 0 {
			return Value{}, fmt.Errorf("sql: division by zero")
		}
		return RealValue(af / bf), nil
	case "%":
		if bf == 0 {
			return Value{}, fmt.Errorf("sql: division by zero")
		}
		return RealValue(math.Mod(af, bf)), nil
	}
	return Value{}, fmt.Errorf("sql: unknown operator %q", op)
}

// isAggregateName reports whether name is an aggregate function.
func isAggregateName(name string) bool {
	switch strings.ToUpper(name) {
	case "COUNT", "SUM", "AVG", "MIN", "MAX":
		return true
	default:
		return false
	}
}

// isScalarName reports whether name is a supported scalar function.
func isScalarName(name string) bool {
	switch strings.ToUpper(name) {
	case "UPPER", "LOWER", "LENGTH", "TRIM", "ABS", "ROUND", "COALESCE", "IFNULL", "NULLIF",
		"VECTOR", "COSINE_DISTANCE", "EUCLIDEAN_DISTANCE", "INNER_PRODUCT":
		return true
	default:
		return false
	}
}

// validateExpr checks column references and function names against a schema.
// It runs before any rows are read so unknown columns fail even on an empty
// table. A nil table means the expression is evaluated without a FROM clause.
func validateExpr(e Expr, table *Table) error {
	switch x := e.(type) {
	case *Literal:
		return nil
	case *ColumnRef:
		if table == nil {
			return fmt.Errorf("%w: %s (no FROM clause)", ErrColumnNotFound, x.Name)
		}
		if x.Table != "" && !strings.EqualFold(x.Table, table.Name) {
			return fmt.Errorf("sql: unknown table %q", x.Table)
		}
		if _, ok := table.ColumnIndex(x.Name); !ok {
			return fmt.Errorf("%w: %s", ErrColumnNotFound, x.Name)
		}
		return nil
	case *Binary:
		if err := validateExpr(x.Left, table); err != nil {
			return err
		}
		return validateExpr(x.Right, table)
	case *Unary:
		return validateExpr(x.Operand, table)
	case *FuncCall:
		if isAggregateName(x.Name) {
			if x.Star {
				if strings.ToUpper(x.Name) != "COUNT" {
					return fmt.Errorf("sql: %s(*) is not valid", x.Name)
				}
				return nil
			}
			if len(x.Args) != 1 {
				return fmt.Errorf("sql: %s expects exactly one argument", x.Name)
			}
			name := strings.ToUpper(x.Name)
			if name == "SUM" || name == "AVG" {
				if typ, ok := staticType(x.Args[0], table); ok && typ != IntegerType && typ != RealType {
					return fmt.Errorf("sql: %s requires a numeric argument", name)
				}
			}
			return validateExpr(x.Args[0], table)
		}
		if !isScalarName(x.Name) {
			return fmt.Errorf("sql: unknown function %q", x.Name)
		}
		for _, arg := range x.Args {
			if err := validateExpr(arg, table); err != nil {
				return err
			}
		}
		return nil
	case *IsNull:
		return validateExpr(x.Operand, table)
	case *In:
		if err := validateExpr(x.Operand, table); err != nil {
			return err
		}
		for _, item := range x.List {
			if err := validateExpr(item, table); err != nil {
				return err
			}
		}
		return nil
	case *Between:
		if err := validateExpr(x.Operand, table); err != nil {
			return err
		}
		if err := validateExpr(x.Low, table); err != nil {
			return err
		}
		return validateExpr(x.High, table)
	case *Like:
		if err := validateExpr(x.Operand, table); err != nil {
			return err
		}
		return validateExpr(x.Pattern, table)
	default:
		return fmt.Errorf("sql: unsupported expression %T", e)
	}
}

func staticType(e Expr, table *Table) (DataType, bool) {
	switch x := e.(type) {
	case *Literal:
		if x.Value.Null {
			return 0, false
		}
		return x.Value.Type, true
	case *ColumnRef:
		if table == nil {
			return 0, false
		}
		if idx, ok := table.ColumnIndex(x.Name); ok {
			return table.Columns[idx].Type, true
		}
	}
	return 0, false
}

// containsAggregate reports whether e contains an aggregate function call.
func containsAggregate(e Expr) bool {
	switch x := e.(type) {
	case *FuncCall:
		if isAggregateName(x.Name) {
			return true
		}
		for _, arg := range x.Args {
			if containsAggregate(arg) {
				return true
			}
		}
	case *Binary:
		return containsAggregate(x.Left) || containsAggregate(x.Right)
	case *Unary:
		return containsAggregate(x.Operand)
	case *IsNull:
		return containsAggregate(x.Operand)
	case *In:
		if containsAggregate(x.Operand) {
			return true
		}
		for _, item := range x.List {
			if containsAggregate(item) {
				return true
			}
		}
	case *Between:
		return containsAggregate(x.Operand) || containsAggregate(x.Low) || containsAggregate(x.High)
	case *Like:
		return containsAggregate(x.Operand) || containsAggregate(x.Pattern)
	}
	return false
}

func evalAggregate(call *FuncCall, ctx *evalContext) (Value, error) {
	name := strings.ToUpper(call.Name)
	if name == "COUNT" && call.Star {
		return IntValue(int64(len(ctx.rows))), nil
	}
	if len(call.Args) != 1 {
		return Value{}, fmt.Errorf("sql: %s expects exactly one argument", name)
	}
	arg := call.Args[0]
	if containsAggregate(arg) {
		return Value{}, fmt.Errorf("sql: aggregate functions cannot be nested")
	}

	child := &evalContext{table: ctx.table}
	var (
		count   int64
		sumInt  int64
		sumF    float64
		isReal  bool
		best    Value
		hasBest bool
	)

	for _, row := range ctx.rows {
		child.row = row
		v, err := evalExpr(arg, child)
		if err != nil {
			return Value{}, err
		}
		if v.Null {
			continue
		}
		switch name {
		case "COUNT":
			count++
		case "SUM", "AVG":
			if !v.numeric() {
				return Value{}, fmt.Errorf("sql: %s requires a numeric argument", name)
			}
			count++
			if v.Type == RealType {
				isReal = true
			}
			sumInt += v.Int
			sumF += v.asFloat()
		case "MIN", "MAX":
			if !hasBest {
				best = v
				hasBest = true
				continue
			}
			cmp, err := CompareValues(v, best)
			if err != nil {
				return Value{}, err
			}
			if (name == "MIN" && cmp < 0) || (name == "MAX" && cmp > 0) {
				best = v
			}
		}
	}

	switch name {
	case "COUNT":
		return IntValue(count), nil
	case "SUM":
		if count == 0 {
			return NullValue(), nil
		}
		if isReal {
			return RealValue(sumF), nil
		}
		return IntValue(sumInt), nil
	case "AVG":
		if count == 0 {
			return NullValue(), nil
		}
		return RealValue(sumF / float64(count)), nil
	case "MIN", "MAX":
		if !hasBest {
			return NullValue(), nil
		}
		return best, nil
	}
	return Value{}, fmt.Errorf("sql: unknown aggregate %q", call.Name)
}

func evalScalar(call *FuncCall, ctx *evalContext) (Value, error) {
	name := strings.ToUpper(call.Name)
	args := make([]Value, len(call.Args))
	for i, arg := range call.Args {
		v, err := evalExpr(arg, ctx)
		if err != nil {
			return Value{}, err
		}
		args[i] = v
	}

	switch name {
	case "VECTOR":
		if len(args) == 0 {
			return Value{}, fmt.Errorf("sql: VECTOR expects at least one numeric argument")
		}
		vector := make([]float32, len(args))
		for i, arg := range args {
			if arg.Null {
				return NullValue(), nil
			}
			if !arg.numeric() {
				return Value{}, fmt.Errorf("sql: VECTOR components must be numeric")
			}
			component := arg.asFloat()
			if math.IsNaN(component) || math.IsInf(component, 0) {
				return Value{}, fmt.Errorf("sql: VECTOR components must be finite")
			}
			vector[i] = float32(component)
		}
		return VectorValue(vector), nil

	case "COSINE_DISTANCE", "EUCLIDEAN_DISTANCE", "INNER_PRODUCT":
		if len(args) != 2 {
			return Value{}, fmt.Errorf("sql: %s expects two VECTOR arguments", name)
		}
		if args[0].Null || args[1].Null {
			return NullValue(), nil
		}
		if args[0].Type != VectorType || args[1].Type != VectorType {
			return Value{}, fmt.Errorf("sql: %s requires VECTOR arguments", name)
		}
		if len(args[0].Vector) != len(args[1].Vector) {
			return Value{}, fmt.Errorf("sql: %s requires vectors with the same dimension", name)
		}
		var dot, leftNorm, rightNorm, squared float64
		for i, left := range args[0].Vector {
			right := args[1].Vector[i]
			lf, rf := float64(left), float64(right)
			dot += lf * rf
			leftNorm += lf * lf
			rightNorm += rf * rf
			delta := lf - rf
			squared += delta * delta
		}
		switch name {
		case "INNER_PRODUCT":
			return RealValue(dot), nil
		case "EUCLIDEAN_DISTANCE":
			return RealValue(math.Sqrt(squared)), nil
		default:
			if leftNorm == 0 || rightNorm == 0 {
				return Value{}, fmt.Errorf("sql: COSINE_DISTANCE is undefined for a zero vector")
			}
			return RealValue(1 - dot/(math.Sqrt(leftNorm)*math.Sqrt(rightNorm))), nil
		}

	case "UPPER", "LOWER":
		if len(args) != 1 {
			return Value{}, fmt.Errorf("sql: %s expects one argument", name)
		}
		if args[0].Null {
			return NullValue(), nil
		}
		if args[0].Type != TextType {
			return Value{}, fmt.Errorf("sql: %s requires TEXT", name)
		}
		if name == "UPPER" {
			return TextValue(strings.ToUpper(args[0].Str)), nil
		}
		return TextValue(strings.ToLower(args[0].Str)), nil

	case "LENGTH":
		if len(args) != 1 {
			return Value{}, fmt.Errorf("sql: LENGTH expects one argument")
		}
		if args[0].Null {
			return NullValue(), nil
		}
		if args[0].Type != TextType {
			return Value{}, fmt.Errorf("sql: LENGTH requires TEXT")
		}
		return IntValue(int64(utf8.RuneCountInString(args[0].Str))), nil

	case "TRIM":
		if len(args) != 1 {
			return Value{}, fmt.Errorf("sql: TRIM expects one argument")
		}
		if args[0].Null {
			return NullValue(), nil
		}
		if args[0].Type != TextType {
			return Value{}, fmt.Errorf("sql: TRIM requires TEXT")
		}
		return TextValue(strings.TrimSpace(args[0].Str)), nil

	case "ABS":
		if len(args) != 1 {
			return Value{}, fmt.Errorf("sql: ABS expects one argument")
		}
		if args[0].Null {
			return NullValue(), nil
		}
		switch args[0].Type {
		case IntegerType:
			if args[0].Int < 0 {
				return IntValue(-args[0].Int), nil
			}
			return args[0], nil
		case RealType:
			return RealValue(math.Abs(args[0].Real)), nil
		default:
			return Value{}, fmt.Errorf("sql: ABS requires a numeric argument")
		}

	case "ROUND":
		if len(args) != 1 && len(args) != 2 {
			return Value{}, fmt.Errorf("sql: ROUND expects one or two arguments")
		}
		if args[0].Null || (len(args) == 2 && args[1].Null) {
			return NullValue(), nil
		}
		if !args[0].numeric() {
			return Value{}, fmt.Errorf("sql: ROUND requires a numeric argument")
		}
		digits := 0
		if len(args) == 2 {
			if args[1].Type != IntegerType {
				return Value{}, fmt.Errorf("sql: ROUND digits must be INTEGER")
			}
			digits = int(args[1].Int)
		}
		if args[0].Type == IntegerType && digits >= 0 {
			return args[0], nil
		}
		scale := math.Pow(10, float64(digits))
		return RealValue(math.Round(args[0].asFloat()*scale) / scale), nil

	case "COALESCE", "IFNULL":
		if len(args) == 0 {
			return Value{}, fmt.Errorf("sql: %s expects at least one argument", name)
		}
		for _, v := range args {
			if !v.Null {
				return v, nil
			}
		}
		return NullValue(), nil

	case "NULLIF":
		if len(args) != 2 {
			return Value{}, fmt.Errorf("sql: NULLIF expects two arguments")
		}
		if args[0].Null {
			return NullValue(), nil
		}
		if args[1].Null {
			return args[0], nil
		}
		cmp, err := CompareValues(args[0], args[1])
		if err != nil {
			return Value{}, err
		}
		if cmp == 0 {
			return NullValue(), nil
		}
		return args[0], nil

	case "CAST":
		return Value{}, fmt.Errorf("sql: CAST is not supported; use explicit type coercion")

	default:
		return Value{}, fmt.Errorf("sql: unknown function %q", call.Name)
	}
}

// likeMatch implements SQL LIKE with % and _ wildcards.
func likeMatch(s, pattern string) bool {
	sr := []rune(s)
	pr := []rune(pattern)
	// Dynamic programming over prefixes.
	memo := make([][]int8, len(sr)+1)
	for i := range memo {
		memo[i] = make([]int8, len(pr)+1)
		for j := range memo[i] {
			memo[i][j] = -1
		}
	}
	var match func(i, j int) bool
	match = func(i, j int) bool {
		if memo[i][j] != -1 {
			return memo[i][j] == 1
		}
		var result bool
		switch {
		case j == len(pr):
			result = i == len(sr)
		case pr[j] == '%':
			result = match(i, j+1) || (i < len(sr) && match(i+1, j))
		case pr[j] == '_':
			result = i < len(sr) && match(i+1, j+1)
		default:
			result = i < len(sr) && sr[i] == pr[j] && match(i+1, j+1)
		}
		if result {
			memo[i][j] = 1
		} else {
			memo[i][j] = 0
		}
		return result
	}
	return match(0, 0)
}

// parseOrdinal returns the integer value of a literal used for ORDER BY
// ordinals, or zero if expr is not an integer literal.
func parseOrdinal(e Expr) (int, bool) {
	lit, ok := e.(*Literal)
	if !ok || lit.Value.Null || lit.Value.Type != IntegerType {
		return 0, false
	}
	return int(lit.Value.Int), true
}
