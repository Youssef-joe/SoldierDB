package sql

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/Youssef-joe/YousDB/engine/btree"
)

// Row encoding tags. The tag makes a row self-describing so a table can be
// decoded without trusting the schema for storage types.
const (
	tagNull byte = iota
	tagInt
	tagReal
	tagText
	tagBool
)

// encodePK encodes a primary key so that byte order matches value order.
func encodePK(v Value) ([]byte, error) {
	if v.Null {
		return nil, fmt.Errorf("sql: primary key cannot be NULL")
	}
	switch v.Type {
	case IntegerType:
		buf := make([]byte, 8)
		// Flip the sign bit so negative integers sort before positive ones.
		binary.BigEndian.PutUint64(buf, uint64(v.Int)^(uint64(1)<<63))
		return buf, nil
	case TextType:
		return []byte(v.Str), nil
	default:
		return nil, fmt.Errorf("sql: unsupported primary key type %s", v.Type)
	}
}

// encodeRow serializes a row of typed values.
func encodeRow(row []Value) ([]byte, error) {
	buf := make([]byte, 0, 32*len(row))
	var scratch [binary.MaxVarintLen64]byte
	for _, v := range row {
		if v.Null {
			buf = append(buf, tagNull)
			continue
		}
		switch v.Type {
		case IntegerType:
			buf = append(buf, tagInt)
			var b [8]byte
			binary.LittleEndian.PutUint64(b[:], uint64(v.Int))
			buf = append(buf, b[:]...)
		case RealType:
			buf = append(buf, tagReal)
			var b [8]byte
			binary.LittleEndian.PutUint64(b[:], math.Float64bits(v.Real))
			buf = append(buf, b[:]...)
		case TextType:
			buf = append(buf, tagText)
			n := binary.PutUvarint(scratch[:], uint64(len(v.Str)))
			buf = append(buf, scratch[:n]...)
			buf = append(buf, v.Str...)
		case BooleanType:
			buf = append(buf, tagBool)
			if v.Bool {
				buf = append(buf, 1)
			} else {
				buf = append(buf, 0)
			}
		default:
			return nil, fmt.Errorf("sql: cannot encode value of type %s", v.Type)
		}
	}
	if len(buf) > btree.MaxValueSize {
		return nil, fmt.Errorf("sql: row is too large (%d bytes, limit %d)", len(buf), btree.MaxValueSize)
	}
	return buf, nil
}

// decodeRow deserializes a row and coerces each value to the table's column
// type. The schema is authoritative for display and expression typing.
func decodeRow(data []byte, table *Table) ([]Value, error) {
	row := make([]Value, len(table.Columns))
	pos := 0
	for i := range table.Columns {
		if pos >= len(data) {
			return nil, fmt.Errorf("sql: truncated row for table %s", table.Name)
		}
		tag := data[pos]
		pos++
		var v Value
		switch tag {
		case tagNull:
			v = NullValue()
		case tagInt:
			if pos+8 > len(data) {
				return nil, fmt.Errorf("sql: truncated integer in row")
			}
			v = IntValue(int64(binary.LittleEndian.Uint64(data[pos : pos+8])))
			pos += 8
		case tagReal:
			if pos+8 > len(data) {
				return nil, fmt.Errorf("sql: truncated real in row")
			}
			v = RealValue(math.Float64frombits(binary.LittleEndian.Uint64(data[pos : pos+8])))
			pos += 8
		case tagText:
			n, adv := binary.Uvarint(data[pos:])
			if adv <= 0 || pos+adv+int(n) > len(data) {
				return nil, fmt.Errorf("sql: truncated text in row")
			}
			pos += adv
			v = TextValue(string(data[pos : pos+int(n)]))
			pos += int(n)
		case tagBool:
			if pos >= len(data) {
				return nil, fmt.Errorf("sql: truncated boolean in row")
			}
			v = BoolValue(data[pos] != 0)
			pos++
		default:
			return nil, fmt.Errorf("sql: unknown row tag %d", tag)
		}
		coerced, err := Coerce(v, table.Columns[i].Type)
		if err != nil {
			return nil, fmt.Errorf("sql: column %s: %w", table.Columns[i].Name, err)
		}
		row[i] = coerced
	}
	return row, nil
}
