//! Integration tests for the Rust client. They run against the real C ABI
//! shared library, so `make shared` must have been run first.

use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

use yousdb::{Database, Value};

static COUNTER: AtomicU64 = AtomicU64::new(0);

/// A uniquely named database file removed when the test ends.
struct TempDb {
    path: PathBuf,
}

impl TempDb {
    fn new(label: &str) -> Self {
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("system clock before the Unix epoch")
            .as_nanos();
        let counter = COUNTER.fetch_add(1, Ordering::Relaxed);
        let path = std::env::temp_dir().join(format!(
            "yousdb-rust-{}-{label}-{nanos}-{counter}.db",
            std::process::id()
        ));
        let _ = std::fs::remove_file(&path);
        TempDb { path }
    }

    fn path(&self) -> &Path {
        &self.path
    }
}

impl Drop for TempDb {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.path);
    }
}

fn text(value: &str) -> Value {
    Value::Text(value.to_owned())
}

#[test]
fn ddl_dml_and_typed_values() {
    let tmp = TempDb::new("types");
    let db = Database::open(tmp.path()).expect("open database");

    let result = db
        .exec(
            "CREATE TABLE t (
                id INTEGER PRIMARY KEY,
                name TEXT NOT NULL,
                score REAL,
                active BOOLEAN DEFAULT TRUE
            )",
        )
        .expect("create table");
    assert_eq!(result.message(), Some("table t created"));

    let result = db
        .exec("INSERT INTO t (id, name, score) VALUES (1, 'Ada', 9.5), (2, 'Grace', 8.25)")
        .expect("insert");
    assert_eq!(result.rows_affected(), 2);
    assert!(result.message().unwrap().contains("2 row"));

    let result = db
        .exec("SELECT id, name, score, active FROM t ORDER BY id")
        .expect("select");
    assert_eq!(result.columns(), ["id", "name", "score", "active"]);
    assert_eq!(result.len(), 2);

    assert_eq!(result.get(0, 0), Some(&Value::Integer(1)));
    assert_eq!(result.get(1, 0), Some(&Value::Integer(2)));
    assert_eq!(result.value(0, "name"), Some(&text("Ada")));
    assert_eq!(result.value(1, "NAME"), Some(&text("Grace")));
    assert_eq!(result.value(0, "score"), Some(&Value::Real(9.5)));
    assert_eq!(result.value(1, "score"), Some(&Value::Real(8.25)));
    assert_eq!(result.value(0, "active"), Some(&Value::Boolean(true)));
    assert_eq!(result.column_index("SCORE"), Some(2));
    assert_eq!(result.column_index("missing"), None);
}

#[test]
fn values_and_display() {
    let tmp = TempDb::new("values");
    let db = Database::open(tmp.path()).expect("open database");

    let result = db
        .exec("SELECT NULL, 7, 3.5, 'x', TRUE, 1 + 2, 'a' || 'b'")
        .expect("select constants");
    assert_eq!(
        result.rows()[0],
        vec![
            Value::Null,
            Value::Integer(7),
            Value::Real(3.5),
            text("x"),
            Value::Boolean(true),
            Value::Integer(3),
            text("ab"),
        ]
    );

    assert_eq!(result.get(0, 0).unwrap().to_string(), "NULL");
    assert_eq!(result.get(0, 1).unwrap().type_name(), "INTEGER");
    assert_eq!(result.get(0, 2).unwrap().type_name(), "REAL");
    assert_eq!(result.get(0, 3).unwrap().type_name(), "TEXT");
    assert_eq!(result.get(0, 4).unwrap().type_name(), "BOOLEAN");
    assert!(result.get(0, 0).unwrap().is_null());
    assert_eq!(result.get(0, 1).unwrap().as_i64(), Some(7));
    assert_eq!(result.get(0, 2).unwrap().as_f64(), Some(3.5));
    assert_eq!(result.get(0, 3).unwrap().as_str(), Some("x"));
    assert_eq!(result.get(0, 4).unwrap().as_bool(), Some(true));
}

