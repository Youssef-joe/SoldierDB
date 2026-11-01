package sql

import "strings"

// Statement is a parsed SQL statement.
type Statement interface {
	statement()
}

// Expr is a parsed SQL expression.
type Expr interface {
	expression()
}

// ColumnDef declares a column in CREATE TABLE.
type ColumnDef struct {
	Name       string
	Type       DataType
	PrimaryKey bool
	NotNull    bool
	Unique     bool
	Default    Expr
}

// CreateTable is CREATE TABLE.
type CreateTable struct {
	Name        string
	IfNotExists bool
	Columns     []ColumnDef
}

func (*CreateTable) statement() {}

// DropTable is DROP TABLE.
type DropTable struct {
	Name     string
	IfExists bool
}

func (*DropTable) statement() {}

// Insert is INSERT INTO.
type Insert struct {
	Table   string
	Columns []string
	Rows    [][]Expr
}

func (*Insert) statement() {}

// SelectItem is one projection in SELECT.
type SelectItem struct {
	Star  bool
	Table string
	Expr  Expr
	Alias string
}

// OrderItem is one ORDER BY term.
type OrderItem struct {
	Expr Expr
	Desc bool
}

// Select is SELECT.
type Select struct {
	Distinct bool
	Items    []SelectItem
	Table    string
	Where    Expr
	GroupBy  []Expr
	Having   Expr
	OrderBy  []OrderItem
	Limit    *int
	Offset   *int
}

func (*Select) statement() {}

// Assignment is one SET term in UPDATE.
type Assignment struct {
	Column string
	Value  Expr
}

// Update is UPDATE.
type Update struct {
	Table       string
	Assignments []Assignment
	Where       Expr
}

func (*Update) statement() {}

// Delete is DELETE FROM.
type Delete struct {
	Table string
	Where Expr
}

func (*Delete) statement() {}

// Begin is BEGIN.
type Begin struct{}

func (*Begin) statement() {}

// Commit is COMMIT.
type Commit struct{}

func (*Commit) statement() {}

// Rollback is ROLLBACK.
type Rollback struct{}

func (*Rollback) statement() {}

// ShowTables is SHOW TABLES.
type ShowTables struct{}

func (*ShowTables) statement() {}

// Describe is DESCRIBE table.
type Describe struct {
	Table string
}

func (*Describe) statement() {}

// Literal is a constant.
type Literal struct {
	Value Value
}

func (*Literal) expression() {}

// ColumnRef is a column reference.
type ColumnRef struct {
	Table string
	Name  string
}

func (*ColumnRef) expression() {}

// Binary is a binary operation.
type Binary struct {
	Op    string
	Left  Expr
	Right Expr
}

func (*Binary) expression() {}

// Unary is a unary operation.
type Unary struct {
	Op      string
	Operand Expr
}

func (*Unary) expression() {}

// FuncCall is a scalar or aggregate function call.
type FuncCall struct {
	Name string
	Args []Expr
	Star bool
}

func (*FuncCall) expression() {}

// IsNull is IS [NOT] NULL.
type IsNull struct {
	Operand Expr
	Not     bool
}

func (*IsNull) expression() {}

// In is [NOT] IN.
type In struct {
	Operand Expr
	List    []Expr
	Not     bool
}

func (*In) expression() {}

// Between is [NOT] BETWEEN.
type Between struct {
	Operand Expr
	Low     Expr
	High    Expr
	Not     bool
}

func (*Between) expression() {}

// Like is [NOT] LIKE.
type Like struct {
	Operand Expr
	Pattern Expr
	Not     bool
}

func (*Like) expression() {}

// FormatExpr renders an expression as SQL text.
func FormatExpr(e Expr) string {
	switch x := e.(type) {
	case *Literal:
		return x.Value.Literal()
	case *ColumnRef:
		if x.Table != "" {
			return x.Table + "." + x.Name
		}
		return x.Name
	case *Binary:
		return FormatExpr(x.Left) + " " + x.Op + " " + FormatExpr(x.Right)
	case *Unary:
		if x.Op == "NOT" {
			return "NOT " + FormatExpr(x.Operand)
		}
		return x.Op + FormatExpr(x.Operand)
	case *FuncCall:
		if x.Star {
			return x.Name + "(*)"
		}
		args := make([]string, len(x.Args))
		for i, arg := range x.Args {
			args[i] = FormatExpr(arg)
		}
		return x.Name + "(" + strings.Join(args, ", ") + ")"
	case *IsNull:
		if x.Not {
			return FormatExpr(x.Operand) + " IS NOT NULL"
		}
		return FormatExpr(x.Operand) + " IS NULL"
	case *In:
		items := make([]string, len(x.List))
		for i, item := range x.List {
			items[i] = FormatExpr(item)
		}
		op := " IN ("
		if x.Not {
			op = " NOT IN ("
		}
		return FormatExpr(x.Operand) + op + strings.Join(items, ", ") + ")"
	case *Between:
		op := " BETWEEN "
		if x.Not {
			op = " NOT BETWEEN "
		}
		return FormatExpr(x.Operand) + op + FormatExpr(x.Low) + " AND " + FormatExpr(x.High)
	case *Like:
		op := " LIKE "
		if x.Not {
			op = " NOT LIKE "
		}
		return FormatExpr(x.Operand) + op + FormatExpr(x.Pattern)
	default:
		return "?"
	}
}
