package sql

import (
	"errors"
	"testing"
)

func TestTokenizeBasic(t *testing.T) {
	input := `SELECT id, "full name" FROM users WHERE score >= 1.5e2 AND name = 'O''Brien'; -- trailing`
	tokens, err := Tokenize(input)
	if err != nil {
		t.Fatal(err)
	}

	want := []struct {
		kind TokenKind
		text string
	}{
		{TokenKeyword, "SELECT"},
		{TokenIdent, "id"},
		{TokenSymbol, ","},
		{TokenIdent, "full name"},
		{TokenKeyword, "FROM"},
		{TokenIdent, "users"},
		{TokenKeyword, "WHERE"},
		{TokenIdent, "score"},
		{TokenSymbol, ">="},
		{TokenFloat, "1.5e2"},
		{TokenKeyword, "AND"},
		{TokenIdent, "name"},
		{TokenSymbol, "="},
		{TokenString, "O'Brien"},
		{TokenSymbol, ";"},
		{TokenEOF, ""},
	}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(tokens), len(want), tokens)
	}
	for i, w := range want {
		if tokens[i].Kind != w.kind || tokens[i].Text != w.text {
			t.Fatalf("token %d = %s %q, want %s %q", i, tokens[i].Kind, tokens[i].Text, w.kind, w.text)
		}
	}
}

func TestTokenizeCommentsAndNumbers(t *testing.T) {
	tokens, err := Tokenize("1 2.5 /* block */ 42 -- line\n 7")
	if err != nil {
		t.Fatal(err)
	}
	var kinds []TokenKind
	var texts []string
	for _, tok := range tokens {
		if tok.Kind == TokenEOF {
			break
		}
		kinds = append(kinds, tok.Kind)
		texts = append(texts, tok.Text)
	}
	wantKinds := []TokenKind{TokenInt, TokenFloat, TokenInt, TokenInt}
	wantTexts := []string{"1", "2.5", "42", "7"}
	if len(kinds) != len(wantKinds) {
		t.Fatalf("kinds = %v, want %v", kinds, wantKinds)
	}
	for i := range wantKinds {
		if kinds[i] != wantKinds[i] || texts[i] != wantTexts[i] {
			t.Fatalf("token %d = %v %q, want %v %q", i, kinds[i], texts[i], wantKinds[i], wantTexts[i])
		}
	}
}

func TestLexerErrors(t *testing.T) {
	if _, err := Tokenize("'unterminated"); err == nil {
		t.Fatal("expected unterminated string error")
	}
	var syntaxErr *SyntaxError
	if _, err := Tokenize("SELECT @"); !errors.As(err, &syntaxErr) {
		t.Fatalf("error = %v, want SyntaxError", err)
	}
}

func TestParseCreateTable(t *testing.T) {
	stmt, err := ParseOne(`CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY,
		name VARCHAR(100) NOT NULL UNIQUE,
		age INT DEFAULT 18,
		active BOOLEAN DEFAULT TRUE
	)`)
	if err != nil {
		t.Fatal(err)
	}
	create, ok := stmt.(*CreateTable)
	if !ok {
		t.Fatalf("statement = %T", stmt)
	}
	if create.Name != "users" || !create.IfNotExists {
		t.Fatalf("create = %+v", create)
	}
	if len(create.Columns) != 4 {
		t.Fatalf("columns = %d", len(create.Columns))
	}
	if !create.Columns[0].PrimaryKey || create.Columns[0].Type != IntegerType {
		t.Fatalf("id column = %+v", create.Columns[0])
	}
	if !create.Columns[1].NotNull || !create.Columns[1].Unique || create.Columns[1].Type != TextType {
		t.Fatalf("name column = %+v", create.Columns[1])
	}
	lit, ok := create.Columns[2].Default.(*Literal)
	if !ok || lit.Value.Int != 18 {
		t.Fatalf("age default = %#v", create.Columns[2].Default)
	}
}

func TestParseTablePrimaryKey(t *testing.T) {
	stmt, err := ParseOne(`CREATE TABLE t (id INTEGER, name TEXT, PRIMARY KEY (id))`)
	if err != nil {
		t.Fatal(err)
	}
	create := stmt.(*CreateTable)
	if !create.Columns[0].PrimaryKey {
		t.Fatal("table-level PRIMARY KEY was not applied")
	}

	if _, err := ParseOne(`CREATE TABLE t (a INT, b INT, PRIMARY KEY (a, b))`); err == nil {
		t.Fatal("composite primary key should be rejected")
	}
	if _, err := ParseOne(`CREATE TABLE t (id SERIAL)`); err == nil {
		t.Fatal("unknown type should be rejected")
	}
}

