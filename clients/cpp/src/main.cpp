#include "yousdb.hpp"

#include <cstdlib>
#include <iostream>

int main(int argc, char** argv) {
    std::string path = "yousdb.db";
    std::string sql;
    for (int i = 1; i < argc; ++i) {
        std::string arg = argv[i];
        if (arg == "-h" || arg == "--help") {
            std::cout << "Usage: yousdb-cpp [-db PATH] [-c SQL] [SQL]\n";
            return 0;
        }
        if ((arg == "-db" || arg == "--db" || arg == "-c" || arg == "--command") && i + 1 < argc) {
            if (arg == "-db" || arg == "--db") path = argv[++i];
            else sql = argv[++i];
            continue;
        }
        if (!arg.empty() && arg[0] == '-') {
            std::cerr << "unknown option: " << arg << "\n";
            return 2;
        }
        if (!sql.empty()) sql += ' ';
        sql += arg;
    }

    try {
        auto db = yousdb::Database::open(path);
        if (sql.empty()) {
            std::cout << "Provide SQL with -c or as the final argument.\n";
            return 0;
        }
        auto result = db.exec(sql);
        if (result.columns().empty()) {
            if (!result.message().empty()) std::cout << result.message() << '\n';
            return 0;
        }
        for (std::size_t i = 0; i < result.columns().size(); ++i) {
            if (i) std::cout << "\t";
            std::cout << result.columns()[i];
        }
        std::cout << '\n';
        for (const auto& row : result.rows()) {
            for (std::size_t i = 0; i < row.size(); ++i) {
                if (i) std::cout << "\t";
                std::cout << row[i].to_string();
            }
            std::cout << '\n';
        }
        return 0;
    } catch (const yousdb::Error& error) {
        std::cerr << "error: " << error.what() << '\n';
        return 1;
    }
}
