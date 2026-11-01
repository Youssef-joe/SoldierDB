package sql

import (
	"fmt"
	"strconv"
	"strings"
)

// Parser is a recursive-descent SQL parser.
type Parser struct {
	lexer *Lexer
	tok   Token
	ahead []Token
}

// NewParser creates a parser and reads the first token.
func NewParser(input string) (*Parser, error) {
	p := &Parser{lexer: NewLexer(input)}
	if err := p.next(); err != nil {
		return nil, err
	}
	return p, nil
}

// Parse parses one or more semicolon-separated statements.
func Parse(input string) ([]Statement, error) {
	p, err := NewParser(input)
	if err != nil {
		return nil, err
	}
	var stmts []Statement
	for {
		for p.isSymbol(";") {
			if err := p.next(); err != nil {
				return nil, err
			}
		}
		if p.tok.Kind == TokenEOF {
			return stmts, nil
		}
		stmt, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, stmt)
		if p.isSymbol(";") {
			if err := p.next(); err != nil {
				return nil, err
			}
			continue
		}
		if p.tok.Kind != TokenEOF {
			return nil, p.errorf("expected ; or end of input, got %s", p.tok)
		}
	}
}

// ParseOne parses exactly one statement.
func ParseOne(input string) (Statement, error) {
	stmts, err := Parse(input)
	if err != nil {
		return nil, err
	}
	if len(stmts) == 0 {
		return nil, &SyntaxError{Pos: 0, Msg: "empty statement"}
	}
	if len(stmts) > 1 {
		return nil, &SyntaxError{Pos: 0, Msg: "expected one statement"}
	}
	return stmts[0], nil
}

func (p *Parser) next() error {
	if len(p.ahead) > 0 {
		p.tok = p.ahead[0]
		p.ahead = p.ahead[1:]
		return nil
	}
	tok, err := p.lexer.Next()
	if err != nil {
		return err
	}
	p.tok = tok
	return nil
}

func (p *Parser) peekN(n int) (Token, error) {
	for len(p.ahead) < n {
		tok, err := p.lexer.Next()
		if err != nil {
			return Token{}, err
		}
		p.ahead = append(p.ahead, tok)
	}
	return p.ahead[n-1], nil
}

func (p *Parser) errorf(format string, args ...any) error {
	return &SyntaxError{Pos: p.tok.Pos, Msg: fmt.Sprintf(format, args...)}
}

func (p *Parser) isKeyword(kw string) bool {
	return p.tok.Kind == TokenKeyword && p.tok.Text == kw
}

func (p *Parser) isSymbol(sym string) bool {
	return p.tok.Kind == TokenSymbol && p.tok.Text == sym
}

func (p *Parser) acceptKeyword(kw string) (bool, error) {
	if !p.isKeyword(kw) {
		return false, nil
	}
	return true, p.next()
}

func (p *Parser) acceptSymbol(sym string) (bool, error) {
	if !p.isSymbol(sym) {
		return false, nil
	}
	return true, p.next()
}

func (p *Parser) expectKeyword(kw string) error {
	if !p.isKeyword(kw) {
		return p.errorf("expected %s, got %s", kw, p.tok)
	}
	return p.next()
}

func (p *Parser) expectSymbol(sym string) error {
	if !p.isSymbol(sym) {
		return p.errorf("expected %q, got %s", sym, p.tok)
	}
	return p.next()
}

func (p *Parser) parseName() (string, error) {
	if p.tok.Kind != TokenIdent && p.tok.Kind != TokenKeyword {
		return "", p.errorf("expected a name, got %s", p.tok)
	}
	name := p.tok.Text
	if err := p.next(); err != nil {
		return "", err
	}
	return name, nil
}

