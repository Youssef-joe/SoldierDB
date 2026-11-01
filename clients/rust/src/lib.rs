//! Safe Rust bindings for the YousDB SQL engine.
//!
//! The crate links against the `yousdb` C ABI shared library. Build it first
//! from the repository root with `make shared`.
//!
//! ```
//! use yousdb::Database;
//!
//! # fn main() -> yousdb::Result<()> {
//! # let path = std::env::temp_dir().join("yousdb-doc.db");
//! # let _ = std::fs::remove_file(&path);
//! let db = Database::open(path)?;
//! db.exec("CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)")?;
//! db.exec("INSERT INTO users VALUES (1, 'Ada')")?;
//!
//! let result = db.exec("SELECT id, name FROM users ORDER BY id")?;
//! assert_eq!(result.columns(), ["id", "name"]);
//! assert_eq!(result.rows().len(), 1);
//! # Ok(())
//! # }
//! ```
//!
//! [`Database`] owns a native handle and closes it on drop. Every call on a
//! handle is serialized inside the library, so a `Database` may be moved to
//! another thread, but not shared without external synchronization.

use std::ffi::{CStr, CString};
use std::fmt;
use std::os::raw::{c_char, c_double, c_int};
use std::path::Path;
use std::ptr;

mod ffi {
    use super::{c_char, c_double, c_int};

    extern "C" {
        pub fn yousdb_open(path: *mut c_char, err_out: *mut *mut c_char) -> u64;
        pub fn yousdb_close(handle: u64);

        pub fn yousdb_exec(
            handle: u64,
            query: *mut c_char,
            out: *mut u64,
            err_out: *mut *mut c_char,
        ) -> c_int;

        pub fn yousdb_result_free(handle: u64);
        pub fn yousdb_result_column_count(handle: u64) -> c_int;
        pub fn yousdb_result_column_name(handle: u64, index: c_int) -> *mut c_char;
        pub fn yousdb_result_row_count(handle: u64) -> c_int;
        pub fn yousdb_result_rows_affected(handle: u64) -> c_int;
        pub fn yousdb_result_message(handle: u64) -> *mut c_char;

        pub fn yousdb_value_type(handle: u64, row: c_int, col: c_int) -> c_int;
        pub fn yousdb_value_int(handle: u64, row: c_int, col: c_int) -> i64;
        pub fn yousdb_value_real(handle: u64, row: c_int, col: c_int) -> c_double;
        pub fn yousdb_value_bool(handle: u64, row: c_int, col: c_int) -> c_int;
        pub fn yousdb_value_text(handle: u64, row: c_int, col: c_int) -> *mut c_char;
        pub fn yousdb_value_vector_dimension(handle: u64, row: c_int, col: c_int) -> c_int;
        pub fn yousdb_value_vector_element(
            handle: u64,
            row: c_int,
            col: c_int,
            index: c_int,
        ) -> f32;

        pub fn yousdb_free_string(s: *mut c_char);
    }
}

/// An error returned by the database engine.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Error {
    message: String,
}

impl Error {
    fn new(message: impl Into<String>) -> Self {
        Error {
            message: message.into(),
        }
    }

    /// The engine's error message.
    pub fn message(&self) -> &str {
        &self.message
    }
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.message)
    }
}

impl std::error::Error for Error {}

/// Convenient result alias for this crate.
pub type Result<T> = std::result::Result<T, Error>;

/// A SQL value returned by a query.
#[derive(Debug, Clone, PartialEq)]
pub enum Value {
    Null,
    Integer(i64),
    Real(f64),
    Text(String),
    Boolean(bool),
    Vector(Vec<f32>),
}

impl Value {
    /// True for SQL `NULL`.
    pub fn is_null(&self) -> bool {
        matches!(self, Value::Null)
    }

    /// The value as an integer, if it is one.
    pub fn as_i64(&self) -> Option<i64> {
        match self {
            Value::Integer(v) => Some(*v),
            _ => None,
        }
    }

    /// The value as a float, if it is numeric.
    pub fn as_f64(&self) -> Option<f64> {
        match self {
            Value::Integer(v) => Some(*v as f64),
            Value::Real(v) => Some(*v),
            _ => None,
        }
    }

