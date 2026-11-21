package sql

import (
	"strings"
	"testing"
)

func corruptTableRow(t *testing.T, e *Engine, table string, which int) {
	t.Helper()
	tx, err := e.DB().Begin(true)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	prefix := tableRowPrefix(table)
	var keys, values [][]byte
	if err := tx.Scan(prefix, prefixEnd(prefix), func(key, value []byte) bool {
		keys = append(keys, append([]byte(nil), key...))
		values = append(values, append([]byte(nil), value...))
		return true
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if which < 0 || which >= len(values) {
		t.Fatalf("no row %d to corrupt (have %d)", which, len(values))
	}

	raw := values[which]
	corrupt := []byte{0xEE}
	if len(raw) > 1 {
		corrupt = append([]byte{0xEE}, raw[1:]...)
	}
	if err := tx.Set(keys[which], corrupt); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	committed = true
}

func TestSelectPropagatesRowDecodeError(t *testing.T) {
	e, _ := openEngine(t)
	defer e.Close()

	mustExec(t, e, "CREATE TABLE items (id INT PRIMARY KEY, name TEXT)")
	mustExec(t, e, "INSERT INTO items (id, name) VALUES (1, 'ok')")
	mustExec(t, e, "INSERT INTO items (id, name) VALUES (2, 'also')")
	corruptTableRow(t, e, "items", 0)

	res, err := e.Exec("SELECT * FROM items")
	if err == nil {
		t.Fatalf("SELECT succeeded after corrupting a stored row (rows=%d)", len(res.Rows))
	}
	if !strings.Contains(err.Error(), "unknown row tag") {
		t.Fatalf("SELECT error = %v, want unknown row tag", err)
	}

	res, err = e.Exec("SELECT name FROM items WHERE id = 2")
	if err == nil {
		t.Fatalf("filtered SELECT succeeded after corrupting a stored row (rows=%d)", len(res.Rows))
	}
	if !strings.Contains(err.Error(), "unknown row tag") {
		t.Fatalf("filtered SELECT error = %v, want unknown row tag", err)
	}
}
