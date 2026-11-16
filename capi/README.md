# SoldierDB C ABI

`capi` builds a native shared library around the Go SQL engine.

```sh
make shared
# macOS: build/libsoldierdb.dylib + build/libsoldierdb.h
# Linux: build/libsoldierdb.so   + build/libsoldierdb.h
```

The generated header is produced by cgo. The smoke test in `capi/test/smoke.c`
is built and run by `make ctest`.

## Handles

`soldierdb_open` returns a `uint64_t` database handle, or `0` on failure. Results
are also referenced by `uint64_t` handles. The ABI never passes Go pointers to
C, so handles stay valid across the boundary and do not depend on Go's garbage
collector.

```c
char *err = NULL;
uint64_t db = soldierdb_open("app.db", &err);
if (db == 0) {
    fprintf(stderr, "%s\n", err);
    soldierdb_free_string(err);
}
```

`soldierdb_exec` executes one or more statements:

```c
uint64_t res = 0;
if (!soldierdb_exec(db, "SELECT id, name FROM users;", &res, &err)) {
    /* err is a malloc'd message */
}
```

## Results

```c
int columns = soldierdb_result_column_count(res);
int rows = soldierdb_result_row_count(res);
int affected = soldierdb_result_rows_affected(res);
char *message = soldierdb_result_message(res);
```

Values are available as formatted strings or typed accessors:

```c
soldierdb_value_type(res, row, col);   /* 0 NULL, 1 INTEGER, 2 REAL, 3 TEXT, 4 BOOLEAN, 5 VECTOR */
soldierdb_value_int(res, row, col);
soldierdb_value_real(res, row, col);
soldierdb_value_text(res, row, col);
soldierdb_value_bool(res, row, col);
soldierdb_value_string(res, row, col);
soldierdb_value_vector_dimension(res, row, col);
soldierdb_value_vector_element(res, row, col, index);
```

## Ownership

Every `char *` returned by the API is allocated with `malloc`. Release it with
`soldierdb_free_string`. Result handles must be released with
`soldierdb_result_free`. Database handles are released with `soldierdb_close`, which
rolls back any open transaction.

Calls on the same database handle are serialized by the library. Separate
handles may be used from separate threads.
