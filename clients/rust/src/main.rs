//! Command-line SQL shell for the YousDB Rust client.
//!
//! Usage:
//!   yousdb-rs -db app.db -c "SELECT 1;"
//!   yousdb-rs -db app.db "SELECT 1;"
//!   yousdb-rs -db app.db            # interactive REPL

use std::env;
use std::io::{self, BufRead, IsTerminal, Write};
use std::process::ExitCode;

use yousdb::{Database, QueryResult};

const USAGE: &str = "\
YousDB Rust client

Usage:
  yousdb-rs [options] [SQL]

Options:
  -db <path>       database file (default: yousdb.db)
  -c <sql>         execute SQL and exit
  -h, --help       show this help
";

struct Options {
    db: String,
    command: Option<String>,
    sql: Option<String>,
}

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(message) => {
            eprintln!("error: {message}");
            ExitCode::FAILURE
        }
    }
}

fn run() -> Result<(), String> {
    let options = parse_args(env::args().skip(1))?;

    let db = Database::open(&options.db).map_err(|e| e.to_string())?;

    if let Some(sql) = options.command.or(options.sql) {
        let result = db.exec(&sql).map_err(|e| e.to_string())?;
        print_result(&result);
        return Ok(());
    }

    repl(&db).map_err(|e| e.to_string())
}

fn parse_args(args: impl Iterator<Item = String>) -> Result<Options, String> {
    let mut options = Options {
        db: "yousdb.db".to_owned(),
        command: None,
        sql: None,
    };
    let mut rest = Vec::new();
    let mut args = args.peekable();

    while let Some(arg) = args.next() {
        match arg.as_str() {
            "-h" | "--help" => {
                print!("{USAGE}");
                std::process::exit(0);
            }
            "-db" | "--db" => {
                options.db = args
                    .next()
                    .ok_or_else(|| format!("{arg} requires a path"))?;
            }
            "-c" | "--command" => {
                options.command = Some(args.next().ok_or_else(|| format!("{arg} requires SQL"))?);
            }
            _ if arg.starts_with('-') && arg != "-" => {
                return Err(format!("unknown option {arg}\n\n{USAGE}"));
            }
            _ => rest.push(arg),
        }
    }

    if !rest.is_empty() {
        options.sql = Some(rest.join(" "));
    }
    Ok(options)
}

fn repl(db: &Database) -> io::Result<()> {
    let interactive = io::stdin().is_terminal();
    if interactive {
        show_banner();
    }

    let stdin = io::stdin();
    let mut buffer = String::new();
    let mut lines = stdin.lock().lines();

    loop {
        if interactive {
            let prompt = if buffer.is_empty() {
                "yousdb> "
            } else {
                "   ...> "
            };
            print!("{prompt}");
            io::stdout().flush()?;
        }

        let Some(line) = lines.next() else {
            break;
        };
        let line = line?;

        if buffer.is_empty() {
            match handle_meta_command(db, line.trim())
                .map_err(|error| io::Error::new(io::ErrorKind::Other, error))?
            {
                Some(true) => return Ok(()),
                Some(false) => continue,
                None => {}
            }
            if line.trim().is_empty() {
                continue;
            }
        }

        buffer.push_str(&line);
        buffer.push('\n');

        if !complete_statement(&buffer) {
            continue;
        }

        let sql = buffer.trim().to_owned();
        buffer.clear();
        execute(db, &sql);
    }

    if !buffer.trim().is_empty() {
        execute(db, buffer.trim());
    }
    Ok(())
}

fn show_banner() {
    for frame in ["·", "··", "···"] {
        print!("\r\x1b[36mStarting YousDB {frame}\x1b[0m");
        let _ = io::stdout().flush();
        std::thread::sleep(std::time::Duration::from_millis(70));
    }
    print!("\r\x1b[2K");
    println!("\x1b[1;36m __   __                         ____  ____  \x1b[0m");
    println!("\x1b[1;36m \\ \\ / /__  _   _ ___  ___   |  _ \\| __ ) \x1b[0m");
    println!("\x1b[1;36m  \\ V / _ \\| | | / __|/ _ \\  | | | |  _ \\ \x1b[0m");
    println!("\x1b[1;36m   | | (_) | |_| \\__ \\  __/  | |_| | |_) |\x1b[0m");
    println!("\x1b[1;36m   |_|\\___/ \\__,_|___/\\___|  |____/|____/ \x1b[0m");
    println!(
        "YousDB Rust shell · type \x1b[33m\\help\x1b[0m for shortcuts, \x1b[33m\\q\x1b[0m to quit."
    );
}

fn execute(db: &Database, sql: &str) {
    match db.exec(sql) {
        Ok(result) => print_result(&result),
        Err(err) => eprintln!("error: {err}"),
    }
}

fn print_result(result: &QueryResult) {
    if result.columns().is_empty() {
        if let Some(message) = result.message() {
            println!("{message}");
        }
        return;
    }

    let mut widths: Vec<usize> = result
        .columns()
        .iter()
        .map(|column| column.chars().count())
        .collect();

    let rendered: Vec<Vec<String>> = result
        .rows()
        .iter()
        .map(|row| {
            row.iter()
                .enumerate()
                .map(|(index, value)| {
                    let text = value.to_string();
                    widths[index] = widths[index].max(text.chars().count());
                    text
                })
                .collect()
        })
        .collect();

    print_row(result.columns().iter().map(String::as_str), &widths);
    for row in &rendered {
        print_row(row.iter().map(String::as_str), &widths);
    }

    let noun = if result.len() == 1 { "row" } else { "rows" };
    println!("({} {noun})", result.len());
}

