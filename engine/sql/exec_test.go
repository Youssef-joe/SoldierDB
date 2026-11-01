package sql

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Youssef-joe/YousDB/engine/db"
)

func openEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	e, err := OpenWithOptions(path, db.Options{NoSync: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return e, path
}

func mustExec(t *testing.T, e *Engine, query string) *Result {
	t.Helper()
	res, err := e.Exec(query)
	if err != nil {
		t.Fatalf("Exec(%q): %v", query, err)
	}
	return res
}

func checkRows(t *testing.T, res *Result, want [][]string) {
	t.Helper()
	if len(res.Rows) != len(want) {
		t.Fatalf("row count = %d, want %d\nrows: %s", len(res.Rows), len(want), dumpRows(res.Rows))
	}
	for i := range want {
		if len(res.Rows[i]) != len(want[i]) {
			t.Fatalf("row %d has %d columns, want %d", i, len(res.Rows[i]), len(want[i]))
		}
		for j := range want[i] {
			got := res.Rows[i][j].String()
			if got != want[i][j] {
				t.Fatalf("row %d column %d = %q, want %q\nrows: %s", i, j, got, want[i][j], dumpRows(res.Rows))
			}
		}
	}
}

func dumpRows(rows [][]Value) string {
	out := ""
	for _, row := range rows {
		out += "["
		for i, v := range row {
			if i > 0 {
				out += " "
			}
			out += v.String()
		}
		out += "]"
	}
	return out
}

func TestCreateInsertSelect(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		age INTEGER,
		active BOOLEAN DEFAULT TRUE
	)`)
	res := mustExec(t, e, `INSERT INTO users (id, name, age) VALUES (1, 'Ada', 36), (2, 'Grace', 45)`)
	if res.RowsAffected != 2 {
		t.Fatalf("RowsAffected = %d, want 2", res.RowsAffected)
	}

	res = mustExec(t, e, `SELECT name, age FROM users ORDER BY age`)
	if len(res.Columns) != 2 || res.Columns[0] != "name" || res.Columns[1] != "age" {
		t.Fatalf("columns = %v", res.Columns)
	}
	checkRows(t, res, [][]string{{"Ada", "36"}, {"Grace", "45"}})

	res = mustExec(t, e, `SELECT * FROM users WHERE active = TRUE ORDER BY id DESC`)
	if len(res.Columns) != 4 {
		t.Fatalf("SELECT * columns = %v", res.Columns)
	}
	checkRows(t, res, [][]string{
		{"2", "Grace", "45", "true"},
		{"1", "Ada", "36", "true"},
	})

	res = mustExec(t, e, `SHOW TABLES`)
	checkRows(t, res, [][]string{{"users"}})

	res = mustExec(t, e, `DESCRIBE users`)
	if len(res.Rows) != 4 {
		t.Fatalf("DESCRIBE rows = %d", len(res.Rows))
	}
	checkRows(t, res, [][]string{
		{"id", "INTEGER", "NO", "NULL", "YES"},
		{"name", "TEXT", "NO", "NULL", "NO"},
		{"age", "INTEGER", "YES", "NULL", "NO"},
		{"active", "BOOLEAN", "YES", "true", "NO"},
	})
}

func TestDefaultsAndHiddenRowID(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE notes (body TEXT NOT NULL, score INT DEFAULT 5)`)
	mustExec(t, e, `INSERT INTO notes (body) VALUES ('first'), ('second')`)

	res := mustExec(t, e, `SELECT * FROM notes ORDER BY rowid`)
	if len(res.Columns) != 2 || res.Columns[0] != "body" || res.Columns[1] != "score" {
		t.Fatalf("SELECT * columns = %v", res.Columns)
	}
	checkRows(t, res, [][]string{{"first", "5"}, {"second", "5"}})

	res = mustExec(t, e, `SELECT rowid, body FROM notes ORDER BY rowid DESC`)
	checkRows(t, res, [][]string{{"2", "second"}, {"1", "first"}})

	mustExec(t, e, `INSERT INTO notes (rowid, body) VALUES (10, 'explicit')`)
	res = mustExec(t, e, `SELECT rowid FROM notes ORDER BY rowid`)
	checkRows(t, res, [][]string{{"1"}, {"2"}, {"10"}})

	// Auto ids continue after the explicit one.
	mustExec(t, e, `INSERT INTO notes (body) VALUES ('next')`)
	res = mustExec(t, e, `SELECT rowid FROM notes ORDER BY rowid`)
	checkRows(t, res, [][]string{{"1"}, {"2"}, {"10"}, {"11"}})

	// Updating the hidden rowid moves the auto-increment high-water mark.
	mustExec(t, e, `UPDATE notes SET rowid = 20 WHERE body = 'explicit'`)
	mustExec(t, e, `INSERT INTO notes (body) VALUES ('after update')`)
	res = mustExec(t, e, `SELECT rowid FROM notes ORDER BY rowid`)
	checkRows(t, res, [][]string{{"1"}, {"2"}, {"11"}, {"20"}, {"21"}})
}

