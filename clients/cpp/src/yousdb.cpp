#include "yousdb.hpp"

#include <algorithm>
#include <cctype>
#include <cstring>
#include <sstream>

namespace yousdb {
namespace {

std::string take_string(char* value) {
    if (value == nullptr) return {};
    std::string result(value);
    yousdb_free_string(value);
    return result;
}

struct ResultGuard {
    std::uint64_t handle;
    explicit ResultGuard(std::uint64_t value) : handle(value) {}
    ~ResultGuard() {
        if (handle != 0) yousdb_result_free(handle);
    }
    ResultGuard(const ResultGuard&) = delete;
    ResultGuard& operator=(const ResultGuard&) = delete;
};

[[noreturn]] void throw_error(char* error) {
    std::string message = take_string(error);
    throw Error(message.empty() ? "unknown database error" : std::move(message));
}

bool equal_fold(const std::string& left, const std::string& right) {
    return left.size() == right.size() && std::equal(left.begin(), left.end(), right.begin(),
        [](unsigned char a, unsigned char b) { return std::tolower(a) == std::tolower(b); });
}

Value read_value(std::uint64_t result, int row, int column) {
    switch (yousdb_value_type(result, row, column)) {
    case 1: return Value(yousdb_value_int(result, row, column));
    case 2: return Value(yousdb_value_real(result, row, column));
    case 3: return Value(take_string(yousdb_value_text(result, row, column)));
    case 4: return Value(yousdb_value_bool(result, row, column) != 0);
    case 5: {
        const auto dimension = yousdb_value_vector_dimension(result, row, column);
        std::vector<float> vector;
        for (int i = 0; i < dimension; ++i) {
            vector.push_back(yousdb_value_vector_element(result, row, column, i));
        }
        return Value(std::move(vector));
    }
    default: return Value();
    }
}

} // namespace

std::int64_t Value::as_i64() const {
    if (type_ != ValueType::Integer) throw Error("value is not an INTEGER");
    return integer_;
}

double Value::as_f64() const {
    if (type_ == ValueType::Integer) return static_cast<double>(integer_);
    if (type_ != ValueType::Real) throw Error("value is not numeric");
    return real_;
}

bool Value::as_bool() const {
    if (type_ != ValueType::Boolean) throw Error("value is not a BOOLEAN");
    return boolean_;
}

const std::string& Value::as_text() const {
    if (type_ != ValueType::Text) throw Error("value is not TEXT");
    return text_;
}

const std::vector<float>& Value::as_vector() const {
    if (type_ != ValueType::Vector) throw Error("value is not a VECTOR");
    return vector_;
}

std::string Value::to_string() const {
    switch (type_) {
    case ValueType::Null: return "NULL";
    case ValueType::Integer: return std::to_string(integer_);
    case ValueType::Real: {
        std::ostringstream out;
        out << real_;
        return out.str();
    }
    case ValueType::Text: return text_;
    case ValueType::Boolean: return boolean_ ? "true" : "false";
    case ValueType::Vector: {
        std::ostringstream out;
        out << "[";
        for (std::size_t i = 0; i < vector_.size(); ++i) {
            if (i) out << ", ";
            out << vector_[i];
        }
        out << "]";
        return out.str();
    }
    }
    return {};
}

const Value* Result::get(std::size_t row, std::size_t column) const noexcept {
    if (row >= rows_.size() || column >= rows_[row].size()) return nullptr;
    return &rows_[row][column];
}

std::size_t Result::column_index(const std::string& name) const noexcept {
    for (std::size_t i = 0; i < columns_.size(); ++i) {
        if (equal_fold(columns_[i], name)) return i;
    }
    return columns_.size();
}

const Value* Result::value(std::size_t row, const std::string& column) const noexcept {
    const auto index = column_index(column);
    return index == columns_.size() ? nullptr : get(row, index);
}

Database Database::open(const std::string& path) {
    char* error = nullptr;
    auto handle = yousdb_open(const_cast<char*>(path.c_str()), &error);
    if (handle == 0) throw_error(error);
    return Database(handle);
}

Database::~Database() {
    if (handle_ != 0) yousdb_close(handle_);
}

Database& Database::operator=(Database&& other) noexcept {
    if (this != &other) {
        if (handle_ != 0) yousdb_close(handle_);
        handle_ = std::exchange(other.handle_, 0);
    }
    return *this;
}

Result Database::exec(const std::string& sql) const {
    if (handle_ == 0) throw Error("database is closed");
    char* error = nullptr;
    std::uint64_t native_result = 0;
    if (!yousdb_exec(handle_, const_cast<char*>(sql.c_str()), &native_result, &error)) {
        throw_error(error);
    }
    ResultGuard guard{native_result};

    Result result;
    const auto columns = std::max(0, yousdb_result_column_count(native_result));
    const auto rows = std::max(0, yousdb_result_row_count(native_result));
    for (int column = 0; column < columns; ++column) {
        result.columns_.push_back(take_string(yousdb_result_column_name(native_result, column)));
    }
    for (int row = 0; row < rows; ++row) {
        std::vector<Value> values;
        for (int column = 0; column < columns; ++column) {
            values.push_back(read_value(native_result, row, column));
        }
        result.rows_.push_back(std::move(values));
    }
    result.rows_affected_ = static_cast<std::size_t>(std::max(0, yousdb_result_rows_affected(native_result)));
    result.message_ = take_string(yousdb_result_message(native_result));
    return result;
}

} // namespace yousdb