func (p *Parser) parseStatement() (Statement, error) {
	if p.tok.Kind == TokenKeyword {
		switch p.tok.Text {
		case "CREATE":
			return p.parseCreate()
		case "DROP":
			return p.parseDrop()
		case "INSERT":
			return p.parseInsert()
		case "SELECT":
			return p.parseSelect()
		case "UPDATE":
			return p.parseUpdate()
		case "DELETE":
			return p.parseDelete()
		case "BEGIN":
			if err := p.next(); err != nil {
				return nil, err
			}
			_, err := p.acceptKeyword("TRANSACTION")
			return &Begin{}, err
		case "COMMIT":
			if err := p.next(); err != nil {
				return nil, err
			}
			_, err := p.acceptKeyword("TRANSACTION")
			return &Commit{}, err
		case "ROLLBACK":
			if err := p.next(); err != nil {
				return nil, err
			}
			_, err := p.acceptKeyword("TRANSACTION")
			return &Rollback{}, err
		case "SHOW":
			if err := p.next(); err != nil {
				return nil, err
			}
			if err := p.expectKeyword("TABLES"); err != nil {
				return nil, err
			}
			return &ShowTables{}, nil
		case "DESCRIBE", "DESC":
			if err := p.next(); err != nil {
				return nil, err
			}
			name, err := p.parseName()
			if err != nil {
				return nil, err
			}
			return &Describe{Table: name}, nil
		}
	}
	return nil, p.errorf("unexpected %s at start of statement", p.tok)
}

func (p *Parser) parseCreate() (Statement, error) {
	if err := p.next(); err != nil { // CREATE
		return nil, err
	}
	if !p.isKeyword("TABLE") {
		return nil, p.errorf("only CREATE TABLE is supported")
	}
	if err := p.next(); err != nil {
		return nil, err
	}

	stmt := &CreateTable{}
	if p.isKeyword("IF") {
		if err := p.next(); err != nil {
			return nil, err
		}
		if err := p.expectKeyword("NOT"); err != nil {
			return nil, err
		}
		if err := p.expectKeyword("EXISTS"); err != nil {
			return nil, err
		}
		stmt.IfNotExists = true
	}

	name, err := p.parseName()
	if err != nil {
		return nil, err
	}
	stmt.Name = name

	if err := p.expectSymbol("("); err != nil {
		return nil, err
	}

	var tablePK string
	for {
		if p.isKeyword("PRIMARY") {
			if tablePK != "" {
				return nil, p.errorf("multiple PRIMARY KEY declarations")
			}
			if err := p.next(); err != nil {
				return nil, err
			}
			if err := p.expectKeyword("KEY"); err != nil {
				return nil, err
			}
			if err := p.expectSymbol("("); err != nil {
				return nil, err
			}
			tablePK, err = p.parseName()
			if err != nil {
				return nil, err
			}
			if p.isSymbol(",") {
				return nil, p.errorf("composite PRIMARY KEY is not supported")
			}
			if err := p.expectSymbol(")"); err != nil {
				return nil, err
			}
		} else {
			col, err := p.parseColumnDef()
			if err != nil {
				return nil, err
			}
			stmt.Columns = append(stmt.Columns, col)
		}
		if p.isSymbol(",") {
			if err := p.next(); err != nil {
				return nil, err
			}
			continue
		}
		break
	}
	if err := p.expectSymbol(")"); err != nil {
		return nil, err
	}

	if tablePK != "" {
		found := false
		for i := range stmt.Columns {
			if stmt.Columns[i].Name == tablePK {
				stmt.Columns[i].PrimaryKey = true
				found = true
			}
		}
		if !found {
			return nil, p.errorf("PRIMARY KEY column %q is not defined", tablePK)
		}
	}
	return stmt, nil
}