func TestTextPrimaryKeyOrdering(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE tags (k TEXT PRIMARY KEY, v INTEGER)`)
	mustExec(t, e, `INSERT INTO tags VALUES ('banana', 2), ('apple', 1), ('cherry', 3)`)

	res := mustExec(t, e, `SELECT k, v FROM tags ORDER BY k`)
	checkRows(t, res, [][]string{{"apple", "1"}, {"banana", "2"}, {"cherry", "3"}})

	res = mustExec(t, e, `SELECT v FROM tags WHERE k = 'banana'`)
	checkRows(t, res, [][]string{{"2"}})
}

func TestConstraints(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE u (
		id INTEGER PRIMARY KEY,
		email TEXT UNIQUE,
		name TEXT NOT NULL
	)`)
	mustExec(t, e, `INSERT INTO u VALUES (1, 'a@example.com', 'Ada')`)

	cases := []struct {
		name  string
		query string
	}{
		{"duplicate primary key", `INSERT INTO u VALUES (1, 'b@example.com', 'Bob')`},
		{"duplicate unique", `INSERT INTO u VALUES (2, 'a@example.com', 'Bob')`},
		{"null not null", `INSERT INTO u VALUES (2, 'b@example.com', NULL)`},
		{"bad integer", `INSERT INTO u VALUES ('nope', 'c@example.com', 'Cid')`},
		{"wrong value count", `INSERT INTO u VALUES (3, 'c@example.com')`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := e.Exec(tc.query); err == nil {
				t.Fatalf("Exec(%q) succeeded, want error", tc.query)
			}
		})
	}

	// Multiple NULLs are allowed in a UNIQUE column.
	mustExec(t, e, `INSERT INTO u VALUES (2, NULL, 'Bob'), (3, NULL, 'Cid')`)
	res := mustExec(t, e, `SELECT COUNT(*) FROM u`)
	checkRows(t, res, [][]string{{"3"}})
}

func TestExpressionsAndOperators(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	res := mustExec(t, e, `SELECT 1 + 2, 7 / 2, 7.0 / 2, 'a' || 'b', 2 * 3 - 1`)
	checkRows(t, res, [][]string{{"3", "3", "3.5", "ab", "5"}})

	res = mustExec(t, e, `SELECT 3 BETWEEN 1 AND 5, 9 BETWEEN 1 AND 5, 2 IN (1, 2, 3), 9 NOT IN (1, 2)`)
	checkRows(t, res, [][]string{{"true", "false", "true", "true"}})

	res = mustExec(t, e, `SELECT 'hello' LIKE 'h%', 'hello' LIKE 'h_llo', 'hello' NOT LIKE '%z%'`)
	checkRows(t, res, [][]string{{"true", "true", "true"}})

	res = mustExec(t, e, `SELECT NULL IS NULL, 1 IS NOT NULL, NULL = NULL, NULL AND FALSE, NULL OR TRUE`)
	checkRows(t, res, [][]string{{"true", "true", "NULL", "false", "true"}})

	res = mustExec(t, e, `SELECT UPPER('ada'), LOWER('ADA'), LENGTH('héllo'), ABS(-4), ROUND(3.14159, 2)`)
	checkRows(t, res, [][]string{{"ADA", "ada", "5", "4", "3.14"}})

	res = mustExec(t, e, `SELECT COALESCE(NULL, NULL, 'x'), IFNULL(NULL, 7), NULLIF(1, 1), NULLIF(1, 2)`)
	checkRows(t, res, [][]string{{"x", "7", "NULL", "1"}})
}

