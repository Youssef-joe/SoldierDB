#include "yousdb.hpp"

#include <cstdlib>
#include <chrono>
#include <iostream>
#include <sstream>
#include <thread>

#if defined(_WIN32)
#include <io.h>
#define YOUSDB_ISATTY _isatty(_fileno(stdin))
#else
#include <unistd.h>
#define YOUSDB_ISATTY isatty(fileno(stdin))
#endif

namespace {

void print_help() {
    std::cout << "YousDB shell commands:\n"
              << "  \\q, \\quit        quit the shell\n"
              << "  \\h, \\help        show this help\n"
              << "  \\dt, \\tables      list tables\n"
              << "  \\d TABLE          describe a table\n"
              << "  \\vector           show a vector search example\n"
              << "  \\clear, \\cls      clear the terminal\n\n"
              << "SQL statements end with a semicolon.\n";
}

void show_banner() {
    for (const char* frame : {"·", "··", "···"}) {
        std::cout << "\r\033[36mStarting YousDB " << frame << "\033[0m" << std::flush;
        std::this_thread::sleep_for(std::chrono::milliseconds(70));
    }
    std::cout << "\r\033[2K"
              << "\033[1;36m __   __                         ____  ____  \033[0m\n"
              << "\033[1;36m \\ \\ / /__  _   _ ___  ___   |  _ \\| __ ) \033[0m\n"
              << "\033[1;36m  \\ V / _ \\| | | / __|/ _ \\  | | | |  _ \\ \033[0m\n"
              << "\033[1;36m   | | (_) | |_| \\__ \\  __/  | |_| | |_) |\033[0m\n"
              << "\033[1;36m   |_|\\___/ \\__,_|___/\\___|  |____/|____/ \033[0m\n"
              << "YousDB C++ shell · type \033[33m\\help\033[0m for shortcuts, \033[33m\\q\033[0m to quit.\n";
}

void print_result(const yousdb::Result& result) {
    if (result.columns().empty()) {
        if (!result.message().empty()) std::cout << result.message() << '\n';
        return;
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
    std::cout << "(" << result.rows().size() << (result.rows().size() == 1 ? " row)\n" : " rows)\n");
}

bool complete_statement(const std::string& sql) {
    bool single = false;
    bool doubled = false;
    for (std::size_t i = 0; i < sql.size(); ++i) {
        const char c = sql[i];
        if (single) {
            if (c == '\'' && i + 1 < sql.size() && sql[i + 1] == '\'') ++i;
            else if (c == '\'') single = false;
        } else if (doubled) {
            if (c == '"' && i + 1 < sql.size() && sql[i + 1] == '"') ++i;
            else if (c == '"') doubled = false;
        } else if (c == '\'') {
            single = true;
        } else if (c == '"') {
            doubled = true;
        } else if (c == ';') {
            return true;
        }
    }
    return false;
}

// Returns true when handled, and sets quit when the command requests exit.
bool meta_command(yousdb::Database& db, const std::string& input, bool& quit) {
    std::istringstream stream(input);
    std::string command;
    stream >> command;
    if (command.empty()) return true;
    if (command == "\\q" || command == "\\quit" || command == "exit" || command == "quit") {
        quit = true;
        return true;
    }
    if (command == "\\h" || command == "\\help" || command == "help") {
        print_help();
        return true;
    }
    if (command == "\\dt" || command == "\\tables") {
        print_result(db.exec("SHOW TABLES;"));
        return true;
    }
    if (command == "\\d" || command == "\\describe") {
        std::string table;
        stream >> table;
        if (table.empty()) std::cerr << "usage: \\d TABLE\n";
        else print_result(db.exec("DESCRIBE " + table + ";"));
        return true;
    }
    if (command == "\\vector") {
        std::cout << "Vector search example:\n  CREATE TABLE docs (id INTEGER PRIMARY KEY, embedding VECTOR(3));\n  SELECT id FROM docs ORDER BY cosine_distance(embedding, VECTOR(1, 0, 0)) LIMIT 10;\n";
        return true;
    }
    if (command == "\\clear" || command == "\\cls") {
        std::cout << "\033[H\033[2J" << std::flush;
        return true;
    }
    return false;
}

int repl(yousdb::Database& db) {
    const bool interactive = YOUSDB_ISATTY != 0;
    if (interactive) show_banner();
    std::string buffer;
    std::string line;
    while (true) {
        if (interactive) std::cout << (buffer.empty() ? "yousdb> " : "   ...> ") << std::flush;
        if (!std::getline(std::cin, line)) break;
        if (buffer.empty()) {
            bool quit = false;
            if (meta_command(db, line, quit)) {
                if (quit) return 0;
                continue;
            }
        }
        buffer += line + '\n';
        if (!complete_statement(buffer)) continue;
        try {
            print_result(db.exec(buffer));
        } catch (const yousdb::Error& error) {
            std::cerr << "error: " << error.what() << '\n';
        }
        buffer.clear();
    }
    if (!buffer.empty()) {
        try { print_result(db.exec(buffer)); }
        catch (const yousdb::Error& error) { std::cerr << "error: " << error.what() << '\n'; }
    }
    return 0;
}

} // namespace

int main(int argc, char** argv) {
    std::string path = "yousdb.db";
    std::string sql;
    for (int i = 1; i < argc; ++i) {
        std::string arg = argv[i];
        if (arg == "-h" || arg == "--help") {
            std::cout << "Usage: yousdb-cpp [-db PATH] [-c SQL] [SQL]\n"
                      << "With no SQL argument, starts the interactive shell.\n";
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
        if (sql.empty()) return repl(db);
        print_result(db.exec(sql));
        return 0;
    } catch (const yousdb::Error& error) {
        std::cerr << "error: " << error.what() << '\n';
        return 1;
    }
}