#[test]
fn constraints_report_errors() {
    let tmp = TempDb::new("errors");
    let db = Database::open(tmp.path()).expect("open database");
    db.exec("CREATE TABLE u (id INTEGER PRIMARY KEY, email TEXT UNIQUE, name TEXT NOT NULL)")
        .expect("create table");
    db.exec("INSERT INTO u VALUES (1, 'a@example.com', 'Ada')")
        .expect("insert");

    let cases = [
        (
            "INSERT INTO u VALUES (1, 'b@example.com', 'Bob')",
            "duplicate primary key",
        ),
        (
            "INSERT INTO u VALUES (2, 'a@example.com', 'Bob')",
            "UNIQUE constraint",
        ),
        ("INSERT INTO u VALUES (2, 'b@example.com', NULL)", "cannot be NULL"),
        ("SELECT nope FROM u", "column not found"),
        ("SELECT * FROM missing", "table not found"),
        ("SELECT 1 / 0", "division by zero"),
        ("SELEC * FROM u", "syntax error"),
    ];

    for (sql, expected) in cases {
        let err = db.exec(sql).expect_err(sql);
        assert!(
            err.message().contains(expected),
            "error for {sql:?} = {err:?}, want it to contain {expected:?}"
        );
    }

    // A failed statement does not abort the handle.
    let result = db.exec("SELECT COUNT(*) FROM u").expect("count");
    assert_eq!(result.get(0, 0), Some(&Value::Integer(1)));
}

#[test]
fn update_delete_and_rows_affected() {
    let tmp = TempDb::new("dml");
    let db = Database::open(tmp.path()).expect("open database");
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER)")
        .expect("create");
    db.exec("INSERT INTO users VALUES (1, 'Ada', 36), (2, 'Grace', 45), (3, 'Linus', 28)")
        .expect("insert");

    let result = db
        .exec("UPDATE users SET age = age + 1 WHERE name = 'Ada'")
        .expect("update");
    assert_eq!(result.rows_affected(), 1);

    let result = db.exec("SELECT age FROM users WHERE id = 1").expect("select");
    assert_eq!(result.get(0, 0), Some(&Value::Integer(37)));

    let result = db.exec("DELETE FROM users WHERE age > 40").expect("delete");
    assert_eq!(result.rows_affected(), 1);

    let result = db.exec("SELECT id FROM users ORDER BY id").expect("select");
    assert_eq!(result.rows(), &[vec![Value::Integer(1)], vec![Value::Integer(3)]]);
}

#[test]
fn aggregates_and_grouping() {
    let tmp = TempDb::new("aggregates");
    let db = Database::open(tmp.path()).expect("open database");
    db.exec("CREATE TABLE sales (region TEXT, amount INTEGER)")
        .expect("create");
    db.exec(
        "INSERT INTO sales VALUES
            ('north', 10), ('north', 20), ('south', 5), ('south', 15), ('east', 30)",
    )
    .expect("insert");

    let result = db
        .exec("SELECT COUNT(*), SUM(amount), AVG(amount), MIN(amount), MAX(amount) FROM sales")
        .expect("aggregate");
    assert_eq!(
        result.rows()[0],
        vec![
            Value::Integer(5),
            Value::Integer(80),
            Value::Real(16.0),
            Value::Integer(5),
            Value::Integer(30),
        ]
    );

    let result = db
        .exec("SELECT region, COUNT(*), SUM(amount) FROM sales GROUP BY region ORDER BY region")
        .expect("group");
    assert_eq!(
        result.rows(),
        &[
            vec![text("east"), Value::Integer(1), Value::Integer(30)],
            vec![text("north"), Value::Integer(2), Value::Integer(30)],
            vec![text("south"), Value::Integer(2), Value::Integer(20)],
        ]
    );
}

#[test]
fn transactions_commit_and_rollback() {
    let tmp = TempDb::new("tx");
    let db = Database::open(tmp.path()).expect("open database");
    db.exec("CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)")
        .expect("create");
    db.exec("INSERT INTO t VALUES (1, 'one')").expect("insert");

    db.exec("BEGIN").expect("begin");
    db.exec("INSERT INTO t VALUES (2, 'two')").expect("insert");
    let result = db.exec("SELECT COUNT(*) FROM t").expect("count");
    assert_eq!(result.get(0, 0), Some(&Value::Integer(2)));
    db.exec("ROLLBACK").expect("rollback");
    let result = db.exec("SELECT COUNT(*) FROM t").expect("count");
    assert_eq!(result.get(0, 0), Some(&Value::Integer(1)));

    db.exec("BEGIN").expect("begin");
    db.exec("INSERT INTO t VALUES (3, 'three')").expect("insert");
    db.exec("COMMIT").expect("commit");
    let result = db.exec("SELECT id FROM t ORDER BY id").expect("select");
    assert_eq!(result.rows(), &[vec![Value::Integer(1)], vec![Value::Integer(3)]]);

    let err = db.exec("COMMIT").expect_err("commit without begin");
    assert!(err.message().contains("no active transaction"));
}