func (p *Parser) parseColumnDef() (ColumnDef, error) {
	var col ColumnDef
	name, err := p.parseName()
	if err != nil {
		return col, err
	}
	col.Name = name

	if p.tok.Kind != TokenKeyword && p.tok.Kind != TokenIdent {
		return col, p.errorf("expected a column type, got %s", p.tok)
	}
	typName := p.tok.Text
	typ, ok := ParseDataType(typName)
	if !ok {
		return col, p.errorf("unknown column type %q", typName)
	}
	col.Type = typ
	if err := p.next(); err != nil {
		return col, err
	}
	if p.isSymbol("(") { // VARCHAR(n) or VECTOR(n)
		if err := p.next(); err != nil {
			return col, err
		}
		if p.tok.Kind != TokenInt {
			return col, p.errorf("expected a length")
		}
		length, parseErr := strconv.Atoi(p.tok.Text)
		if parseErr != nil {
			return col, p.errorf("invalid type length %q", p.tok.Text)
		}
		if typ == VectorType {
			if length <= 0 {
				return col, p.errorf("VECTOR dimension must be positive")
			}
			col.VectorDim = length
		}
		if err := p.next(); err != nil {
			return col, err
		}
		if err := p.expectSymbol(")"); err != nil {
			return col, err
		}
	}
	if typ == VectorType && col.VectorDim == 0 {
		return col, p.errorf("VECTOR requires a dimension, for example VECTOR(3)")
	}

	for {
		switch {
		case p.isKeyword("PRIMARY"):
			if err := p.next(); err != nil {
				return col, err
			}
			if err := p.expectKeyword("KEY"); err != nil {
				return col, err
			}
			col.PrimaryKey = true
		case p.isKeyword("NOT"):
			if err := p.next(); err != nil {
				return col, err
			}
			if err := p.expectKeyword("NULL"); err != nil {
				return col, err
			}
			col.NotNull = true
		case p.isKeyword("UNIQUE"):
			if err := p.next(); err != nil {
				return col, err
			}
			col.Unique = true
		case p.isKeyword("DEFAULT"):
			if err := p.next(); err != nil {
				return col, err
			}
			expr, err := p.parseExpr()
			if err != nil {
				return col, err
			}
			col.Default = expr
		case p.isKeyword("NULL"):
			if err := p.next(); err != nil {
				return col, err
			}
		default:
			return col, nil
		}
	}
}

func (p *Parser) parseDrop() (Statement, error) {
	if err := p.next(); err != nil {
		return nil, err
	}
	if err := p.expectKeyword("TABLE"); err != nil {
		return nil, err
	}
	stmt := &DropTable{}
	if p.isKeyword("IF") {
		if err := p.next(); err != nil {
			return nil, err
		}
		if err := p.expectKeyword("EXISTS"); err != nil {
			return nil, err
		}
		stmt.IfExists = true
	}
	name, err := p.parseName()
	if err != nil {
		return nil, err
	}
	stmt.Name = name
	return stmt, nil
}

func (p *Parser) parseInsert() (Statement, error) {
	if err := p.next(); err != nil { // INSERT
		return nil, err
	}
	if err := p.expectKeyword("INTO"); err != nil {
		return nil, err
	}
	name, err := p.parseName()
	if err != nil {
		return nil, err
	}
	stmt := &Insert{Table: name}

	if p.isSymbol("(") {
		if err := p.next(); err != nil {
			return nil, err
		}
		for {
			col, err := p.parseName()
			if err != nil {
				return nil, err
			}
			stmt.Columns = append(stmt.Columns, col)
			if p.isSymbol(",") {
				if err := p.next(); err != nil {
					return nil, err
				}
				continue
			}
			break
		}
		if err := p.expectSymbol(")"); err != nil {
			return nil, err
		}
	}

	if err := p.expectKeyword("VALUES"); err != nil {
		return nil, err
	}
	for {
		if err := p.expectSymbol("("); err != nil {
			return nil, err
		}
		var row []Expr
		if !p.isSymbol(")") {
			for {
				expr, err := p.parseExpr()
				if err != nil {
					return nil, err
				}
				row = append(row, expr)
				if p.isSymbol(",") {
					if err := p.next(); err != nil {
						return nil, err
					}
					continue
				}
				break
			}
		}
		if err := p.expectSymbol(")"); err != nil {
			return nil, err
		}
		stmt.Rows = append(stmt.Rows, row)
		if p.isSymbol(",") {
			if err := p.next(); err != nil {
				return nil, err
			}
			continue
		}
		break
	}
	return stmt, nil
}