func TestParseInsert(t *testing.T) {
	stmt, err := ParseOne(`INSERT INTO users (id, name) VALUES (1, 'Ada'), (2, 'Grace')`)
	if err != nil {
		t.Fatal(err)
	}
	insert := stmt.(*Insert)
	if insert.Table != "users" || len(insert.Columns) != 2 || len(insert.Rows) != 2 {
		t.Fatalf("insert = %+v", insert)
	}
	if lit := insert.Rows[1][1].(*Literal); lit.Value.Str != "Grace" {
		t.Fatalf("second row = %+v", insert.Rows[1])
	}
}

func TestParseSelect(t *testing.T) {
	stmt, err := ParseOne(`SELECT DISTINCT name, age + 1 AS next_age, COUNT(*)
		FROM users
		WHERE age >= 18 AND (active = TRUE OR name LIKE 'A%')
		GROUP BY name, age
		HAVING COUNT(*) > 1
		ORDER BY next_age DESC, name
		LIMIT 10 OFFSET 5`)
	if err != nil {
		t.Fatal(err)
	}
	sel := stmt.(*Select)
	if !sel.Distinct || sel.Table != "users" || len(sel.Items) != 3 {
		t.Fatalf("select = %+v", sel)
	}
	if len(sel.GroupBy) != 2 || sel.Having == nil || len(sel.OrderBy) != 2 {
		t.Fatalf("clauses: group=%d having=%v order=%d", len(sel.GroupBy), sel.Having, len(sel.OrderBy))
	}
	if sel.Limit == nil || *sel.Limit != 10 || sel.Offset == nil || *sel.Offset != 5 {
		t.Fatalf("limit/offset = %v/%v", sel.Limit, sel.Offset)
	}
	if !sel.OrderBy[0].Desc || sel.OrderBy[1].Desc {
		t.Fatalf("order direction = %+v", sel.OrderBy)
	}
}

func TestParseUpdateDelete(t *testing.T) {
	stmt, err := ParseOne(`UPDATE users SET age = age + 1, name = 'x' WHERE id = 1`)
	if err != nil {
		t.Fatal(err)
	}
	upd := stmt.(*Update)
	if upd.Table != "users" || len(upd.Assignments) != 2 || upd.Where == nil {
		t.Fatalf("update = %+v", upd)
	}

	stmt, err = ParseOne(`DELETE FROM users WHERE age < 18`)
	if err != nil {
		t.Fatal(err)
	}
	del := stmt.(*Delete)
	if del.Table != "users" || del.Where == nil {
		t.Fatalf("delete = %+v", del)
	}
}

func TestParsePrecedence(t *testing.T) {
	stmt, err := ParseOne(`SELECT * FROM t WHERE a = 1 OR b = 2 AND NOT c = 3`)
	if err != nil {
		t.Fatal(err)
	}
	where := stmt.(*Select).Where
	or, ok := where.(*Binary)
	if !ok || or.Op != "OR" {
		t.Fatalf("root = %#v", where)
	}
	and, ok := or.Right.(*Binary)
	if !ok || and.Op != "AND" {
		t.Fatalf("right = %#v", or.Right)
	}
	not, ok := and.Right.(*Unary)
	if !ok || not.Op != "NOT" {
		t.Fatalf("and.right = %#v", and.Right)
	}

	stmt, err = ParseOne(`SELECT * FROM t WHERE x BETWEEN 1 AND 2 OR y NOT IN (3, 4)`)
	if err != nil {
		t.Fatal(err)
	}
	root := stmt.(*Select).Where.(*Binary)
	if root.Op != "OR" {
		t.Fatalf("root op = %s", root.Op)
	}
	if _, ok := root.Left.(*Between); !ok {
		t.Fatalf("left = %#v", root.Left)
	}
	in, ok := root.Right.(*In)
	if !ok || !in.Not {
		t.Fatalf("right = %#v", root.Right)
	}
}

func TestParseMultipleStatementsAndMisc(t *testing.T) {
	stmts, err := Parse(`BEGIN; INSERT INTO t VALUES (1); COMMIT;`)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 3 {
		t.Fatalf("statements = %d", len(stmts))
	}
	if _, ok := stmts[0].(*Begin); !ok {
		t.Fatalf("first = %T", stmts[0])
	}
	if _, ok := stmts[2].(*Commit); !ok {
		t.Fatalf("third = %T", stmts[2])
	}

	for _, input := range []string{"SHOW TABLES", "DESCRIBE users", "DESC users", "ROLLBACK"} {
		if _, err := ParseOne(input); err != nil {
			t.Fatalf("ParseOne(%q): %v", input, err)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := []string{
		"SELECT",
		"SELECT * FROM",
		"SELECT * FROM t WHERE",
		"INSERT INTO t VALUES",
		"CREATE TABLE t (id)",
		"CREATE TABLE t id INT)",
		"UPDATE t SET",
		"DELETE t",
	}
	for _, input := range cases {
		if _, err := ParseOne(input); err == nil {
			t.Errorf("ParseOne(%q) succeeded, want error", input)
		}
	}
}
