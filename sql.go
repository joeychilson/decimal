package decimal

import (
	"database/sql/driver"
	"fmt"
	"reflect"
)

// Scan implements [sql.Scanner]. It accepts decimal text from string or []byte
// values and exact values from all integer types. It also accepts finite
// float32 and float64 values using the semantics of [FromFloat]. Scan rejects
// nil, unsupported types, and non-finite floats with [ErrInvalidOperation]; use
// sql.Null[Decimal] when a database column is nullable. Invalid text returns a
// [*ParseError]. Scan replaces d only after a successful conversion and panics
// if d is nil.
func (d *Decimal) Scan(src any) error {
	if d == nil {
		panic("decimal: Scan on nil *Decimal")
	}
	var value Decimal
	var err error
	switch src := src.(type) {
	case string:
		value, err = Parse(src)
	case []byte:
		value, err = Parse(string(src))
	case float32:
		value, err = FromFloat(src)
	case float64:
		value, err = FromFloat(src)
	case nil:
		err = ErrInvalidOperation
	default:
		reflected := reflect.ValueOf(src)
		switch reflected.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			value = FromInt(reflected.Int())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			value = FromInt(reflected.Uint())
		default:
			err = fmt.Errorf("decimal: cannot scan %T: %w", src, ErrInvalidOperation)
		}
	}
	if err != nil {
		return err
	}
	*d = value
	return nil
}

// Value implements [driver.Valuer]. It returns the same lossless text as
// [Decimal.String], allowing database drivers to bind it as an exact NUMERIC or
// DECIMAL value without passing through binary floating point. Value never
// returns a non-nil error.
func (d Decimal) Value() (driver.Value, error) {
	return d.String(), nil
}
