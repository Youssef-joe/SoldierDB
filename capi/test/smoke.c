#include <math.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "libyousdb.h"

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
        yousdb_free_string(s);
    }
}

static int exec_ok(uint64_t db, const char *sql, uint64_t *result) {
    char *err = NULL;
    int ok = yousdb_exec(db, (char *)sql, result, &err);
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
    uint64_t db = yousdb_open(argv[1], &err);
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
    yousdb_result_free(res);

    CHECK(exec_ok(db,
                  "INSERT INTO t (id, name, score) VALUES "
                  "(1, 'Ada', 9.5), (2, 'Grace', 8.25);",
                  &res),
          "insert rows");
    CHECK(yousdb_result_rows_affected(res) == 2, "insert affected two rows");
    {
        char *msg = yousdb_result_message(res);
        CHECK(msg != NULL && strstr(msg, "2 row") != NULL, "insert message");
        free_string(msg);
    }
    yousdb_result_free(res);

    CHECK(exec_ok(db, "SELECT id, name, score, active FROM t ORDER BY id;",
                  &res),
          "select rows");
    CHECK(yousdb_result_column_count(res) == 4, "column count");
    CHECK(yousdb_result_row_count(res) == 2, "row count");
    {
        char *name = yousdb_result_column_name(res, 1);
        CHECK(name != NULL && strcmp(name, "name") == 0, "column name");
        free_string(name);
    }

    CHECK(yousdb_value_type(res, 0, 0) == 1, "id is integer");
    CHECK(yousdb_value_int(res, 0, 0) == 1, "first id");
    CHECK(yousdb_value_int(res, 1, 0) == 2, "second id");

    {
        char *name = yousdb_value_text(res, 0, 1);
        CHECK(strcmp(name, "Ada") == 0, "first name");
        free_string(name);
        name = yousdb_value_text(res, 1, 1);
        CHECK(strcmp(name, "Grace") == 0, "second name");
        free_string(name);
    }

    CHECK(yousdb_value_type(res, 0, 2) == 2, "score is real");
    CHECK(fabs(yousdb_value_real(res, 0, 2) - 9.5) < 1e-9, "first score");
    CHECK(yousdb_value_type(res, 0, 3) == 4, "active is boolean");
    CHECK(yousdb_value_bool(res, 0, 3) == 1, "default boolean is true");

    {
        char *value = yousdb_value_string(res, 1, 2);
        CHECK(strcmp(value, "8.25") == 0, "formatted value");
        free_string(value);
    }
    yousdb_result_free(res);

    CHECK(exec_ok(db, "SELECT NULL, 7, 'x';", &res), "select constants");
    CHECK(yousdb_value_type(res, 0, 0) == 0, "NULL type");
    {
        char *value = yousdb_value_string(res, 0, 0);
        CHECK(strcmp(value, "NULL") == 0, "NULL display");
        free_string(value);
    }
    CHECK(yousdb_value_int(res, 0, 1) == 7, "constant integer");
    {
        char *value = yousdb_value_text(res, 0, 2);
        CHECK(strcmp(value, "x") == 0, "constant text");
        free_string(value);
    }
    yousdb_result_free(res);

    CHECK(exec_ok(db, "UPDATE t SET score = score + 0.5 WHERE id = 2;", &res),
          "update");
    CHECK(yousdb_result_rows_affected(res) == 1, "update affected one row");
    yousdb_result_free(res);

    CHECK(exec_ok(db, "SELECT COUNT(*) FROM t;", &res), "aggregate");
    CHECK(yousdb_value_int(res, 0, 0) == 2, "count value");
    yousdb_result_free(res);

    // Errors are reported through the out-parameter and do not crash.
    res = 0;
    CHECK(yousdb_exec(db, (char *)"SELECT * FROM missing;", &res, &err) == 0,
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
    CHECK(yousdb_value_int(res, 0, 0) == 3, "multi statement count");
    yousdb_result_free(res);

    yousdb_close(db);

    // Reopen and verify durability.
    db = yousdb_open(argv[1], &err);
    if (db == 0) {
        fprintf(stderr, "reopen failed: %s\n", err ? err : "(no error)");
        free_string(err);
        return 1;
    }
    CHECK(exec_ok(db, "SELECT COUNT(*) FROM t;", &res), "count after reopen");
    CHECK(yousdb_value_int(res, 0, 0) == 3, "durable row count");
    yousdb_result_free(res);
    yousdb_close(db);

    if (failures == 0) {
        printf("capi smoke: ok\n");
        return 0;
    }
    fprintf(stderr, "capi smoke: %d failure(s)\n", failures);
    return 1;
}