func TestAggregatesAndGrouping(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE sales (region TEXT, amount INTEGER)`)
	mustExec(t, e, `INSERT INTO sales VALUES
		('north', 10), ('north', 20), ('south', 5), ('south', 15), ('east', 30)`)

	res := mustExec(t, e, `SELECT COUNT(*), SUM(amount), AVG(amount), MIN(amount), MAX(amount) FROM sales`)
	checkRows(t, res, [][]string{{"5", "80", "16", "5", "30"}})

	res = mustExec(t, e, `SELECT region, COUNT(*), SUM(amount)
		FROM sales GROUP BY region ORDER BY region`)
	checkRows(t, res, [][]string{
		{"east", "1", "30"},
		{"north", "2", "30"},
		{"south", "2", "20"},
	})

	res = mustExec(t, e, `SELECT region, SUM(amount) AS total
		FROM sales GROUP BY region HAVING SUM(amount) > 20 ORDER BY total DESC, region`)
	checkRows(t, res, [][]string{{"east", "30"}, {"north", "30"}})

	// Empty input with aggregates yields one row.
	res = mustExec(t, e, `SELECT COUNT(*), SUM(amount) FROM sales WHERE amount > 1000`)
	checkRows(t, res, [][]string{{"0", "NULL"}})

	// HAVING without GROUP BY filters the single aggregate group.
	res = mustExec(t, e, `SELECT COUNT(*) FROM sales WHERE amount > 1000 HAVING COUNT(*) = 0`)
	checkRows(t, res, [][]string{{"0"}})

	// DISTINCT combines with ORDER BY and LIMIT/OFFSET.
	res = mustExec(t, e, `SELECT DISTINCT amount FROM sales ORDER BY amount DESC LIMIT 2 OFFSET 1`)
	checkRows(t, res, [][]string{{"20"}, {"15"}})
}

func TestOrderingByAliasAndOrdinal(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE t (name TEXT, age INTEGER)`)
	mustExec(t, e, `INSERT INTO t VALUES ('Ada', 36), ('Grace', 45), ('Linus', 28)`)

	res := mustExec(t, e, `SELECT name AS n, age FROM t ORDER BY n DESC`)
	checkRows(t, res, [][]string{{"Linus", "28"}, {"Grace", "45"}, {"Ada", "36"}})

	res = mustExec(t, e, `SELECT name, age FROM t ORDER BY 2`)
	checkRows(t, res, [][]string{{"Linus", "28"}, {"Ada", "36"}, {"Grace", "45"}})

	res = mustExec(t, e, `SELECT name FROM t WHERE age > 30 ORDER BY age DESC`)
	checkRows(t, res, [][]string{{"Grace"}, {"Ada"}})
}

