// Command yousdb is the YousDB SQL shell.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Youssef-joe/YousDB/engine/sql"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	dbPath := flag.String("db", "yousdb.db", "path to the database file")
	command := flag.String("c", "", "execute SQL and exit")
	flag.Parse()

	engine, err := sql.Open(*dbPath)
	if err != nil {
		return err
	}
	defer engine.Close()

	if *command != "" {
		res, err := engine.Exec(*command)
		if err != nil {
			return err
		}
		printResult(res)
		return nil
	}

	if args := flag.Args(); len(args) > 0 {
		res, err := engine.Exec(strings.Join(args, " "))
		if err != nil {
			return err
		}
		printResult(res)
		return nil
	}

	return repl(engine)
}

func repl(engine *sql.Engine) error {
	interactive := false
	if info, err := os.Stdin.Stat(); err == nil {
		interactive = info.Mode()&os.ModeCharDevice != 0
	}

	if interactive {
		showBanner()
	}

	reader := bufio.NewReader(os.Stdin)
	var buffer strings.Builder

	for {
		if interactive {
			if buffer.Len() == 0 {
				fmt.Print("yousdb> ")
			} else {
				fmt.Print("   ...> ")
			}
		}

		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if line == "" {
			break
		}
		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")

		trimmed := strings.TrimSpace(line)
		if buffer.Len() == 0 {
			handled, quit := handleMetaCommand(engine, trimmed)
			if quit {
				return nil
			}
			if handled {
				continue
			}
		}

		buffer.WriteString(line)
		buffer.WriteString("\n")

		if !completeStatement(buffer.String()) {
			if err != nil {
				break
			}
			continue
		}

		query := strings.TrimSpace(buffer.String())
		buffer.Reset()
		execute(engine, query)
	}

	if strings.TrimSpace(buffer.String()) != "" {
		execute(engine, strings.TrimSpace(buffer.String()))
	}
	return nil
}

func showBanner() {
	frames := []string{"·", "··", "···"}
	for _, frame := range frames {
		fmt.Printf("\r\033[36mStarting YousDB %s\033[0m", frame)
		time.Sleep(70 * time.Millisecond)
	}
	fmt.Print("\r\033[2K")
	fmt.Println("\033[1;36m __   __                         ____  ____  ")
	fmt.Println("\033[1;36m \\ \\ / /__  _   _ ___  ___   |  _ \\| __ ) ")
	fmt.Println("\033[1;36m  \\ V / _ \\| | | / __|/ _ \\  | | | |  _ \\ ")
	fmt.Println("\033[1;36m   | | (_) | |_| \\__ \\  __/  | |_| | |_) |")
	fmt.Println("\033[1;36m   |_|\\___/ \\__,_|___/\\___|  |____/|____/ \\033[0m")
	fmt.Println("YousDB shell · type \\033[33m\\help\\033[0m for shortcuts, \\033[33m\\q\\033[0m to quit.")
}

func execute(engine *sql.Engine, query string) {
	res, err := engine.Exec(query)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return
	}
	printResult(res)
}

func printResult(res *sql.Result) {
	if len(res.Columns) > 0 {
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, strings.Join(res.Columns, "\t"))
		for _, row := range res.Rows {
			cells := make([]string, len(row))
			for i, value := range row {
				cells[i] = value.String()
			}
			fmt.Fprintln(w, strings.Join(cells, "\t"))
		}
		if err := w.Flush(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return
		}
		noun := "rows"
		if len(res.Rows) == 1 {
			noun = "row"
		}
		fmt.Printf("(%d %s)\n", len(res.Rows), noun)
		return
	}
	if res.Message != "" {
		fmt.Println(res.Message)
	}
}

func printHelp() {
	fmt.Print(`YousDB shell commands:
  \q, \quit        quit the shell
  \h, \help        show this help
  \dt, \tables      list tables
  \d TABLE          describe a table
  \vector           show a vector search example
  \clear, \cls      clear the terminal

SQL statements end with a semicolon. Examples:
  CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, age INTEGER);
  INSERT INTO users VALUES (1, 'Ada', 36);
  SELECT name, age FROM users WHERE age > 30 ORDER BY age DESC;
  BEGIN; ... COMMIT; / ROLLBACK;
  SHOW TABLES;
  DESCRIBE users;
`)
}

func handleMetaCommand(engine *sql.Engine, command string) (handled, quit bool) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return true, false
	}
	switch fields[0] {
	case "\\q", "\\quit", "exit", "quit":
		return true, true
	case "\\h", "\\help", "help":
		printHelp()
		return true, false
	case "\\dt", "\\tables":
		execute(engine, "SHOW TABLES;")
		return true, false
	case "\\d", "\\describe":
		if len(fields) != 2 {
			fmt.Fprintln(os.Stderr, "usage: \\d TABLE")
		} else {
			execute(engine, "DESCRIBE "+fields[1]+";")
		}
		return true, false
	case "\\vector":
		fmt.Print("Vector search example:\n  CREATE TABLE docs (id INTEGER PRIMARY KEY, embedding VECTOR(3));\n  SELECT id FROM docs ORDER BY cosine_distance(embedding, VECTOR(1, 0, 0)) LIMIT 10;\n")
		return true, false
	case "\\clear", "\\cls":
		fmt.Print("\033[H\033[2J")
		return true, false
	default:
		return false, false
	}
}

// completeStatement reports whether s contains a semicolon outside of string
// literals, quoted identifiers and comments.
func completeStatement(s string) bool {
	inSingle := false
	inDouble := false
	inLineComment := false
	inBlockComment := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inLineComment:
			if c == '\n' {
				inLineComment = false
			}
		case inBlockComment:
			if c == '*' && i+1 < len(s) && s[i+1] == '/' {
				inBlockComment = false
				i++
			}
		case inSingle:
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
				} else {
					inSingle = false
				}
			}
		case inDouble:
			if c == '"' {
				if i+1 < len(s) && s[i+1] == '"' {
					i++
				} else {
					inDouble = false
				}
			}
		default:
			switch {
			case c == '-' && i+1 < len(s) && s[i+1] == '-':
				inLineComment = true
				i++
			case c == '/' && i+1 < len(s) && s[i+1] == '*':
				inBlockComment = true
				i++
			case c == '\'':
				inSingle = true
			case c == '"':
				inDouble = true
			case c == ';':
				return true
			}
		}
	}
	return false
}
