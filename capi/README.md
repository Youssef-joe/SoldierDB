# YousDB C ABI

`capi` builds a native shared library around the Go SQL engine.

```sh
make shared
# macOS: build/libyousdb.dylib + build/libyousdb.h
# Linux: build/libyousdb.so   + build/libyousdb.h
```

The generated header is produced by cgo. The smoke test in `capi/test/smoke.c`
is built and run by `make ctest`.

## Handles

`yousdb_open` returns a `uint64_t` database handle, or `0` on failure. Results
are also referenced by `uint64_t` handles. The ABI never passes Go pointers to
C, so handles stay valid across the boundary and do not depend on Go's garbage
collector.

```c
char *err = NULL;
uint64_t db = yousdb_open("app.db", &err);
if (db == 0) {
    fprintf(stderr, "%s\n", err);
    yousdb_free_string(err);
}
```

`yousdb_exec` executes one or more statements:

```c
uint64_t res = 0;
if (!yousdb_exec(db, "SELECT id, name FROM users;", &res, &err)) {
    /* err is a malloc'd message */
}
```

## Results

```c
int columns = yousdb_result_column_count(res);
int rows = yousdb_result_row_count(res);
int affected = yousdb_result_rows_affected(res);
char *message = yousdb_result_message(res);
```

Values are available as formatted strings or typed accessors:

```c
yousdb_value_type(res, row, col);   /* 0 NULL, 1 INTEGER, 2 REAL, 3 TEXT, 4 BOOLEAN */
yousdb_value_int(res, row, col);
yousdb_value_real(res, row, col);
yousdb_value_text(res, row, col);
yousdb_value_bool(res, row, col);
yousdb_value_string(res, row, col);
```

## Ownership

Every `char *` returned by the API is allocated with `malloc`. Release it with
`yousdb_free_string`. Result handles must be released with
`yousdb_result_free`. Database handles are released with `yousdb_close`, which
rolls back any open transaction.

Calls on the same database handle are serialized by the library. Separate
handles may be used from separate threads.
