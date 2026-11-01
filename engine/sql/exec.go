package sql

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Youssef-joe/YousDB/engine/db"
)

var (
	// ErrConstraint is returned when a row violates a schema constraint.
	ErrConstraint = errors.New("sql: constraint violation")
	// ErrNoTransaction is returned by COMMIT/ROLLBACK without BEGIN.
	ErrNoTransaction = errors.New("sql: no active transaction")
	// ErrTxActive is returned by BEGIN while a transaction is active.
	ErrTxActive = errors.New("sql: transaction already active")
)

// Result is the outcome of executing SQL.
type Result struct {
	Columns      []string
	Rows         [][]Value
	RowsAffected int
	Message      string
}

// Engine is a SQL database backed by the durable KV store.
type Engine struct {
	db *db.DB
	tx *db.Tx
}

// Open opens or creates a SQL database file.
func Open(path string) (*Engine, error) {
	return OpenWithOptions(path, db.Options{})
}

// OpenWithOptions opens a SQL database with explicit options.
func OpenWithOptions(path string, opts db.Options) (*Engine, error) {
	d, err := db.OpenWithOptions(path, opts)
	if err != nil {
		return nil, err
	}
	return &Engine{db: d}, nil
}

// Close rolls back any active transaction and closes the database.
func (e *Engine) Close() error {
	if e.tx != nil {
		e.tx.Rollback()
		e.tx = nil
	}
	return e.db.Close()
}

// DB returns the underlying key-value database.
func (e *Engine) DB() *db.DB { return e.db }

// InTransaction reports whether an explicit transaction is active.
func (e *Engine) InTransaction() bool { return e.tx != nil }

// Exec parses and executes one or more semicolon-separated statements.
func (e *Engine) Exec(input string) (*Result, error) {
	stmts, err := Parse(input)
	if err != nil {
		return nil, err
	}
	if len(stmts) == 0 {
		return &Result{}, nil
	}
	var last *Result
	for _, stmt := range stmts {
		res, err := e.execute(stmt)
		if err != nil {
			return nil, err
		}
		last = res
	}
	return last, nil
}