    /// The value as text, if it is text.
    pub fn as_str(&self) -> Option<&str> {
        match self {
            Value::Text(v) => Some(v),
            _ => None,
        }
    }

    /// The value as a boolean, if it is one.
    pub fn as_bool(&self) -> Option<bool> {
        match self {
            Value::Boolean(v) => Some(*v),
            _ => None,
        }
    }

    /// The value as a vector, if it is one.
    pub fn as_vector(&self) -> Option<&[f32]> {
        match self {
            Value::Vector(v) => Some(v),
            _ => None,
        }
    }

    /// The name of the value's SQL type.
    pub fn type_name(&self) -> &'static str {
        match self {
            Value::Null => "NULL",
            Value::Integer(_) => "INTEGER",
            Value::Real(_) => "REAL",
            Value::Text(_) => "TEXT",
            Value::Boolean(_) => "BOOLEAN",
            Value::Vector(_) => "VECTOR",
        }
    }
}

impl fmt::Display for Value {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Value::Null => f.write_str("NULL"),
            Value::Integer(v) => write!(f, "{v}"),
            Value::Real(v) => write!(f, "{v}"),
            Value::Text(v) => f.write_str(v),
            Value::Boolean(true) => f.write_str("true"),
            Value::Boolean(false) => f.write_str("false"),
            Value::Vector(v) => {
                f.write_str("[")?;
                for (i, component) in v.iter().enumerate() {
                    if i > 0 {
                        f.write_str(", ")?;
                    }
                    write!(f, "{component}")?;
                }
                f.write_str("]")
            }
        }
    }
}

impl From<i64> for Value {
    fn from(v: i64) -> Self {
        Value::Integer(v)
    }
}

impl From<f64> for Value {
    fn from(v: f64) -> Self {
        Value::Real(v)
    }
}

impl From<&str> for Value {
    fn from(v: &str) -> Self {
        Value::Text(v.to_owned())
    }
}

impl From<String> for Value {
    fn from(v: String) -> Self {
        Value::Text(v)
    }
}

impl From<bool> for Value {
    fn from(v: bool) -> Self {
        Value::Boolean(v)
    }
}

/// The eager outcome of executing one or more SQL statements.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct QueryResult {
    columns: Vec<String>,
    rows: Vec<Vec<Value>>,
    rows_affected: usize,
    message: Option<String>,
}

impl QueryResult {
    /// Column names in result order. Empty for statements that return no rows.
    pub fn columns(&self) -> &[String] {
        &self.columns
    }

    /// Result rows. Empty for statements that return no rows.
    pub fn rows(&self) -> &[Vec<Value>] {
        &self.rows
    }

    /// Number of rows inserted, updated or deleted.
    pub fn rows_affected(&self) -> usize {
        self.rows_affected
    }

    /// Engine status message for non-query statements.
    pub fn message(&self) -> Option<&str> {
        self.message.as_deref()
    }

    /// Number of result rows.
    pub fn len(&self) -> usize {
        self.rows.len()
    }

    /// True when the statement produced no rows.
    pub fn is_empty(&self) -> bool {
        self.rows.is_empty()
    }

    /// A value by position.
    pub fn get(&self, row: usize, column: usize) -> Option<&Value> {
        self.rows.get(row)?.get(column)
    }

    /// The index of a column, matched case-insensitively.
    pub fn column_index(&self, name: &str) -> Option<usize> {
        self.columns
            .iter()
            .position(|column| column.eq_ignore_ascii_case(name))
    }

    /// A value by row and column name, matched case-insensitively.
    pub fn value(&self, row: usize, column: &str) -> Option<&Value> {
        self.get(row, self.column_index(column)?)
    }

    /// Consumes the result and returns its rows.
    pub fn into_rows(self) -> Vec<Vec<Value>> {
        self.rows
    }
}

/// A handle to an open YousDB database.
pub struct Database {
    handle: u64,
}

// The C ABI serializes calls per handle with its own mutex.
unsafe impl Send for Database {}