func (p *Parser) parseSelect() (Statement, error) {
	if err := p.next(); err != nil { // SELECT
		return nil, err
	}
	stmt := &Select{}
	var err error
	if p.isKeyword("DISTINCT") {
		stmt.Distinct = true
		if err := p.next(); err != nil {
			return nil, err
		}
	}

	for {
		if p.isSymbol("*") {
			if err := p.next(); err != nil {
				return nil, err
			}
			stmt.Items = append(stmt.Items, SelectItem{Star: true})
		} else {
			expr, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			item := SelectItem{Expr: expr}
			if p.isKeyword("AS") {
				if err := p.next(); err != nil {
					return nil, err
				}
				alias, err := p.parseName()
				if err != nil {
					return nil, err
				}
				item.Alias = alias
			} else if p.tok.Kind == TokenIdent {
				item.Alias = p.tok.Text
				if err := p.next(); err != nil {
					return nil, err
				}
			}
			stmt.Items = append(stmt.Items, item)
		}
		if p.isSymbol(",") {
			if err := p.next(); err != nil {
				return nil, err
			}
			continue
		}
		break
	}

	if p.isKeyword("FROM") {
		if err := p.next(); err != nil {
			return nil, err
		}
		table, err := p.parseName()
		if err != nil {
			return nil, err
		}
		stmt.Table = table
	}

	if p.isKeyword("WHERE") {
		if err := p.next(); err != nil {
			return nil, err
		}
		stmt.Where, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}

	if p.isKeyword("GROUP") {
		if err := p.next(); err != nil {
			return nil, err
		}
		if err := p.expectKeyword("BY"); err != nil {
			return nil, err
		}
		for {
			expr, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			stmt.GroupBy = append(stmt.GroupBy, expr)
			if p.isSymbol(",") {
				if err := p.next(); err != nil {
					return nil, err
				}
				continue
			}
			break
		}
	}

	if p.isKeyword("HAVING") {
		if err := p.next(); err != nil {
			return nil, err
		}
		stmt.Having, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}

	if p.isKeyword("ORDER") {
		if err := p.next(); err != nil {
			return nil, err
		}
		if err := p.expectKeyword("BY"); err != nil {
			return nil, err
		}
		for {
			expr, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			item := OrderItem{Expr: expr}
			if p.isKeyword("ASC") {
				if err := p.next(); err != nil {
					return nil, err
				}
			} else if p.isKeyword("DESC") {
				item.Desc = true
				if err := p.next(); err != nil {
					return nil, err
				}
			}
			stmt.OrderBy = append(stmt.OrderBy, item)
			if p.isSymbol(",") {
				if err := p.next(); err != nil {
					return nil, err
				}
				continue
			}
			break
		}
	}

	if p.isKeyword("LIMIT") {
		if err := p.next(); err != nil {
			return nil, err
		}
		n, err := p.parseInt()
		if err != nil {
			return nil, err
		}
		if p.isSymbol(",") {
			if err := p.next(); err != nil {
				return nil, err
			}
			m, err := p.parseInt()
			if err != nil {
				return nil, err
			}
			stmt.Offset = &n
			stmt.Limit = &m
		} else {
			stmt.Limit = &n
			if p.isKeyword("OFFSET") {
				if err := p.next(); err != nil {
					return nil, err
				}
				m, err := p.parseInt()
				if err != nil {
					return nil, err
				}
				stmt.Offset = &m
			}
		}
	}

	return stmt, nil
}

func (p *Parser) parseUpdate() (Statement, error) {
	if err := p.next(); err != nil { // UPDATE
		return nil, err
	}
	name, err := p.parseName()
	if err != nil {
		return nil, err
	}
	stmt := &Update{Table: name}
	if err := p.expectKeyword("SET"); err != nil {
		return nil, err
	}
	for {
		col, err := p.parseName()
		if err != nil {
			return nil, err
		}
		if err := p.expectSymbol("="); err != nil {
			return nil, err
		}
		value, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		stmt.Assignments = append(stmt.Assignments, Assignment{Column: col, Value: value})
		if p.isSymbol(",") {
			if err := p.next(); err != nil {
				return nil, err
			}
			continue
		}
		break
	}
	if p.isKeyword("WHERE") {
		if err := p.next(); err != nil {
			return nil, err
		}
		stmt.Where, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}
	return stmt, nil
}

func (p *Parser) parseDelete() (Statement, error) {
	if err := p.next(); err != nil { // DELETE
		return nil, err
	}
	if err := p.expectKeyword("FROM"); err != nil {
		return nil, err
	}
	name, err := p.parseName()
	if err != nil {
		return nil, err
	}
	stmt := &Delete{Table: name}
	if p.isKeyword("WHERE") {
		if err := p.next(); err != nil {
			return nil, err
		}
		stmt.Where, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}
	return stmt, nil
}

