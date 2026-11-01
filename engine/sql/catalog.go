package sql

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Youssef-joe/YousDB/engine/db"
)

var (
	// ErrTableNotFound is returned when a table does not exist.
	ErrTableNotFound = errors.New("sql: table not found")
	// ErrTableExists is returned when CREATE TABLE finds an existing table.
	ErrTableExists = errors.New("sql: table already exists")
	// ErrColumnNotFound is returned for an unknown column.
	ErrColumnNotFound = errors.New("sql: column not found")
)

var (
	schemaPrefix = []byte("!schema/")
	rowPrefixKey = []byte("!row/")
)

// Column describes one column of a table.
type Column struct {
	Name       string
	Type       DataType
	PrimaryKey bool
	NotNull    bool
	Unique     bool
	Hidden     bool
	Default    *Value
}

// Table is a stored schema.
type Table struct {
	Name      string
	Columns   []Column
	PK        int
	NextRowID int64
}

// ColumnIndex finds a column by name, case-insensitively.
func (t *Table) ColumnIndex(name string) (int, bool) {
	for i, col := range t.Columns {
		if strings.EqualFold(col.Name, name) {
			return i, true
		}
	}
	return 0, false
}

// VisibleColumns returns the indexes of columns shown by SELECT *.
func (t *Table) VisibleColumns() []int {
	var idx []int
	for i, col := range t.Columns {
		if !col.Hidden {
			idx = append(idx, i)
		}
	}
	return idx
}

func schemaKey(name string) []byte {
	return append(append([]byte(nil), schemaPrefix...), []byte(hex.EncodeToString([]byte(strings.ToLower(name))))...)
}

func tableRowPrefix(name string) []byte {
	prefix := append([]byte(nil), rowPrefixKey...)
	prefix = append(prefix, []byte(hex.EncodeToString([]byte(strings.ToLower(name))))...)
	return append(prefix, '/')
}

func rowKey(name string, pk []byte) []byte {
	prefix := tableRowPrefix(name)
	key := make([]byte, 0, len(prefix)+len(pk))
	key = append(key, prefix...)
	key = append(key, pk...)
	return key
}

func prefixEnd(prefix []byte) []byte {
	end := append([]byte(nil), prefix...)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] < 0xff {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}

// LoadTable reads a table schema inside tx.
func LoadTable(tx *db.Tx, name string) (*Table, error) {
	data, err := tx.Get(schemaKey(name))
	if errors.Is(err, db.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrTableNotFound, name)
	}
	if err != nil {
		return nil, err
	}
	var table Table
	if err := json.Unmarshal(data, &table); err != nil {
		return nil, fmt.Errorf("sql: decode schema for %s: %w", name, err)
	}
	return &table, nil
}

// SaveTable writes a table schema inside tx.
func SaveTable(tx *db.Tx, table *Table) error {
	data, err := json.Marshal(table)
	if err != nil {
		return err
	}
	return tx.Set(schemaKey(table.Name), data)
}

// ListTables returns all tables sorted by name.
func ListTables(tx *db.Tx) ([]*Table, error) {
	var tables []*Table
	var scanErr error
	err := tx.Scan(schemaPrefix, prefixEnd(schemaPrefix), func(_, value []byte) bool {
		var table Table
		if err := json.Unmarshal(value, &table); err != nil {
			scanErr = err
			return false
		}
		tables = append(tables, &table)
		return true
	})
	if err != nil {
		return nil, err
	}
	if scanErr != nil {
		return nil, scanErr
	}
	sort.Slice(tables, func(i, j int) bool {
		return strings.ToLower(tables[i].Name) < strings.ToLower(tables[j].Name)
	})
	return tables, nil
}

// DropTableSchema removes a table's schema and all of its rows.
func DropTableSchema(tx *db.Tx, table *Table) error {
	prefix := tableRowPrefix(table.Name)
	var keys [][]byte
	if err := tx.Scan(prefix, prefixEnd(prefix), func(key, _ []byte) bool {
		keys = append(keys, append([]byte(nil), key...))
		return true
	}); err != nil {
		return err
	}
	for _, key := range keys {
		tx.Delete(key)
	}
	tx.Delete(schemaKey(table.Name))
	return nil
}

// buildTable validates a CREATE TABLE statement and produces a schema.
func buildTable(stmt *CreateTable) (*Table, error) {
	if len(stmt.Columns) == 0 {
		return nil, fmt.Errorf("sql: table %s needs at least one column", stmt.Name)
	}
	table := &Table{Name: stmt.Name, NextRowID: 1}
	seen := make(map[string]bool)
	for _, def := range stmt.Columns {
		key := strings.ToLower(def.Name)
		if seen[key] {
			return nil, fmt.Errorf("sql: duplicate column %q", def.Name)
		}
		seen[key] = true
		col := Column{
			Name:       def.Name,
			Type:       def.Type,
			PrimaryKey: def.PrimaryKey,
			NotNull:    def.NotNull,
			Unique:     def.Unique,
		}
		if col.PrimaryKey {
			col.NotNull = true
		}
		if def.Default != nil {
			value, err := evalExpr(def.Default, &evalContext{})
			if err != nil {
				return nil, fmt.Errorf("sql: default for %s: %w", def.Name, err)
			}
			value, err = Coerce(value, col.Type)
			if err != nil {
				return nil, fmt.Errorf("sql: default for %s: %w", def.Name, err)
			}
			if value.Null && col.NotNull {
				return nil, fmt.Errorf("sql: default for NOT NULL column %s cannot be NULL", def.Name)
			}
			col.Default = &value
		}
		table.Columns = append(table.Columns, col)
	}

	pk := -1
	for i, col := range table.Columns {
		if col.PrimaryKey {
			if pk != -1 {
				return nil, fmt.Errorf("sql: only one PRIMARY KEY column is supported")
			}
			pk = i
		}
	}
	if pk == -1 {
		if seen["rowid"] {
			return nil, fmt.Errorf("sql: column name %q is reserved", "rowid")
		}
		table.Columns = append(table.Columns, Column{
			Name:       "rowid",
			Type:       IntegerType,
			PrimaryKey: true,
			NotNull:    true,
			Hidden:     true,
		})
		pk = len(table.Columns) - 1
	}
	if typ := table.Columns[pk].Type; typ != IntegerType && typ != TextType {
		return nil, fmt.Errorf("sql: PRIMARY KEY must be INTEGER or TEXT, got %s", typ)
	}
	table.PK = pk
	return table, nil
}