impl Database {
    /// Opens or creates a database file.
    pub fn open<P: AsRef<Path>>(path: P) -> Result<Self> {
        let path = path
            .as_ref()
            .to_str()
            .ok_or_else(|| Error::new("database path is not valid UTF-8"))?;
        let path =
            CString::new(path).map_err(|_| Error::new("database path contains a NUL byte"))?;

        let mut err: *mut c_char = ptr::null_mut();
        let handle = unsafe { ffi::yousdb_open(path.as_ptr() as *mut c_char, &mut err) };
        if handle == 0 {
            return Err(take_error(err));
        }
        Ok(Database { handle })
    }

    /// Executes one or more semicolon-separated statements.
    pub fn exec(&self, sql: &str) -> Result<QueryResult> {
        let sql = CString::new(sql).map_err(|_| Error::new("SQL contains a NUL byte"))?;
        let mut result = 0u64;
        let mut err: *mut c_char = ptr::null_mut();

        let ok = unsafe {
            ffi::yousdb_exec(
                self.handle,
                sql.as_ptr() as *mut c_char,
                &mut result,
                &mut err,
            )
        };
        if ok == 0 {
            return Err(take_error(err));
        }

        // collect_result always frees the native result handle.
        Ok(unsafe { collect_result(result) })
    }
}

impl Drop for Database {
    fn drop(&mut self) {
        if self.handle != 0 {
            unsafe { ffi::yousdb_close(self.handle) };
            self.handle = 0;
        }
    }
}

fn take_error(err: *mut c_char) -> Error {
    match unsafe { take_string(err) } {
        Some(message) => Error::new(message),
        None => Error::new("unknown database error"),
    }
}

/// Copies a malloc'd string into a `String` and frees the original.
unsafe fn take_string(ptr: *mut c_char) -> Option<String> {
    if ptr.is_null() {
        return None;
    }
    let value = CStr::from_ptr(ptr).to_string_lossy().into_owned();
    ffi::yousdb_free_string(ptr);
    Some(value)
}

unsafe fn read_value(handle: u64, row: c_int, col: c_int) -> Value {
    match ffi::yousdb_value_type(handle, row, col) {
        1 => Value::Integer(ffi::yousdb_value_int(handle, row, col)),
        2 => Value::Real(ffi::yousdb_value_real(handle, row, col)),
        3 => Value::Text(take_string(ffi::yousdb_value_text(handle, row, col)).unwrap_or_default()),
        4 => Value::Boolean(ffi::yousdb_value_bool(handle, row, col) != 0),
        5 => {
            let dimension = ffi::yousdb_value_vector_dimension(handle, row, col).max(0);
            let mut vector = Vec::with_capacity(dimension as usize);
            for index in 0..dimension {
                vector.push(ffi::yousdb_value_vector_element(handle, row, col, index));
            }
            Value::Vector(vector)
        }
        _ => Value::Null,
    }
}

unsafe fn collect_result(handle: u64) -> QueryResult {
    let column_count = ffi::yousdb_result_column_count(handle).max(0);
    let row_count = ffi::yousdb_result_row_count(handle).max(0);

    let mut columns = Vec::with_capacity(column_count as usize);
    for index in 0..column_count {
        columns
            .push(take_string(ffi::yousdb_result_column_name(handle, index)).unwrap_or_default());
    }

    let mut rows = Vec::with_capacity(row_count as usize);
    for row in 0..row_count {
        let mut values = Vec::with_capacity(column_count as usize);
        for col in 0..column_count {
            values.push(read_value(handle, row, col));
        }
        rows.push(values);
    }

    let rows_affected = ffi::yousdb_result_rows_affected(handle).max(0) as usize;
    let message = take_string(ffi::yousdb_result_message(handle));
    ffi::yousdb_result_free(handle);

    QueryResult {
        columns,
        rows,
        rows_affected,
        message,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn value_accessors() {
        assert!(Value::Null.is_null());
        assert_eq!(Value::Integer(3).as_i64(), Some(3));
        assert_eq!(Value::Real(1.5).as_f64(), Some(1.5));
        assert_eq!(Value::Integer(2).as_f64(), Some(2.0));
        assert_eq!(Value::Text("x".into()).as_str(), Some("x"));
        assert_eq!(Value::Boolean(true).as_bool(), Some(true));
        assert_eq!(Value::Null.to_string(), "NULL");
        assert_eq!(Value::Boolean(false).to_string(), "false");
        assert_eq!(Value::Real(3.5).to_string(), "3.5");
    }
}
