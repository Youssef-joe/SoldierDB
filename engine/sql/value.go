package sql

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// DataType is a SQL column type.
type DataType int

const (
	// IntegerType is a 64-bit signed integer.
	IntegerType DataType = iota + 1
	// RealType is a 64-bit floating point number.
	RealType
	// TextType is a UTF-8 string.
	TextType
	// BooleanType is true or false.
	BooleanType
	// VectorType is a fixed-dimension vector of float32 values.
	VectorType
)

func (t DataType) String() string {
	switch t {
	case IntegerType:
		return "INTEGER"
	case RealType:
		return "REAL"
	case TextType:
		return "TEXT"
	case BooleanType:
		return "BOOLEAN"
	case VectorType:
		return "VECTOR"
	default:
		return "UNKNOWN"
	}
}

// ParseDataType maps a SQL type name to a DataType.
func ParseDataType(name string) (DataType, bool) {
	switch strings.ToUpper(name) {
	case "INT", "INTEGER", "BIGINT", "SMALLINT":
		return IntegerType, true
	case "REAL", "DOUBLE", "FLOAT":
		return RealType, true
	case "TEXT", "VARCHAR", "CHAR", "STRING":
		return TextType, true
	case "BOOL", "BOOLEAN":
		return BooleanType, true
	case "VECTOR":
		return VectorType, true
	default:
		return 0, false
	}
}

// Value is a typed SQL value. A zero Value is NULL.
type Value struct {
	Type   DataType
	Null   bool
	Int    int64
	Real   float64
	Str    string
	Bool   bool
	Vector []float32
}

// NullValue returns a NULL value.
func NullValue() Value { return Value{Null: true} }

// IntValue returns an INTEGER value.
func IntValue(v int64) Value { return Value{Type: IntegerType, Int: v} }

// RealValue returns a REAL value.
func RealValue(v float64) Value { return Value{Type: RealType, Real: v} }

// TextValue returns a TEXT value.
func TextValue(v string) Value { return Value{Type: TextType, Str: v} }

// BoolValue returns a BOOLEAN value.
func BoolValue(v bool) Value { return Value{Type: BooleanType, Bool: v} }

// VectorValue returns a VECTOR value. The input is copied.
func VectorValue(v []float32) Value {
	return Value{Type: VectorType, Vector: append([]float32(nil), v...)}
}

// IsNull reports whether the value is NULL.
func (v Value) IsNull() bool { return v.Null }