fn print_row<'a>(cells: impl Iterator<Item = &'a str>, widths: &[usize]) {
    let cells: Vec<&str> = cells.collect();
    let mut line = String::new();
    for (index, cell) in cells.iter().enumerate() {
        if index > 0 {
            line.push_str("  ");
        }
        line.push_str(cell);
        if index + 1 < cells.len() {
            let pad = widths[index].saturating_sub(cell.chars().count());
            line.extend(std::iter::repeat(' ').take(pad));
        }
    }
    println!("{line}");
}

fn print_help() {
    print!(
        "YousDB shell commands:\n\
         \x20 \\q, \\quit        quit the shell\n\
         \x20 \\h, \\help        show this help\n\
         \x20 \\dt, \\tables      list tables\n\
         \x20 \\d TABLE          describe a table\n\
         \x20 \\vector           show a vector search example\n\
         \x20 \\clear, \\cls      clear the terminal\n\
         \n\
         SQL statements end with a semicolon. Examples:\n\
         \x20 CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, age INTEGER);\n\
         \x20 INSERT INTO users VALUES (1, 'Ada', 36);\n\
         \x20 SELECT name, age FROM users WHERE age > 30 ORDER BY age DESC;\n\
         \x20 BEGIN; ... COMMIT; / ROLLBACK;\n\
         \x20 SHOW TABLES;\n\
         \x20 DESCRIBE users;\n"
    );
}

/// Handles a shell command. `Some(true)` means quit, `Some(false)` means the
/// command was handled, and `None` means the input is SQL.
fn handle_meta_command(db: &Database, command: &str) -> Result<Option<bool>, String> {
    let fields: Vec<&str> = command.split_whitespace().collect();
    if fields.is_empty() {
        return Ok(Some(false));
    }
    match fields[0] {
        "\\q" | "\\quit" | "exit" | "quit" => Ok(Some(true)),
        "\\h" | "\\help" | "help" => {
            print_help();
            Ok(Some(false))
        }
        "\\dt" | "\\tables" => {
            let result = db.exec("SHOW TABLES;").map_err(|e| e.to_string())?;
            print_result(&result);
            Ok(Some(false))
        }
        "\\d" | "\\describe" => {
            if fields.len() != 2 {
                eprintln!("usage: \\d TABLE");
            } else {
                let sql = format!("DESCRIBE {};", fields[1]);
                let result = db.exec(&sql).map_err(|e| e.to_string())?;
                print_result(&result);
            }
            Ok(Some(false))
        }
        "\\vector" => {
            println!("Vector search example:\n  CREATE TABLE docs (id INTEGER PRIMARY KEY, embedding VECTOR(3));\n  SELECT id FROM docs ORDER BY cosine_distance(embedding, VECTOR(1, 0, 0)) LIMIT 10;");
            Ok(Some(false))
        }
        "\\clear" | "\\cls" => {
            print!("\x1b[H\x1b[2J");
            io::stdout().flush().map_err(|e| e.to_string())?;
            Ok(Some(false))
        }
        _ => Ok(None),
    }
}

/// Reports whether `sql` contains a semicolon outside string literals, quoted
/// identifiers and comments.
fn complete_statement(sql: &str) -> bool {
    let bytes = sql.as_bytes();
    let mut index = 0;
    let mut in_single = false;
    let mut in_double = false;
    let mut in_line_comment = false;
    let mut in_block_comment = false;

    while index < bytes.len() {
        let byte = bytes[index];
        if in_line_comment {
            if byte == b'\n' {
                in_line_comment = false;
            }
        } else if in_block_comment {
            if byte == b'*' && bytes.get(index + 1) == Some(&b'/') {
                in_block_comment = false;
                index += 1;
            }
        } else if in_single {
            if byte == b'\'' {
                if bytes.get(index + 1) == Some(&b'\'') {
                    index += 1;
                } else {
                    in_single = false;
                }
            }
        } else if in_double {
            if byte == b'"' {
                if bytes.get(index + 1) == Some(&b'"') {
                    index += 1;
                } else {
                    in_double = false;
                }
            }
        } else if byte == b'-' && bytes.get(index + 1) == Some(&b'-') {
            in_line_comment = true;
            index += 1;
        } else if byte == b'/' && bytes.get(index + 1) == Some(&b'*') {
            in_block_comment = true;
            index += 1;
        } else if byte == b'\'' {
            in_single = true;
        } else if byte == b'"' {
            in_double = true;
        } else if byte == b';' {
            return true;
        }
        index += 1;
    }
    false
}

#[cfg(test)]
mod tests {
    use super::complete_statement;

    #[test]
    fn detects_statement_terminators() {
        assert!(!complete_statement("SELECT 1"));
        assert!(complete_statement("SELECT 1;"));
        assert!(!complete_statement("SELECT ';'"));
        assert!(complete_statement("SELECT ';';"));
        assert!(!complete_statement("-- comment; here\nSELECT 1"));
        assert!(complete_statement("-- comment; here\nSELECT 1;"));
        assert!(!complete_statement("SELECT 1 /* ; */"));
        assert!(complete_statement("SELECT 1 /* ; */;"));
        assert!(!complete_statement(""));
    }
}