func TestUpdateAndDelete(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER)`)
	mustExec(t, e, `INSERT INTO users VALUES (1, 'Ada', 36), (2, 'Grace', 45), (3, 'Linus', 28)`)

	res := mustExec(t, e, `UPDATE users SET age = age + 1 WHERE name = 'Ada'`)
	if res.RowsAffected != 1 {
		t.Fatalf("RowsAffected = %d, want 1", res.RowsAffected)
	}
	res = mustExec(t, e, `SELECT age FROM users WHERE id = 1`)
	checkRows(t, res, [][]string{{"37"}})

	res = mustExec(t, e, `UPDATE users SET id = 10, name = UPPER(name) WHERE id = 2`)
	if res.RowsAffected != 1 {
		t.Fatalf("RowsAffected = %d, want 1", res.RowsAffected)
	}
	res = mustExec(t, e, `SELECT id, name FROM users WHERE name = 'GRACE'`)
	checkRows(t, res, [][]string{{"10", "GRACE"}})

	res = mustExec(t, e, `DELETE FROM users WHERE age > 40`)
	if res.RowsAffected != 1 {
		t.Fatalf("RowsAffected = %d, want 1", res.RowsAffected)
	}
	res = mustExec(t, e, `SELECT COUNT(*) FROM users`)
	checkRows(t, res, [][]string{{"2"}})

	res = mustExec(t, e, `DELETE FROM users`)
	if res.RowsAffected != 2 {
		t.Fatalf("RowsAffected = %d, want 2", res.RowsAffected)
	}
	res = mustExec(t, e, `SELECT COUNT(*) FROM users`)
	checkRows(t, res, [][]string{{"0"}})
}

func TestUpdateConstraints(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE u (id INTEGER PRIMARY KEY, email TEXT UNIQUE, name TEXT NOT NULL)`)
	mustExec(t, e, `INSERT INTO u VALUES (1, 'a@x', 'Ada'), (2, 'b@x', 'Bob')`)

	if _, err := e.Exec(`UPDATE u SET email = 'b@x' WHERE id = 1`); !errors.Is(err, ErrConstraint) {
		t.Fatalf("duplicate unique update error = %v", err)
	}
	if _, err := e.Exec(`UPDATE u SET name = NULL WHERE id = 1`); !errors.Is(err, ErrConstraint) {
		t.Fatalf("not-null update error = %v", err)
	}
	if _, err := e.Exec(`UPDATE u SET id = 2 WHERE id = 1`); !errors.Is(err, ErrConstraint) {
		t.Fatalf("duplicate primary key update error = %v", err)
	}
	res := mustExec(t, e, `SELECT id, email, name FROM u ORDER BY id`)
	checkRows(t, res, [][]string{{"1", "a@x", "Ada"}, {"2", "b@x", "Bob"}})
}