func (p *Parser) parseInt() (int, error) {
	if p.tok.Kind != TokenInt {
		return 0, p.errorf("expected an integer, got %s", p.tok)
	}
	n, err := strconv.Atoi(p.tok.Text)
	if err != nil {
		return 0, p.errorf("invalid integer %q", p.tok.Text)
	}
	if err := p.next(); err != nil {
		return 0, err
	}
	return n, nil
}

// Expression parsing.

func (p *Parser) parseExpr() (Expr, error) { return p.parseOr() }

func (p *Parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.isKeyword("OR") {
		if err := p.next(); err != nil {
			return nil, err
		}
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: "OR", Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseAnd() (Expr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.isKeyword("AND") {
		if err := p.next(); err != nil {
			return nil, err
		}
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: "AND", Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseNot() (Expr, error) {
	if p.isKeyword("NOT") {
		if err := p.next(); err != nil {
			return nil, err
		}
		operand, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &Unary{Op: "NOT", Operand: operand}, nil
	}
	return p.parseComparison()
}

func (p *Parser) parseComparison() (Expr, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}

	for {
		switch {
		case p.isSymbol("="), p.isSymbol("!="), p.isSymbol("<>"), p.isSymbol("<"),
			p.isSymbol("<="), p.isSymbol(">"), p.isSymbol(">="):
			op := p.tok.Text
			if op == "<>" {
				op = "!="
			}
			if err := p.next(); err != nil {
				return nil, err
			}
			right, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			left = &Binary{Op: op, Left: left, Right: right}
		case p.isKeyword("IS"):
			if err := p.next(); err != nil {
				return nil, err
			}
			not := false
			if p.isKeyword("NOT") {
				not = true
				if err := p.next(); err != nil {
					return nil, err
				}
			}
			if err := p.expectKeyword("NULL"); err != nil {
				return nil, err
			}
			left = &IsNull{Operand: left, Not: not}
		case p.isKeyword("IN"):
			if err := p.next(); err != nil {
				return nil, err
			}
			list, err := p.parseExprList()
			if err != nil {
				return nil, err
			}
			left = &In{Operand: left, List: list}
		case p.isKeyword("BETWEEN"):
			if err := p.next(); err != nil {
				return nil, err
			}
			low, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			if err := p.expectKeyword("AND"); err != nil {
				return nil, err
			}
			high, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			left = &Between{Operand: left, Low: low, High: high}
		case p.isKeyword("LIKE"):
			if err := p.next(); err != nil {
				return nil, err
			}
			pattern, err := p.parseAdditive()
			if err != nil {
				return nil, err
			}
			left = &Like{Operand: left, Pattern: pattern}
		case p.isKeyword("NOT"):
			next, err := p.peekN(1)
			if err != nil {
				return nil, err
			}
			if next.Kind != TokenKeyword {
				return left, nil
			}
			switch next.Text {
			case "IN":
				if err := p.next(); err != nil { // NOT
					return nil, err
				}
				if err := p.next(); err != nil { // IN
					return nil, err
				}
				list, err := p.parseExprList()
				if err != nil {
					return nil, err
				}
				left = &In{Operand: left, List: list, Not: true}
			case "BETWEEN":
				if err := p.next(); err != nil {
					return nil, err
				}
				if err := p.next(); err != nil {
					return nil, err
				}
				low, err := p.parseAdditive()
				if err != nil {
					return nil, err
				}
				if err := p.expectKeyword("AND"); err != nil {
					return nil, err
				}
				high, err := p.parseAdditive()
				if err != nil {
					return nil, err
				}
				left = &Between{Operand: left, Low: low, High: high, Not: true}
			case "LIKE":
				if err := p.next(); err != nil {
					return nil, err
				}
				if err := p.next(); err != nil {
					return nil, err
				}
				pattern, err := p.parseAdditive()
				if err != nil {
					return nil, err
				}
				left = &Like{Operand: left, Pattern: pattern, Not: true}
			default:
				return left, nil
			}
		default:
			return left, nil
		}
	}
}

func (p *Parser) parseExprList() ([]Expr, error) {
	if err := p.expectSymbol("("); err != nil {
		return nil, err
	}
	var list []Expr
	if !p.isSymbol(")") {
		for {
			expr, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			list = append(list, expr)
			if p.isSymbol(",") {
				if err := p.next(); err != nil {
					return nil, err
				}
				continue
			}
			break
		}
	}
	if err := p.expectSymbol(")"); err != nil {
		return nil, err
	}
	return list, nil
}

func (p *Parser) parseAdditive() (Expr, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for p.isSymbol("+") || p.isSymbol("-") {
		op := p.tok.Text
		if err := p.next(); err != nil {
			return nil, err
		}
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseMultiplicative() (Expr, error) {
	left, err := p.parseConcat()
	if err != nil {
		return nil, err
	}
	for p.isSymbol("*") || p.isSymbol("/") || p.isSymbol("%") {
		op := p.tok.Text
		if err := p.next(); err != nil {
			return nil, err
		}
		right, err := p.parseConcat()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseConcat() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.isSymbol("||") {
		if err := p.next(); err != nil {
			return nil, err
		}
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: "||", Left: left, Right: right}
	}
	return left, nil
}

func (p *Parser) parseUnary() (Expr, error) {
	if p.isSymbol("+") || p.isSymbol("-") {
		op := p.tok.Text
		if err := p.next(); err != nil {
			return nil, err
		}
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Unary{Op: op, Operand: operand}, nil
	}
	return p.parsePrimary()
}

func (p *Parser) parsePrimary() (Expr, error) {
	switch p.tok.Kind {
	case TokenInt:
		n, err := strconv.ParseInt(p.tok.Text, 10, 64)
		if err != nil {
			return nil, p.errorf("invalid integer %q", p.tok.Text)
		}
		if err := p.next(); err != nil {
			return nil, err
		}
		return &Literal{Value: IntValue(n)}, nil
	case TokenFloat:
		f, err := strconv.ParseFloat(p.tok.Text, 64)
		if err != nil {
			return nil, p.errorf("invalid float %q", p.tok.Text)
		}
		if err := p.next(); err != nil {
			return nil, err
		}
		return &Literal{Value: RealValue(f)}, nil
	case TokenString:
		value := TextValue(p.tok.Text)
		if err := p.next(); err != nil {
			return nil, err
		}
		return &Literal{Value: value}, nil
	case TokenKeyword:
		switch p.tok.Text {
		case "NULL":
			if err := p.next(); err != nil {
				return nil, err
			}
			return &Literal{Value: NullValue()}, nil
		case "TRUE":
			if err := p.next(); err != nil {
				return nil, err
			}
			return &Literal{Value: BoolValue(true)}, nil
		case "FALSE":
			if err := p.next(); err != nil {
				return nil, err
			}
			return &Literal{Value: BoolValue(false)}, nil
		}
	case TokenIdent:
		name := p.tok.Text
		if err := p.next(); err != nil {
			return nil, err
		}
		if p.isSymbol("(") {
			return p.parseFuncCall(name)
		}
		if p.isSymbol(".") {
			if err := p.next(); err != nil {
				return nil, err
			}
			col, err := p.parseName()
			if err != nil {
				return nil, err
			}
			return &ColumnRef{Table: name, Name: col}, nil
		}
		return &ColumnRef{Name: name}, nil
	case TokenSymbol:
		if p.isSymbol("(") {
			if err := p.next(); err != nil {
				return nil, err
			}
			expr, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expectSymbol(")"); err != nil {
				return nil, err
			}
			return expr, nil
		}
	}
	return nil, p.errorf("expected an expression, got %s", p.tok)
}

func (p *Parser) parseFuncCall(name string) (Expr, error) {
	if err := p.expectSymbol("("); err != nil {
		return nil, err
	}
	call := &FuncCall{Name: strings.ToUpper(name)}
	if p.isSymbol("*") {
		call.Star = true
		if err := p.next(); err != nil {
			return nil, err
		}
		if err := p.expectSymbol(")"); err != nil {
			return nil, err
		}
		return call, nil
	}
	if p.isSymbol(")") {
		if err := p.next(); err != nil {
			return nil, err
		}
		return call, nil
	}
	for {
		arg, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		call.Args = append(call.Args, arg)
		if p.isSymbol(",") {
			if err := p.next(); err != nil {
				return nil, err
			}
			continue
		}
		break
	}
	if err := p.expectSymbol(")"); err != nil {
		return nil, err
	}
	return call, nil
}
