#pragma once

#include <cstdint>
#include <stdexcept>
#include <string>
#include <utility>
#include <vector>

extern "C" {
#include "libyousdb.h"
}

namespace yousdb {

enum class ValueType { Null = 0, Integer = 1, Real = 2, Text = 3, Boolean = 4 };

class Error : public std::runtime_error {
public:
    explicit Error(std::string message) : std::runtime_error(message), message_(std::move(message)) {}

    const std::string& message() const noexcept { return message_; }

private:
    std::string message_;
};

class Value {
public:
    Value() = default;
    explicit Value(std::int64_t value) : type_(ValueType::Integer), integer_(value) {}
    explicit Value(double value) : type_(ValueType::Real), real_(value) {}
    explicit Value(std::string value) : type_(ValueType::Text), text_(std::move(value)) {}
    explicit Value(bool value) : type_(ValueType::Boolean), boolean_(value) {}

    ValueType type() const noexcept { return type_; }
    bool is_null() const noexcept { return type_ == ValueType::Null; }
    std::int64_t as_i64() const;
    double as_f64() const;
    bool as_bool() const;
    const std::string& as_text() const;
    std::string to_string() const;

private:
    ValueType type_ = ValueType::Null;
    std::int64_t integer_ = 0;
    double real_ = 0.0;
    bool boolean_ = false;
    std::string text_;
};

class Result {
public:
    const std::vector<std::string>& columns() const noexcept { return columns_; }
    const std::vector<std::vector<Value>>& rows() const noexcept { return rows_; }
    std::size_t rows_affected() const noexcept { return rows_affected_; }
    const std::string& message() const noexcept { return message_; }
    bool empty() const noexcept { return rows_.empty(); }
    const Value* get(std::size_t row, std::size_t column) const noexcept;
    std::size_t column_index(const std::string& name) const noexcept;
    const Value* value(std::size_t row, const std::string& column) const noexcept;

private:
    friend class Database;
    std::vector<std::string> columns_;
    std::vector<std::vector<Value>> rows_;
    std::size_t rows_affected_ = 0;
    std::string message_;
};

class Database {
public:
    static Database open(const std::string& path);

    Database() = default;
    explicit Database(std::uint64_t handle) : handle_(handle) {}
    ~Database();

    Database(const Database&) = delete;
    Database& operator=(const Database&) = delete;
    Database(Database&& other) noexcept : handle_(std::exchange(other.handle_, 0)) {}
    Database& operator=(Database&& other) noexcept;

    Result exec(const std::string& sql) const;
    explicit operator bool() const noexcept { return handle_ != 0; }

private:
    std::uint64_t handle_ = 0;
};

} // namespace yousdb