func TestTransactions(t *testing.T) {
	e, path := openEngine(t)
	mustExec(t, e, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)
	mustExec(t, e, `INSERT INTO t VALUES (1, 'one')`)

	mustExec(t, e, `BEGIN`)
	if !e.InTransaction() {
		t.Fatal("InTransaction = false after BEGIN")
	}
	mustExec(t, e, `INSERT INTO t VALUES (2, 'two')`)
	res := mustExec(t, e, `SELECT COUNT(*) FROM t`)
	checkRows(t, res, [][]string{{"2"}})
	mustExec(t, e, `ROLLBACK`)
	if e.InTransaction() {
		t.Fatal("InTransaction = true after ROLLBACK")
	}
	res = mustExec(t, e, `SELECT COUNT(*) FROM t`)
	checkRows(t, res, [][]string{{"1"}})

	mustExec(t, e, `BEGIN`)
	mustExec(t, e, `INSERT INTO t VALUES (3, 'three')`)
	mustExec(t, e, `COMMIT`)
	res = mustExec(t, e, `SELECT id FROM t ORDER BY id`)
	checkRows(t, res, [][]string{{"1"}, {"3"}})

	if _, err := e.Exec(`COMMIT`); !errors.Is(err, ErrNoTransaction) {
		t.Fatalf("COMMIT without BEGIN = %v", err)
	}
	if _, err := e.Exec(`ROLLBACK`); !errors.Is(err, ErrNoTransaction) {
		t.Fatalf("ROLLBACK without BEGIN = %v", err)
	}
	mustExec(t, e, `BEGIN`)
	if _, err := e.Exec(`BEGIN`); !errors.Is(err, ErrTxActive) {
		t.Fatalf("nested BEGIN = %v", err)
	}
	mustExec(t, e, `ROLLBACK`)

	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	e2, err := OpenWithOptions(path, db.Options{NoSync: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e2.Close()
	res = mustExec(t, e2, `SELECT id FROM t ORDER BY id`)
	checkRows(t, res, [][]string{{"1"}, {"3"}})
}

func TestPersistenceAndRowIDContinuity(t *testing.T) {
	e, path := openEngine(t)
	mustExec(t, e, `CREATE TABLE t (v TEXT)`)
	mustExec(t, e, `INSERT INTO t VALUES ('a'), ('b')`)
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	e2, err := OpenWithOptions(path, db.Options{NoSync: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e2.Close()

	res := mustExec(t, e2, `SELECT rowid, v FROM t ORDER BY rowid`)
	checkRows(t, res, [][]string{{"1", "a"}, {"2", "b"}})

	mustExec(t, e2, `INSERT INTO t VALUES ('c')`)
	res = mustExec(t, e2, `SELECT rowid FROM t ORDER BY rowid`)
	checkRows(t, res, [][]string{{"1"}, {"2"}, {"3"}})

	res = mustExec(t, e2, `SHOW TABLES`)
	checkRows(t, res, [][]string{{"t"}})
}

func TestCaseInsensitiveIdentifiers(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE Users (ID INTEGER PRIMARY KEY, Name TEXT)`)
	mustExec(t, e, `INSERT INTO users (id, name) VALUES (1, 'Ada')`)

	res := mustExec(t, e, `SELECT NAME FROM USERS WHERE Id = 1`)
	checkRows(t, res, [][]string{{"Ada"}})

	res = mustExec(t, e, `SELECT users.name FROM users`)
	checkRows(t, res, [][]string{{"Ada"}})
}

func TestDropTable(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE a (id INTEGER PRIMARY KEY)`)
	mustExec(t, e, `CREATE TABLE b (id INTEGER PRIMARY KEY)`)
	mustExec(t, e, `INSERT INTO a VALUES (1)`)

	mustExec(t, e, `DROP TABLE a`)
	res := mustExec(t, e, `SHOW TABLES`)
	checkRows(t, res, [][]string{{"b"}})

	mustExec(t, e, `DROP TABLE IF EXISTS a`)
	if _, err := e.Exec(`DROP TABLE a`); !errors.Is(err, ErrTableNotFound) {
		t.Fatalf("DROP missing = %v", err)
	}

	mustExec(t, e, `CREATE TABLE a (id INTEGER PRIMARY KEY)`)
	res = mustExec(t, e, `SELECT COUNT(*) FROM a`)
	checkRows(t, res, [][]string{{"0"}})
}

func TestCreateTableOptions(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE t (id INTEGER PRIMARY KEY)`)
	mustExec(t, e, `CREATE TABLE IF NOT EXISTS t (id INTEGER PRIMARY KEY)`)
	if _, err := e.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY)`); !errors.Is(err, ErrTableExists) {
		t.Fatalf("duplicate CREATE = %v", err)
	}

	// Table-level primary key syntax.
	mustExec(t, e, `CREATE TABLE u (id INTEGER, name TEXT, PRIMARY KEY (id))`)
	res := mustExec(t, e, `DESCRIBE u`)
	checkRows(t, res, [][]string{
		{"id", "INTEGER", "NO", "NULL", "YES"},
		{"name", "TEXT", "YES", "NULL", "NO"},
	})
}

func TestMultiStatementExec(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	res := mustExec(t, e, `
		CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT);
		INSERT INTO t VALUES (1, 'a'), (2, 'b');
		SELECT v FROM t ORDER BY id;
	`)
	checkRows(t, res, [][]string{{"a"}, {"b"}})
}

func TestQueryErrors(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`)

	cases := []string{
		`SELECT * FROM missing`,
		`SELECT nope FROM t`,
		`INSERT INTO missing VALUES (1)`,
		`UPDATE t SET nope = 1`,
		`UPDATE t SET v = nope`,
		`DELETE FROM missing`,
		`DELETE FROM t WHERE nope = 1`,
		`SELECT 1 / 0`,
		`SELECT * FROM t ORDER BY 5`,
		`SELECT SUM(v) FROM t`,
	}
	for _, query := range cases {
		if _, err := e.Exec(query); err == nil {
			t.Errorf("Exec(%q) succeeded, want error", query)
		}
	}

	// Parse errors are reported before execution.
	if _, err := e.Exec(`SELEC * FROM t`); err == nil {
		t.Fatal("invalid syntax succeeded")
	}
}

func TestExplicitTransactionOnError(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE t (id INTEGER PRIMARY KEY)`)
	mustExec(t, e, `BEGIN`)
	mustExec(t, e, `INSERT INTO t VALUES (1)`)
	if _, err := e.Exec(`INSERT INTO t VALUES (1)`); !errors.Is(err, ErrConstraint) {
		t.Fatalf("duplicate insert = %v", err)
	}
	// The first insert is still part of the open transaction.
	res := mustExec(t, e, `SELECT COUNT(*) FROM t`)
	checkRows(t, res, [][]string{{"1"}})
	mustExec(t, e, `ROLLBACK`)
	res = mustExec(t, e, `SELECT COUNT(*) FROM t`)
	checkRows(t, res, [][]string{{"0"}})
}

func TestAutoRowIDAfterFailedInsertInTransaction(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE t (v TEXT NOT NULL)`)
	mustExec(t, e, `BEGIN`)

	// The second row fails, but the first row stays in the transaction. The
	// auto-increment counter must advance so the next insert does not collide.
	if _, err := e.Exec(`INSERT INTO t (v) VALUES ('a'), (NULL)`); err == nil {
		t.Fatal("multi-row insert with NULL succeeded")
	}
	mustExec(t, e, `INSERT INTO t (v) VALUES ('b')`)

	res := mustExec(t, e, `SELECT rowid, v FROM t ORDER BY rowid`)
	checkRows(t, res, [][]string{{"1", "a"}, {"2", "b"}})
	mustExec(t, e, `ROLLBACK`)
	res = mustExec(t, e, `SELECT COUNT(*) FROM t`)
	checkRows(t, res, [][]string{{"0"}})
}

func TestVectorsAndSimilarityFunctions(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, `CREATE TABLE docs (id INTEGER PRIMARY KEY, embedding VECTOR(3) NOT NULL)`)
	mustExec(t, e, `INSERT INTO docs VALUES
		(1, VECTOR(1, 0, 0)),
		(2, VECTOR(0.9, 0.1, 0)),
		(3, VECTOR(0, 1, 0))`)

	res := mustExec(t, e, `SELECT id, cosine_distance(embedding, VECTOR(1, 0, 0)) AS distance
		FROM docs ORDER BY distance LIMIT 2`)
	checkRows(t, res, [][]string{{"1", "0"}, {"2", "0.006116265828075562"}})

	res = mustExec(t, e, `SELECT euclidean_distance(VECTOR(1, 2), VECTOR(4, 6)),
		inner_product(VECTOR(1, 2), VECTOR(3, 4))`)
	checkRows(t, res, [][]string{{"5", "11"}})

	res = mustExec(t, e, `DESCRIBE docs`)
	checkRows(t, res, [][]string{{"id", "INTEGER", "NO", "NULL", "YES"}, {"embedding", "VECTOR(3)", "NO", "NULL", "NO"}})

	if _, err := e.Exec(`INSERT INTO docs VALUES (4, VECTOR(1, 2))`); err == nil {
		t.Fatal("wrong-dimension vector succeeded")
	}
}
