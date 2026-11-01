#include "yousdb.hpp"

#include <cassert>
#include <cstdio>
#include <string>

int main(int argc, char** argv) {
    const std::string path = argc > 1 ? argv[1] : "yousdb-cpp-test.db";
    std::remove(path.c_str());

    {
        auto db = yousdb::Database::open(path);
        db.exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, active BOOLEAN)");
        auto insert = db.exec("INSERT INTO users VALUES (1, 'Ada', TRUE), (2, 'Grace', FALSE)");
        assert(insert.rows_affected() == 2);
        auto result = db.exec("SELECT id, name, active FROM users ORDER BY id");
        assert(result.columns().size() == 3);
        assert(result.rows().size() == 2);
        assert(result.value(0, "NAME")->as_text() == "Ada");
        assert(result.get(1, 0)->as_i64() == 2);
        assert(!result.get(1, 2)->as_bool());

        bool failed = false;
        try { db.exec("SELECT * FROM missing"); }
        catch (const yousdb::Error&) { failed = true; }
        assert(failed);
    }

    auto reopened = yousdb::Database::open(path);
    auto durable = reopened.exec("SELECT COUNT(*) FROM users");
    assert(durable.get(0, 0)->as_i64() == 2);
    std::remove(path.c_str());
}
