#include <math.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "libsoldierdb.h"

static int failures = 0;

#define CHECK(cond, msg)                                                       \
    do {                                                                       \
        if (!(cond)) {                                                         \
            fprintf(stderr, "FAIL: %s (%s:%d)\n", msg, __FILE__, __LINE__);    \
            failures++;                                                        \
        }                                                                      \
    } while (0)

static void free_string(char *s) {
    if (s != NULL) {
        soldierdb_free_string(s);
    }
}

static int exec_ok(uint64_t db, const char *sql, uint64_t *result) {
    char *err = NULL;
    int ok = soldierdb_exec(db, (char *)sql, result, &err);
    if (!ok) {
        fprintf(stderr, "exec failed: %s\n  SQL: %s\n", err ? err : "(no error)",
                sql);
        free_string(err);
    }
    return ok;
}

int main(int argc, char **argv) {
    if (argc < 2) {
        fprintf(stderr, "usage: %s <database path>\n", argv[0]);
        return 2;
    }
    remove(argv[1]);

    char *err = NULL;
    uint64_t db = soldierdb_open(argv[1], &err);
    if (db == 0) {
        fprintf(stderr, "open failed: %s\n", err ? err : "(no error)");
        free_string(err);
        return 1;
    }

    uint64_t res = 0;
    CHECK(exec_ok(db,
                  "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT NOT NULL, "
                  "score REAL, active BOOLEAN DEFAULT TRUE);",
                  &res),
          "create table");
    soldierdb_result_free(res);

    CHECK(exec_ok(db,
                  "INSERT INTO t (id, name, score) VALUES "
                  "(1, 'Ada', 9.5), (2, 'Grace', 8.25);",
                  &res),
          "insert rows");
    CHECK(soldierdb_result_rows_affected(res) == 2, "insert affected two rows");
    {
        char *msg = soldierdb_result_message(res);
        CHECK(msg != NULL && strstr(msg, "2 row") != NULL, "insert message");
        free_string(msg);
    }
    soldierdb_result_free(res);

    CHECK(exec_ok(db, "SELECT id, name, score, active FROM t ORDER BY id;",
                  &res),
          "select rows");
    CHECK(soldierdb_result_column_count(res) == 4, "column count");
    CHECK(soldierdb_result_row_count(res) == 2, "row count");
    {
        char *name = soldierdb_result_column_name(res, 1);
        CHECK(name != NULL && strcmp(name, "name") == 0, "column name");
        free_string(name);
    }

    CHECK(soldierdb_value_type(res, 0, 0) == 1, "id is integer");
    CHECK(soldierdb_value_int(res, 0, 0) == 1, "first id");
    CHECK(soldierdb_value_int(res, 1, 0) == 2, "second id");

    {
        char *name = soldierdb_value_text(res, 0, 1);
        CHECK(strcmp(name, "Ada") == 0, "first name");
        free_string(name);
        name = soldierdb_value_text(res, 1, 1);
        CHECK(strcmp(name, "Grace") == 0, "second name");
        free_string(name);
    }

    CHECK(soldierdb_value_type(res, 0, 2) == 2, "score is real");
    CHECK(fabs(soldierdb_value_real(res, 0, 2) - 9.5) < 1e-9, "first score");
    CHECK(soldierdb_value_type(res, 0, 3) == 4, "active is boolean");
    CHECK(soldierdb_value_bool(res, 0, 3) == 1, "default boolean is true");

    {
        char *value = soldierdb_value_string(res, 1, 2);
        CHECK(strcmp(value, "8.25") == 0, "formatted value");
        free_string(value);
    }
    soldierdb_result_free(res);

    CHECK(exec_ok(db, "SELECT NULL, 7, 'x';", &res), "select constants");
    CHECK(soldierdb_value_type(res, 0, 0) == 0, "NULL type");
    {
        char *value = soldierdb_value_string(res, 0, 0);
        CHECK(strcmp(value, "NULL") == 0, "NULL display");
        free_string(value);
    }
    CHECK(soldierdb_value_int(res, 0, 1) == 7, "constant integer");
    {
        char *value = soldierdb_value_text(res, 0, 2);
        CHECK(strcmp(value, "x") == 0, "constant text");
        free_string(value);
    }
    soldierdb_result_free(res);

    CHECK(exec_ok(db, "UPDATE t SET score = score + 0.5 WHERE id = 2;", &res),
          "update");
    CHECK(soldierdb_result_rows_affected(res) == 1, "update affected one row");
    soldierdb_result_free(res);

    CHECK(exec_ok(db, "SELECT COUNT(*) FROM t;", &res), "aggregate");
    CHECK(soldierdb_value_int(res, 0, 0) == 2, "count value");
    soldierdb_result_free(res);

    // Errors are reported through the out-parameter and do not crash.
    res = 0;
    CHECK(soldierdb_exec(db, (char *)"SELECT * FROM missing;", &res, &err) == 0,
          "missing table fails");
    CHECK(err != NULL, "error message is set");
    free_string(err);
    err = NULL;
    CHECK(res == 0, "failed exec does not return a result");

    // Multiple statements in one call.
    CHECK(exec_ok(db,
                  "INSERT INTO t (id, name, score) VALUES (3, 'Linus', 7.0);"
                  "SELECT COUNT(*) FROM t;",
                  &res),
          "multi statement");
    CHECK(soldierdb_value_int(res, 0, 0) == 3, "multi statement count");
    soldierdb_result_free(res);

    soldierdb_close(db);

    // Reopen and verify durability.
    db = soldierdb_open(argv[1], &err);
    if (db == 0) {
        fprintf(stderr, "reopen failed: %s\n", err ? err : "(no error)");
        free_string(err);
        return 1;
    }
    CHECK(exec_ok(db, "SELECT COUNT(*) FROM t;", &res), "count after reopen");
    CHECK(soldierdb_value_int(res, 0, 0) == 3, "durable row count");
    soldierdb_result_free(res);
    soldierdb_close(db);

    if (failures == 0) {
        printf("capi smoke: ok\n");
        return 0;
    }
    fprintf(stderr, "capi smoke: %d failure(s)\n", failures);
    return 1;
}
