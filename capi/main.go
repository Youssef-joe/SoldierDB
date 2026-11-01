// Package main builds the YousDB C shared library.
//
// The ABI uses integer handles instead of Go pointers so that C callers never
// hold memory owned by the Go runtime. Every string returned by the API is
// allocated with malloc and must be released with yousdb_free_string.
package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef uint64_t yousdb_handle;
typedef uint64_t yousdb_result;
*/
import "C"

import (
	"sync"
	"unsafe"

	"github.com/Youssef-joe/YousDB/engine/sql"
)

func main() {}

var (
	mu         sync.Mutex
	nextHandle uint64 = 1
	engines           = make(map[uint64]*dbHandle)
	results           = make(map[uint64]*sql.Result)
)

type dbHandle struct {
	mu     sync.Mutex
	engine *sql.Engine
}

func registerEngine(e *sql.Engine) C.uint64_t {
	mu.Lock()
	defer mu.Unlock()
	h := nextHandle
	nextHandle++
	engines[h] = &dbHandle{engine: e}
	return C.uint64_t(h)
}

func lookupEngine(h C.uint64_t) *dbHandle {
	mu.Lock()
	defer mu.Unlock()
	return engines[uint64(h)]
}

func unregisterEngine(h C.uint64_t) *dbHandle {
	mu.Lock()
	defer mu.Unlock()
	e := engines[uint64(h)]
	delete(engines, uint64(h))
	return e
}

func registerResult(r *sql.Result) C.uint64_t {
	mu.Lock()
	defer mu.Unlock()
	h := nextHandle
	nextHandle++
	results[h] = r
	return C.uint64_t(h)
}

func lookupResult(h C.uint64_t) *sql.Result {
	mu.Lock()
	defer mu.Unlock()
	return results[uint64(h)]
}

func setError(out **C.char, msg string) {
	if out != nil {
		*out = C.CString(msg)
	}
}

func cString(s string) *C.char {
	return C.CString(s)
}

// yousdb_open opens or creates a database file. It returns 0 on failure and
// stores a malloc'd error message in err if err is not NULL.
//
//export yousdb_open
func yousdb_open(path *C.char, errOut **C.char) C.uint64_t {
	if path == nil {
		setError(errOut, "yousdb_open: path is NULL")
		return 0
	}
	engine, err := sql.Open(C.GoString(path))
	if err != nil {
		setError(errOut, err.Error())
		return 0
	}
	return registerEngine(engine)
}

// yousdb_close closes a database handle and releases its resources.
//
//export yousdb_close
func yousdb_close(h C.uint64_t) {
	if entry := unregisterEngine(h); entry != nil {
		entry.mu.Lock()
		_ = entry.engine.Close()
		entry.mu.Unlock()
	}
}

// yousdb_exec executes one or more statements. On success it stores a result
// handle in out and returns 1. On failure it returns 0 and stores a malloc'd
// error message in err. The result handle must be freed with
// yousdb_result_free.
//
//export yousdb_exec
func yousdb_exec(h C.uint64_t, query *C.char, out *C.uint64_t, errOut **C.char) C.int {
	if query == nil {
		setError(errOut, "yousdb_exec: query is NULL")
		return 0
	}
	if out == nil {
		setError(errOut, "yousdb_exec: result out-parameter is NULL")
		return 0
	}
	engine := lookupEngine(h)
	if engine == nil {
		setError(errOut, "yousdb_exec: invalid database handle")
		return 0
	}
	engine.mu.Lock()
	res, err := engine.engine.Exec(C.GoString(query))
	engine.mu.Unlock()
	if err != nil {
		setError(errOut, err.Error())
		return 0
	}
	*out = registerResult(res)
	return 1
}

// yousdb_result_free releases a result handle.
//
//export yousdb_result_free
func yousdb_result_free(h C.uint64_t) {
	mu.Lock()
	defer mu.Unlock()
	delete(results, uint64(h))
}

// yousdb_result_column_count returns the number of columns, or 0.
//
//export yousdb_result_column_count
func yousdb_result_column_count(h C.uint64_t) C.int {
	res := lookupResult(h)
	if res == nil {
		return 0
	}
	return C.int(len(res.Columns))
}