// String formats the value for display.
func (v Value) String() string {
	if v.Null {
		return "NULL"
	}
	switch v.Type {
	case IntegerType:
		return strconv.FormatInt(v.Int, 10)
	case RealType:
		return strconv.FormatFloat(v.Real, 'g', -1, 64)
	case TextType:
		return v.Str
	case BooleanType:
		if v.Bool {
			return "true"
		}
		return "false"
	case VectorType:
		parts := make([]string, len(v.Vector))
		for i, component := range v.Vector {
			parts[i] = strconv.FormatFloat(float64(component), 'g', -1, 32)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return "NULL"
	}
}

// Literal formats the value as a SQL literal.
func (v Value) Literal() string {
	if v.Null {
		return "NULL"
	}
	if v.Type == TextType {
		return "'" + strings.ReplaceAll(v.Str, "'", "''") + "'"
	}
	if v.Type == VectorType {
		parts := make([]string, len(v.Vector))
		for i, component := range v.Vector {
			parts[i] = strconv.FormatFloat(float64(component), 'g', -1, 32)
		}
		return "VECTOR(" + strings.Join(parts, ", ") + ")"
	}
	return v.String()
}

// Key returns a stable key for grouping and DISTINCT.
func (v Value) Key() string {
	if v.Null {
		return "n:"
	}
	switch v.Type {
	case IntegerType:
		return "i:" + strconv.FormatInt(v.Int, 10)
	case RealType:
		return "r:" + strconv.FormatUint(math.Float64bits(v.Real), 16)
	case TextType:
		return "s:" + v.Str
	case BooleanType:
		if v.Bool {
			return "b:1"
		}
		return "b:0"
	case VectorType:
		var b strings.Builder
		b.WriteString("v:")
		for _, component := range v.Vector {
			fmt.Fprintf(&b, "%08x", math.Float32bits(component))
		}
		return b.String()
	default:
		return "n:"
	}
}

// numeric reports whether the value is numeric.
func (v Value) numeric() bool {
	return v.Type == IntegerType || v.Type == RealType
}

// CompareValues compares two non-NULL values.
func CompareValues(a, b Value) (int, error) {
	if a.Null || b.Null {
		return 0, fmt.Errorf("sql: cannot compare NULL values")
	}
	if a.numeric() && b.numeric() {
		if a.Type == IntegerType && b.Type == IntegerType {
			switch {
			case a.Int < b.Int:
				return -1, nil
			case a.Int > b.Int:
				return 1, nil
			default:
				return 0, nil
			}
		}
		af, bf := a.asFloat(), b.asFloat()
		switch {
		case af < bf:
			return -1, nil
		case af > bf:
			return 1, nil
		default:
			return 0, nil
		}
	}
	if a.Type != b.Type {
		return 0, fmt.Errorf("sql: cannot compare %s with %s", a.Type, b.Type)
	}
	switch a.Type {
	case TextType:
		return strings.Compare(a.Str, b.Str), nil
	case BooleanType:
		switch {
		case !a.Bool && b.Bool:
			return -1, nil
		case a.Bool && !b.Bool:
			return 1, nil
		default:
			return 0, nil
		}
	default:
		return 0, fmt.Errorf("sql: cannot compare %s values", a.Type)
	}
}

func (v Value) asFloat() float64 {
	if v.Type == IntegerType {
		return float64(v.Int)
	}
	return v.Real
}

// CompareOrder compares values for ORDER BY, placing NULLs first.
func CompareOrder(a, b Value) int {
	if a.Null && b.Null {
		return 0
	}
	if a.Null {
		return -1
	}
	if b.Null {
		return 1
	}
	cmp, err := CompareValues(a, b)
	if err != nil {
		return strings.Compare(a.String(), b.String())
	}
	return cmp
}

// Truthy interprets a value as a boolean. The second result is false when the
// value is NULL.
func Truthy(v Value) (bool, bool) {
	if v.Null {
		return false, false
	}
	switch v.Type {
	case BooleanType:
		return v.Bool, true
	case IntegerType:
		return v.Int != 0, true
	case RealType:
		return v.Real != 0, true
	default:
		return false, false
	}
}

// Coerce converts v to typ.
func Coerce(v Value, typ DataType) (Value, error) {
	if v.Null {
		return Value{Type: typ, Null: true}, nil
	}
	if v.Type == typ {
		return v, nil
	}
	switch typ {
	case IntegerType:
		switch v.Type {
		case RealType:
			if v.Real != math.Trunc(v.Real) {
				return Value{}, fmt.Errorf("sql: cannot store %v in INTEGER column", v.Real)
			}
			return IntValue(int64(v.Real)), nil
		case BooleanType:
			if v.Bool {
				return IntValue(1), nil
			}
			return IntValue(0), nil
		case TextType:
			n, err := strconv.ParseInt(strings.TrimSpace(v.Str), 10, 64)
			if err != nil {
				return Value{}, fmt.Errorf("sql: cannot store %q in INTEGER column", v.Str)
			}
			return IntValue(n), nil
		}
	case RealType:
		switch v.Type {
		case IntegerType:
			return RealValue(float64(v.Int)), nil
		case TextType:
			f, err := strconv.ParseFloat(strings.TrimSpace(v.Str), 64)
			if err != nil {
				return Value{}, fmt.Errorf("sql: cannot store %q in REAL column", v.Str)
			}
			return RealValue(f), nil
		case BooleanType:
			if v.Bool {
				return RealValue(1), nil
			}
			return RealValue(0), nil
		}
	case TextType:
		return TextValue(v.String()), nil
	case BooleanType:
		switch v.Type {
		case IntegerType:
			return BoolValue(v.Int != 0), nil
		case RealType:
			return BoolValue(v.Real != 0), nil
		case TextType:
			switch strings.ToLower(strings.TrimSpace(v.Str)) {
			case "true", "t", "yes", "1":
				return BoolValue(true), nil
			case "false", "f", "no", "0":
				return BoolValue(false), nil
			}
			return Value{}, fmt.Errorf("sql: cannot store %q in BOOLEAN column", v.Str)
		}
	case VectorType:
		if v.Type == VectorType {
			return VectorValue(v.Vector), nil
		}
	}
	return Value{}, fmt.Errorf("sql: cannot store %s in %s column", v.Type, typ)
}

// CoerceColumn converts a value and validates a VECTOR column dimension.
func CoerceColumn(v Value, col Column) (Value, error) {
	coerced, err := Coerce(v, col.Type)
	if err != nil {
		return Value{}, err
	}
	if !coerced.Null && col.Type == VectorType && len(coerced.Vector) != col.VectorDim {
		return Value{}, fmt.Errorf("sql: vector for column %s has dimension %d, want %d", col.Name, len(coerced.Vector), col.VectorDim)
	}
	return coerced, nil
}