func (e *Engine) execute(stmt Statement) (*Result, error) {
	switch stmt.(type) {
	case *Begin:
		if e.tx != nil {
			return nil, ErrTxActive
		}
		tx, err := e.db.Begin(true)
		if err != nil {
			return nil, err
		}
		e.tx = tx
		return &Result{Message: "BEGIN"}, nil

	case *Commit:
		if e.tx == nil {
			return nil, ErrNoTransaction
		}
		tx := e.tx
		e.tx = nil
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &Result{Message: "COMMIT"}, nil

	case *Rollback:
		if e.tx == nil {
			return nil, ErrNoTransaction
		}
		e.tx.Rollback()
		e.tx = nil
		return &Result{Message: "ROLLBACK"}, nil
	}

	if e.tx != nil {
		return execStatement(e.tx, stmt)
	}

	var res *Result
	err := e.db.Update(func(tx *db.Tx) error {
		var err error
		res, err = execStatement(tx, stmt)
		return err
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func execStatement(tx *db.Tx, stmt Statement) (*Result, error) {
	switch s := stmt.(type) {
	case *CreateTable:
		return execCreateTable(tx, s)
	case *DropTable:
		return execDropTable(tx, s)
	case *Insert:
		return execInsert(tx, s)
	case *Select:
		return execSelect(tx, s)
	case *Update:
		return execUpdate(tx, s)
	case *Delete:
		return execDelete(tx, s)
	case *ShowTables:
		return execShowTables(tx)
	case *Describe:
		return execDescribe(tx, s)
	default:
		return nil, fmt.Errorf("sql: unsupported statement %T", stmt)
	}
}

func execCreateTable(tx *db.Tx, stmt *CreateTable) (*Result, error) {
	if _, err := LoadTable(tx, stmt.Name); err == nil {
		if stmt.IfNotExists {
			return &Result{Message: fmt.Sprintf("table %s already exists", stmt.Name)}, nil
		}
		return nil, fmt.Errorf("%w: %s", ErrTableExists, stmt.Name)
	} else if !errors.Is(err, ErrTableNotFound) {
		return nil, err
	}

	table, err := buildTable(stmt)
	if err != nil {
		return nil, err
	}
	if err := SaveTable(tx, table); err != nil {
		return nil, err
	}
	return &Result{Message: fmt.Sprintf("table %s created", table.Name)}, nil
}

func execDropTable(tx *db.Tx, stmt *DropTable) (*Result, error) {
	table, err := LoadTable(tx, stmt.Name)
	if errors.Is(err, ErrTableNotFound) {
		if stmt.IfExists {
			return &Result{Message: fmt.Sprintf("table %s does not exist", stmt.Name)}, nil
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if err := DropTableSchema(tx, table); err != nil {
		return nil, err
	}
	return &Result{Message: fmt.Sprintf("table %s dropped", table.Name)}, nil
}

func execInsert(tx *db.Tx, stmt *Insert) (res *Result, err error) {
	table, err := LoadTable(tx, stmt.Table)
	if err != nil {
		return nil, err
	}

	var target []int
	if len(stmt.Columns) == 0 {
		target = table.VisibleColumns()
	} else {
		seen := make(map[int]bool)
		for _, name := range stmt.Columns {
			idx, ok := table.ColumnIndex(name)
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrColumnNotFound, name)
			}
			if seen[idx] {
				return nil, fmt.Errorf("sql: column %q specified more than once", name)
			}
			seen[idx] = true
			target = append(target, idx)
		}
	}

	dirtySchema := false
	// Persist NextRowID even if a later row fails. In an explicit transaction
	// the earlier rows remain buffered, so the counter must not be reused.
	defer func() {
		if dirtySchema {
			if serr := SaveTable(tx, table); serr != nil && err == nil {
				res = nil
				err = serr
			}
		}
	}()
	affected := 0
	for _, exprs := range stmt.Rows {
		if len(exprs) != len(target) {
			return nil, fmt.Errorf("sql: INSERT has %d values for %d columns", len(exprs), len(target))
		}
		row := make([]Value, len(table.Columns))
		specified := make([]bool, len(table.Columns))
		for i, expr := range exprs {
			idx := target[i]
			value, err := evalExpr(expr, &evalContext{})
			if err != nil {
				return nil, err
			}
			value, err = CoerceColumn(value, table.Columns[idx])
			if err != nil {
				return nil, fmt.Errorf("sql: column %s: %w", table.Columns[idx].Name, err)
			}
			row[idx] = value
			specified[idx] = true
		}

		for i, col := range table.Columns {
			if specified[i] {
				continue
			}
			if col.Default != nil {
				row[i] = *col.Default
			} else {
				row[i] = Value{Type: col.Type, Null: true}
			}
		}

		pkCol := table.Columns[table.PK]

		// Check NOT NULL before allocating an auto rowid so a failed row does
		// not consume an id.
		for i, col := range table.Columns {
			if i == table.PK && pkCol.Hidden && !specified[i] {
				continue
			}
			if col.NotNull && row[i].Null {
				return nil, fmt.Errorf("%w: column %s cannot be NULL", ErrConstraint, col.Name)
			}
		}

		if pkCol.Hidden && !specified[table.PK] {
			row[table.PK] = IntValue(table.NextRowID)
			table.NextRowID++
			dirtySchema = true
		} else if pkCol.Hidden && row[table.PK].Type == IntegerType && !row[table.PK].Null {
			if row[table.PK].Int >= table.NextRowID {
				table.NextRowID = row[table.PK].Int + 1
				dirtySchema = true
			}
		}

		if row[table.PK].Null {
			return nil, fmt.Errorf("%w: primary key %s cannot be NULL", ErrConstraint, pkCol.Name)
		}

		if err := checkUnique(tx, table, row, nil); err != nil {
			return nil, err
		}

		pk, err := encodePK(row[table.PK])
		if err != nil {
			return nil, err
		}
		key := rowKey(table.Name, pk)
		if _, err := tx.Get(key); err == nil {
			return nil, fmt.Errorf("%w: duplicate primary key in %s", ErrConstraint, table.Name)
		} else if !errors.Is(err, db.ErrNotFound) {
			return nil, err
		}

		value, err := encodeRow(row)
		if err != nil {
			return nil, err
		}
		if err := tx.Set(key, value); err != nil {
			return nil, err
		}
		affected++
	}

	return &Result{RowsAffected: affected, Message: fmt.Sprintf("%d row(s) inserted", affected)}, nil
}

func execSelect(tx *db.Tx, stmt *Select) (*Result, error) {
	if stmt.Table == "" {
		return execSelectNoFrom(stmt)
	}

	table, err := LoadTable(tx, stmt.Table)
	if err != nil {
		return nil, err
	}

	columns, exprs, err := expandSelectItems(table, stmt.Items)
	if err != nil {
		return nil, err
	}

	for _, item := range stmt.Items {
		if item.Star {
			continue
		}
		if err := validateExpr(item.Expr, table); err != nil {
			return nil, err
		}
	}
	if stmt.Where != nil {
		if err := validateExpr(stmt.Where, table); err != nil {
			return nil, err
		}
	}
	for _, expr := range stmt.GroupBy {
		if err := validateExpr(expr, table); err != nil {
			return nil, err
		}
	}
	if stmt.Having != nil {
		if err := validateExpr(stmt.Having, table); err != nil {
			return nil, err
		}
	}
	for _, item := range stmt.OrderBy {
		if n, ok := parseOrdinal(item.Expr); ok {
			if n < 1 || n > len(columns) {
				return nil, fmt.Errorf("sql: ORDER BY position %d is out of range", n)
			}
			continue
		}
		if ref, ok := item.Expr.(*ColumnRef); ok && ref.Table == "" {
			if _, ok := aliasIndex(ref, columns); ok {
				continue
			}
		}
		if err := validateExpr(item.Expr, table); err != nil {
			return nil, err
		}
	}

	rows, err := scanTable(tx, table)
	if err != nil {
		return nil, err
	}

	if stmt.Where != nil {
		filtered := rows[:0]
		for _, row := range rows {
			keep, err := evalPredicate(stmt.Where, &evalContext{table: table, row: row})
			if err != nil {
				return nil, err
			}
			if keep {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}

	grouped := len(stmt.GroupBy) > 0 || stmt.Having != nil
	for _, expr := range exprs {
		if containsAggregate(expr) {
			grouped = true
		}
	}
	for _, item := range stmt.OrderBy {
		if containsAggregate(item.Expr) {
			grouped = true
		}
	}

	type outRow struct {
		values []Value
		order  []Value
	}
	var out []outRow

	if grouped {
		groups, err := groupRows(table, rows, stmt.GroupBy)
		if err != nil {
			return nil, err
		}
		for _, group := range groups {
			ctx := &evalContext{table: table, rows: group, group: true}
			if len(group) > 0 {
				ctx.row = group[0]
			}
			if stmt.Having != nil {
				keep, err := evalPredicate(stmt.Having, ctx)
				if err != nil {
					return nil, err
				}
				if !keep {
					continue
				}
			}
			values := make([]Value, len(exprs))
			for i, expr := range exprs {
				v, err := evalExpr(expr, ctx)
				if err != nil {
					return nil, err
				}
				values[i] = v
			}
			order := make([]Value, len(stmt.OrderBy))
			for i, item := range stmt.OrderBy {
				v, err := orderValue(item, ctx, values, columns)
				if err != nil {
					return nil, err
				}
				order[i] = v
			}
			out = append(out, outRow{values: values, order: order})
		}
	} else {
		for _, row := range rows {
			ctx := &evalContext{table: table, row: row}
			values := make([]Value, len(exprs))
			for i, expr := range exprs {
				v, err := evalExpr(expr, ctx)
				if err != nil {
					return nil, err
				}
				values[i] = v
			}
			order := make([]Value, len(stmt.OrderBy))
			for i, item := range stmt.OrderBy {
				v, err := orderValue(item, ctx, values, columns)
				if err != nil {
					return nil, err
				}
				order[i] = v
			}
			out = append(out, outRow{values: values, order: order})
		}
	}

	if stmt.Distinct {
		seen := make(map[string]bool)
		deduped := out[:0]
		for _, row := range out {
			key := rowKeyForValues(row.values)
			if seen[key] {
				continue
			}
			seen[key] = true
			deduped = append(deduped, row)
		}
		out = deduped
	}

	if len(stmt.OrderBy) > 0 {
		sort.SliceStable(out, func(i, j int) bool {
			for k, item := range stmt.OrderBy {
				cmp := CompareOrder(out[i].order[k], out[j].order[k])
				if cmp == 0 {
					continue
				}
				if item.Desc {
					return cmp > 0
				}
				return cmp < 0
			}
			return false
		})
	}

	offset := 0
	if stmt.Offset != nil {
		offset = *stmt.Offset
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(out) {
		offset = len(out)
	}
	out = out[offset:]
	if stmt.Limit != nil {
		limit := *stmt.Limit
		if limit < 0 {
			limit = 0
		}
		if limit < len(out) {
			out = out[:limit]
		}
	}

	resultRows := make([][]Value, len(out))
	for i, row := range out {
		resultRows[i] = row.values
	}
	return &Result{Columns: columns, Rows: resultRows}, nil
}

func execSelectNoFrom(stmt *Select) (*Result, error) {
	if stmt.Where != nil {
		return nil, fmt.Errorf("sql: WHERE requires a FROM clause")
	}
	if len(stmt.GroupBy) > 0 || stmt.Having != nil {
		return nil, fmt.Errorf("sql: GROUP BY and HAVING require a FROM clause")
	}

	columns, exprs, err := expandSelectItems(nil, stmt.Items)
	if err != nil {
		return nil, err
	}
	for _, item := range stmt.Items {
		if item.Star {
			return nil, fmt.Errorf("sql: SELECT * requires a FROM clause")
		}
		if err := validateExpr(item.Expr, nil); err != nil {
			return nil, err
		}
	}
	for _, item := range stmt.OrderBy {
		if n, ok := parseOrdinal(item.Expr); ok {
			if n < 1 || n > len(columns) {
				return nil, fmt.Errorf("sql: ORDER BY position %d is out of range", n)
			}
			continue
		}
		if ref, ok := item.Expr.(*ColumnRef); ok && ref.Table == "" {
			if _, ok := aliasIndex(ref, columns); ok {
				continue
			}
		}
		if err := validateExpr(item.Expr, nil); err != nil {
			return nil, err
		}
	}

	grouped := false
	for _, expr := range exprs {
		if containsAggregate(expr) {
			grouped = true
		}
	}
	ctx := &evalContext{group: grouped}
	values := make([]Value, len(exprs))
	for i, expr := range exprs {
		v, err := evalExpr(expr, ctx)
		if err != nil {
			return nil, err
		}
		values[i] = v
	}
	order := make([]Value, len(stmt.OrderBy))
	for i, item := range stmt.OrderBy {
		v, err := orderValue(item, ctx, values, columns)
		if err != nil {
			return nil, err
		}
		order[i] = v
	}

	out := [][]Value{values}
	offset := 0
	if stmt.Offset != nil {
		offset = *stmt.Offset
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(out) {
		offset = len(out)
	}
	out = out[offset:]
	if stmt.Limit != nil {
		limit := *stmt.Limit
		if limit < 0 {
			limit = 0
		}
		if limit < len(out) {
			out = out[:limit]
		}
	}
	return &Result{Columns: columns, Rows: out}, nil
}

func execUpdate(tx *db.Tx, stmt *Update) (res *Result, err error) {
	table, err := LoadTable(tx, stmt.Table)
	if err != nil {
		return nil, err
	}

	assignments := make([]int, len(stmt.Assignments))
	seen := make(map[int]bool)
	for i, a := range stmt.Assignments {
		idx, ok := table.ColumnIndex(a.Column)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrColumnNotFound, a.Column)
		}
		if seen[idx] {
			return nil, fmt.Errorf("sql: column %q assigned more than once", a.Column)
		}
		seen[idx] = true
		assignments[i] = idx
		if err := validateExpr(a.Value, table); err != nil {
			return nil, err
		}
	}
	if stmt.Where != nil {
		if err := validateExpr(stmt.Where, table); err != nil {
			return nil, err
		}
	}

	dirtySchema := false
	defer func() {
		if dirtySchema {
			if serr := SaveTable(tx, table); serr != nil && err == nil {
				res = nil
				err = serr
			}
		}
	}()

	type match struct {
		key []byte
		row []Value
	}
	var matches []match
	var scanErr error
	prefix := tableRowPrefix(table.Name)
	if err := tx.Scan(prefix, prefixEnd(prefix), func(key, value []byte) bool {
		row, err := decodeRow(value, table)
		if err != nil {
			scanErr = err
			return false
		}
		if stmt.Where != nil {
			keep, perr := evalPredicate(stmt.Where, &evalContext{table: table, row: row})
			if perr != nil {
				scanErr = perr
				return false
			}
			if !keep {
				return true
			}
		}
		matches = append(matches, match{key: append([]byte(nil), key...), row: row})
		return true
	}); err != nil {
		return nil, err
	}
	if scanErr != nil {
		return nil, scanErr
	}

	affected := 0
	for _, m := range matches {
		newRow := append([]Value(nil), m.row...)
		for i, a := range stmt.Assignments {
			idx := assignments[i]
			value, err := evalExpr(a.Value, &evalContext{table: table, row: m.row})
			if err != nil {
				return nil, err
			}
			value, err = CoerceColumn(value, table.Columns[idx])
			if err != nil {
				return nil, fmt.Errorf("sql: column %s: %w", table.Columns[idx].Name, err)
			}
			newRow[idx] = value
		}

		for i, col := range table.Columns {
			if col.NotNull && newRow[i].Null {
				return nil, fmt.Errorf("%w: column %s cannot be NULL", ErrConstraint, col.Name)
			}
		}

		pkCol := table.Columns[table.PK]
		if pkCol.Hidden && newRow[table.PK].Type == IntegerType && !newRow[table.PK].Null {
			if newRow[table.PK].Int >= table.NextRowID {
				table.NextRowID = newRow[table.PK].Int + 1
				dirtySchema = true
			}
		}

		if err := checkUnique(tx, table, newRow, m.key); err != nil {
			return nil, err
		}

		oldPK, err := encodePK(m.row[table.PK])
		if err != nil {
			return nil, err
		}
		newPK, err := encodePK(newRow[table.PK])
		if err != nil {
			return nil, err
		}
		oldKey := rowKey(table.Name, oldPK)
		newKey := rowKey(table.Name, newPK)

		if !bytes.Equal(oldKey, newKey) {
			if _, err := tx.Get(newKey); err == nil {
				return nil, fmt.Errorf("%w: duplicate primary key in %s", ErrConstraint, table.Name)
			} else if !errors.Is(err, db.ErrNotFound) {
				return nil, err
			}
			tx.Delete(oldKey)
		}

		value, err := encodeRow(newRow)
		if err != nil {
			return nil, err
		}
		if err := tx.Set(newKey, value); err != nil {
			return nil, err
		}
		affected++
	}

	return &Result{RowsAffected: affected, Message: fmt.Sprintf("%d row(s) updated", affected)}, nil
}

func execDelete(tx *db.Tx, stmt *Delete) (*Result, error) {
	table, err := LoadTable(tx, stmt.Table)
	if err != nil {
		return nil, err
	}
	if stmt.Where != nil {
		if err := validateExpr(stmt.Where, table); err != nil {
			return nil, err
		}
	}

	var keys [][]byte
	var scanErr error
	prefix := tableRowPrefix(table.Name)
	if err := tx.Scan(prefix, prefixEnd(prefix), func(key, value []byte) bool {
		if stmt.Where == nil {
			keys = append(keys, append([]byte(nil), key...))
			return true
		}
		row, err := decodeRow(value, table)
		if err != nil {
			scanErr = err
			return false
		}
		keep, err := evalPredicate(stmt.Where, &evalContext{table: table, row: row})
		if err != nil {
			scanErr = err
			return false
		}
		if keep {
			keys = append(keys, append([]byte(nil), key...))
		}
		return true
	}); err != nil {
		return nil, err
	}
	if scanErr != nil {
		return nil, scanErr
	}

	for _, key := range keys {
		tx.Delete(key)
	}
	return &Result{RowsAffected: len(keys), Message: fmt.Sprintf("%d row(s) deleted", len(keys))}, nil
}

func execShowTables(tx *db.Tx) (*Result, error) {
	tables, err := ListTables(tx)
	if err != nil {
		return nil, err
	}
	rows := make([][]Value, len(tables))
	for i, table := range tables {
		rows[i] = []Value{TextValue(table.Name)}
	}
	return &Result{Columns: []string{"table"}, Rows: rows}, nil
}

func execDescribe(tx *db.Tx, stmt *Describe) (*Result, error) {
	table, err := LoadTable(tx, stmt.Table)
	if err != nil {
		return nil, err
	}
	rows := make([][]Value, len(table.Columns))
	for i, col := range table.Columns {
		defaultValue := NullValue()
		if col.Default != nil {
			defaultValue = *col.Default
		}
		nullable := "YES"
		if col.NotNull {
			nullable = "NO"
		}
		primary := "NO"
		if col.PrimaryKey {
			primary = "YES"
		}
		typeName := col.Type.String()
		if col.Type == VectorType {
			typeName = fmt.Sprintf("VECTOR(%d)", col.VectorDim)
		}
		rows[i] = []Value{
			TextValue(col.Name),
			TextValue(typeName),
			TextValue(nullable),
			defaultValue,
			TextValue(primary),
		}
	}
	return &Result{
		Columns: []string{"column", "type", "nullable", "default", "primary_key"},
		Rows:    rows,
	}, nil
}

func scanTable(tx *db.Tx, table *Table) ([][]Value, error) {
	var rows [][]Value
	prefix := tableRowPrefix(table.Name)
	err := tx.Scan(prefix, prefixEnd(prefix), func(_, value []byte) bool {
		row, err := decodeRow(value, table)
		if err != nil {
			return false
		}
		rows = append(rows, row)
		return true
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func expandSelectItems(table *Table, items []SelectItem) ([]string, []Expr, error) {
	var columns []string
	var exprs []Expr
	for _, item := range items {
		if item.Star {
			if table == nil {
				return nil, nil, fmt.Errorf("sql: SELECT * requires a FROM clause")
			}
			for _, idx := range table.VisibleColumns() {
				col := table.Columns[idx]
				columns = append(columns, col.Name)
				exprs = append(exprs, &ColumnRef{Name: col.Name})
			}
			continue
		}
		name := item.Alias
		if name == "" {
			if ref, ok := item.Expr.(*ColumnRef); ok {
				name = ref.Name
			} else {
				name = FormatExpr(item.Expr)
			}
		}
		columns = append(columns, name)
		exprs = append(exprs, item.Expr)
	}
	if len(columns) == 0 {
		return nil, nil, fmt.Errorf("sql: SELECT list is empty")
	}
	return columns, exprs, nil
}

func groupRows(table *Table, rows [][]Value, exprs []Expr) ([][][]Value, error) {
	if len(exprs) == 0 {
		return [][][]Value{rows}, nil
	}
	index := make(map[string]int)
	var groups [][][]Value
	for _, row := range rows {
		ctx := &evalContext{table: table, row: row}
		var key strings.Builder
		for _, expr := range exprs {
			v, err := evalExpr(expr, ctx)
			if err != nil {
				return nil, err
			}
			k := v.Key()
			fmt.Fprintf(&key, "%d:%s", len(k), k)
		}
		keyStr := key.String()
		idx, ok := index[keyStr]
		if !ok {
			idx = len(groups)
			index[keyStr] = idx
			groups = append(groups, nil)
		}
		groups[idx] = append(groups[idx], row)
	}
	return groups, nil
}

func aliasIndex(ref *ColumnRef, columns []string) (int, bool) {
	for i, name := range columns {
		if strings.EqualFold(name, ref.Name) {
			return i, true
		}
	}
	return 0, false
}

func orderValue(item OrderItem, ctx *evalContext, out []Value, columns []string) (Value, error) {
	if ref, ok := item.Expr.(*ColumnRef); ok && ref.Table == "" {
		if idx, ok := aliasIndex(ref, columns); ok {
			return out[idx], nil
		}
	}
	if n, ok := parseOrdinal(item.Expr); ok {
		if n >= 1 && n <= len(out) {
			return out[n-1], nil
		}
		return Value{}, fmt.Errorf("sql: ORDER BY position %d is out of range", n)
	}
	return evalExpr(item.Expr, ctx)
}

func evalPredicate(e Expr, ctx *evalContext) (bool, error) {
	v, err := evalExpr(e, ctx)
	if err != nil {
		return false, err
	}
	truth, ok := Truthy(v)
	if !ok {
		return false, nil
	}
	return truth, nil
}

func checkUnique(tx *db.Tx, table *Table, row []Value, skipKey []byte) error {
	for i, col := range table.Columns {
		if !col.Unique || i == table.PK || row[i].Null {
			continue
		}
		var conflict error
		prefix := tableRowPrefix(table.Name)
		if err := tx.Scan(prefix, prefixEnd(prefix), func(key, value []byte) bool {
			if skipKey != nil && bytes.Equal(key, skipKey) {
				return true
			}
			other, err := decodeRow(value, table)
			if err != nil {
				conflict = err
				return false
			}
			if other[i].Null {
				return true
			}
			cmp, err := CompareValues(row[i], other[i])
			if err != nil {
				conflict = err
				return false
			}
			if cmp == 0 {
				conflict = fmt.Errorf("%w: UNIQUE constraint failed on %s.%s", ErrConstraint, table.Name, col.Name)
				return false
			}
			return true
		}); err != nil {
			return err
		}
		if conflict != nil {
			return conflict
		}
	}
	return nil
}

func rowKeyForValues(values []Value) string {
	var b strings.Builder
	for _, v := range values {
		k := v.Key()
		fmt.Fprintf(&b, "%d:%s", len(k), k)
	}
	return b.String()
}