// yousdb_result_column_name returns a malloc'd column name.
//
//export yousdb_result_column_name
func yousdb_result_column_name(h C.uint64_t, index C.int) *C.char {
	res := lookupResult(h)
	if res == nil || index < 0 || int(index) >= len(res.Columns) {
		return nil
	}
	return cString(res.Columns[index])
}

// yousdb_result_row_count returns the number of rows.
//
//export yousdb_result_row_count
func yousdb_result_row_count(h C.uint64_t) C.int {
	res := lookupResult(h)
	if res == nil {
		return 0
	}
	return C.int(len(res.Rows))
}

// yousdb_result_rows_affected returns the number of modified rows.
//
//export yousdb_result_rows_affected
func yousdb_result_rows_affected(h C.uint64_t) C.int {
	res := lookupResult(h)
	if res == nil {
		return 0
	}
	return C.int(res.RowsAffected)
}

// yousdb_result_message returns a malloc'd status message.
//
//export yousdb_result_message
func yousdb_result_message(h C.uint64_t) *C.char {
	res := lookupResult(h)
	if res == nil {
		return nil
	}
	return cString(res.Message)
}

// yousdb_value_type returns 0 for NULL, 1 INTEGER, 2 REAL, 3 TEXT, 4 BOOLEAN,
// or -1 for an invalid handle or position.
//
//export yousdb_value_type
func yousdb_value_type(h C.uint64_t, row C.int, col C.int) C.int {
	v, ok := valueAt(h, row, col)
	if !ok {
		return -1
	}
	if v.Null {
		return 0
	}
	switch v.Type {
	case sql.IntegerType:
		return 1
	case sql.RealType:
		return 2
	case sql.TextType:
		return 3
	case sql.BooleanType:
		return 4
	default:
		return -1
	}
}

// yousdb_value_string returns a malloc'd display string for a value.
//
//export yousdb_value_string
func yousdb_value_string(h C.uint64_t, row C.int, col C.int) *C.char {
	v, ok := valueAt(h, row, col)
	if !ok {
		return nil
	}
	return cString(v.String())
}

// yousdb_value_int returns an INTEGER value, or 0 for NULL/other types.
//
//export yousdb_value_int
func yousdb_value_int(h C.uint64_t, row C.int, col C.int) C.int64_t {
	v, ok := valueAt(h, row, col)
	if !ok || v.Null || v.Type != sql.IntegerType {
		return 0
	}
	return C.int64_t(v.Int)
}

// yousdb_value_real returns a REAL value, or 0 for NULL/other types.
//
//export yousdb_value_real
func yousdb_value_real(h C.uint64_t, row C.int, col C.int) C.double {
	v, ok := valueAt(h, row, col)
	if !ok || v.Null || v.Type != sql.RealType {
		return 0
	}
	return C.double(v.Real)
}

// yousdb_value_bool returns a BOOLEAN value, or 0 for NULL/other types.
//
//export yousdb_value_bool
func yousdb_value_bool(h C.uint64_t, row C.int, col C.int) C.int {
	v, ok := valueAt(h, row, col)
	if !ok || v.Null || v.Type != sql.BooleanType {
		return 0
	}
	if v.Bool {
		return 1
	}
	return 0
}

// yousdb_value_text returns a malloc'd TEXT value, or an empty string for
// NULL/other types.
//
//export yousdb_value_text
func yousdb_value_text(h C.uint64_t, row C.int, col C.int) *C.char {
	v, ok := valueAt(h, row, col)
	if !ok || v.Null || v.Type != sql.TextType {
		return cString("")
	}
	return cString(v.Str)
}

// yousdb_free_string releases a string returned by this API.
//
//export yousdb_free_string
func yousdb_free_string(s *C.char) {
	if s != nil {
		C.free(unsafe.Pointer(s))
	}
}

func valueAt(h C.uint64_t, row, col C.int) (sql.Value, bool) {
	res := lookupResult(h)
	if res == nil || row < 0 || int(row) >= len(res.Rows) {
		return sql.Value{}, false
	}
	values := res.Rows[row]
	if col < 0 || int(col) >= len(values) {
		return sql.Value{}, false
	}
	return values[col], true
}