#[test]
fn vectors_can_be_read_and_ranked() {
    let tmp = TempDb::new("vectors");
    let db = Database::open(tmp.path()).expect("open database");
    db.exec("CREATE TABLE docs (id INTEGER PRIMARY KEY, embedding VECTOR(3) NOT NULL)")
        .expect("create");
    db.exec("INSERT INTO docs VALUES (1, VECTOR(1, 0, 0)), (2, VECTOR(0, 1, 0))")
        .expect("insert");

    let result = db
        .exec("SELECT id, embedding FROM docs ORDER BY cosine_distance(embedding, VECTOR(1, 0, 0)) LIMIT 1")
        .expect("search");
    assert_eq!(result.get(0, 0), Some(&Value::Integer(1)));
    assert_eq!(result.get(0, 1), Some(&Value::Vector(vec![1.0, 0.0, 0.0])));
    assert_eq!(result.get(0, 1).unwrap().as_vector(), Some(&[1.0, 0.0, 0.0][..]));
}

#[test]
fn multi_statement_exec_returns_last_result() {
    let tmp = TempDb::new("multi");
    let db = Database::open(tmp.path()).expect("open database");
    let result = db
        .exec(
            "CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT);
             INSERT INTO t VALUES (1, 'a'), (2, 'b');
             SELECT v FROM t ORDER BY id;",
        )
        .expect("multi statement");
    assert_eq!(result.columns(), ["v"]);
    assert_eq!(result.rows(), &[vec![text("a")], vec![text("b")]]);
}

#[test]
fn hidden_rowid_and_reopen_persistence() {
    let tmp = TempDb::new("reopen");
    {
        let db = Database::open(tmp.path()).expect("open database");
        db.exec("CREATE TABLE notes (body TEXT NOT NULL)").expect("create");
        db.exec("INSERT INTO notes (body) VALUES ('first'), ('second')")
            .expect("insert");

        let result = db
            .exec("SELECT rowid, body FROM notes ORDER BY rowid")
            .expect("select");
        assert_eq!(
            result.rows(),
            &[
                vec![Value::Integer(1), text("first")],
                vec![Value::Integer(2), text("second")],
            ]
        );
    }

    let db = Database::open(tmp.path()).expect("reopen");
    let result = db
        .exec("SELECT rowid, body FROM notes ORDER BY rowid")
        .expect("select after reopen");
    assert_eq!(result.len(), 2);
    assert_eq!(result.value(1, "body"), Some(&text("second")));

    db.exec("INSERT INTO notes (body) VALUES ('third')")
        .expect("insert after reopen");
    let result = db
        .exec("SELECT rowid FROM notes ORDER BY rowid")
        .expect("select rowids");
    assert_eq!(
        result.rows(),
        &[
            vec![Value::Integer(1)],
            vec![Value::Integer(2)],
            vec![Value::Integer(3)],
        ]
    );
}

#[test]
fn describe_and_show_tables() {
    let tmp = TempDb::new("meta");
    let db = Database::open(tmp.path()).expect("open database");
    db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)")
        .expect("create");

    let result = db.exec("SHOW TABLES").expect("show tables");
    assert_eq!(result.columns(), ["table"]);
    assert_eq!(result.rows(), &[vec![text("users")]]);

    let result = db.exec("DESCRIBE users").expect("describe");
    assert_eq!(
        result.columns(),
        ["column", "type", "nullable", "default", "primary_key"]
    );
    assert_eq!(result.len(), 2);
    assert_eq!(result.value(0, "column"), Some(&text("id")));
    assert_eq!(result.value(0, "primary_key"), Some(&text("YES")));
    assert_eq!(result.value(1, "nullable"), Some(&text("NO")));
}
