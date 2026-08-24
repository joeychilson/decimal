package decimal

import (
	"bytes"
	"encoding/binary"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"math/big"
	"strconv"
)

// AppendText implements [encoding.TextAppender]. It appends the same lossless
// representation as [Decimal.String], does not retain dst, and never returns a
// non-nil error.
func (d Decimal) AppendText(dst []byte) ([]byte, error) {
	return d.Append(dst), nil
}

// MarshalText implements [encoding.TextMarshaler] using the same lossless
// representation as [Decimal.String]. It never returns a non-nil error.
func (d Decimal) MarshalText() ([]byte, error) {
	return d.Append(nil), nil
}

// UnmarshalText implements [encoding.TextUnmarshaler]. It uses [Parse] and
// replaces d only after the complete input has been validated successfully.
// UnmarshalText panics if d is nil.
func (d *Decimal) UnmarshalText(text []byte) error {
	if d == nil {
		panic("decimal: UnmarshalText on nil *Decimal")
	}
	value, err := Parse(string(text))
	if err != nil {
		return err
	}
	*d = value
	return nil
}

// AppendBinary implements [encoding.BinaryAppender]. The encoding is versioned,
// canonical for a representation, and preserves scale. It is not specified to
// sort in numeric order. AppendBinary does not retain dst and never returns a
// non-nil error.
func (d Decimal) AppendBinary(dst []byte) ([]byte, error) {
	coefficient, scale := decimalParts(d)
	magnitude := coefficient.Bytes()
	dst = append(dst, binaryEncodingVersion)
	if coefficient.Sign() < 0 {
		dst = append(dst, 1)
	} else {
		dst = append(dst, 0)
	}
	dst = binary.AppendVarint(dst, int64(scale))
	dst = binary.AppendUvarint(dst, uint64(len(magnitude)))
	dst = append(dst, magnitude...)
	return dst, nil
}

// MarshalBinary implements [encoding.BinaryMarshaler]. Its output is identical
// to d.AppendBinary(nil), and it never returns a non-nil error.
func (d Decimal) MarshalBinary() ([]byte, error) {
	return d.AppendBinary(nil)
}

// UnmarshalBinary implements [encoding.BinaryUnmarshaler]. It returns
// [ErrSyntax] for unknown versions, truncated or trailing input, and
// non-canonical encodings. It replaces d only after validating all input.
// UnmarshalBinary panics if d is nil.
func (d *Decimal) UnmarshalBinary(data []byte) error {
	if d == nil {
		panic("decimal: UnmarshalBinary on nil *Decimal")
	}
	if len(data) < 2 || data[0] != binaryEncodingVersion {
		return ErrSyntax
	}
	if data[1] > 1 {
		return ErrSyntax
	}
	negative := data[1] == 1
	data = data[2:]

	scale, n := binary.Varint(data)
	if n <= 0 {
		return ErrSyntax
	}
	var varintBuffer [binary.MaxVarintLen64]byte
	varintLength := binary.PutVarint(varintBuffer[:], scale)
	if !bytes.Equal(data[:n], varintBuffer[:varintLength]) {
		return ErrSyntax
	}
	data = data[n:]
	size, n := binary.Uvarint(data)
	if n <= 0 {
		return ErrSyntax
	}
	var uvarintBuffer [binary.MaxVarintLen64]byte
	uvarintLength := binary.PutUvarint(uvarintBuffer[:], size)
	if !bytes.Equal(data[:n], uvarintBuffer[:uvarintLength]) {
		return ErrSyntax
	}
	data = data[n:]
	if size != uint64(len(data)) {
		return ErrSyntax
	}
	if size > 0 && data[0] == 0 {
		return ErrSyntax
	}
	if size == 0 && negative {
		return ErrSyntax
	}

	coefficient := new(big.Int).SetBytes(data)
	if negative {
		coefficient.Neg(coefficient)
	}
	*d = makeDecimal(coefficient, Scale(scale))
	return nil
}

// MarshalJSON implements encoding/json.Marshaler and encoding/json/v2.Marshaler.
// It emits d as a JSON number, not a binary float, and preserves d's scale.
// Systems that cannot represent arbitrary-precision JSON numbers should use a
// string field or encoding/json/v2's StringifyNumbers option. MarshalJSON never
// returns a non-nil error.
func (d Decimal) MarshalJSON() ([]byte, error) {
	return d.Append(nil), nil
}

// UnmarshalJSON implements encoding/json.Unmarshaler and
// encoding/json/v2.Unmarshaler. It accepts a JSON number and replaces d only
// after the complete value is valid. It rejects null, strings, and non-numbers.
// Invalid JSON returns [ErrSyntax]; decimal syntax and range failures are
// returned as [*ParseError]. UnmarshalJSON panics if d is nil.
func (d *Decimal) UnmarshalJSON(data []byte) error {
	if d == nil {
		panic("decimal: UnmarshalJSON on nil *Decimal")
	}
	data = bytes.TrimSpace(data)
	value := jsontext.Value(data)
	if value.Kind() != jsontext.KindNumber || !value.IsValid() {
		return ErrSyntax
	}
	parsed, err := Parse(string(data))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// MarshalJSONTo implements encoding/json/v2.MarshalerTo without constructing an
// intermediate JSON buffer. It respects the encoder's StringifyNumbers option
// and returns errors reported by the encoder. MarshalJSONTo panics if enc is
// nil.
func (d Decimal) MarshalJSONTo(enc *jsontext.Encoder) error {
	if enc == nil {
		panic("decimal: MarshalJSONTo on nil *jsontext.Encoder")
	}
	text := d.Append(nil)
	stringify, _ := jsonv2.GetOption(enc.Options(), jsonv2.StringifyNumbers)
	if stringify {
		text = strconv.AppendQuote(nil, string(text))
	}
	return enc.WriteValue(jsontext.Value(text))
}

// UnmarshalJSONFrom implements encoding/json/v2.UnmarshalerFrom without
// constructing an intermediate JSON buffer. It respects the decoder's
// StringifyNumbers option and replaces d only after successful decoding.
// It returns decoder errors directly, [ErrSyntax] for an unexpected JSON kind,
// and [*ParseError] for decimal syntax or range failures. UnmarshalJSONFrom
// panics if d or dec is nil.
func (d *Decimal) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if d == nil {
		panic("decimal: UnmarshalJSONFrom on nil *Decimal")
	}
	if dec == nil {
		panic("decimal: UnmarshalJSONFrom on nil *jsontext.Decoder")
	}
	value, err := dec.ReadValue()
	if err != nil {
		return err
	}
	stringify, _ := jsonv2.GetOption(dec.Options(), jsonv2.StringifyNumbers)
	if stringify {
		if value.Kind() != jsontext.KindString {
			return ErrSyntax
		}
		unquoted, unquoteErr := strconv.Unquote(string(value))
		if unquoteErr != nil {
			return ErrSyntax
		}
		value = jsontext.Value(unquoted)
	}
	if value.Kind() != jsontext.KindNumber || !value.IsValid() {
		return ErrSyntax
	}
	parsed, err := Parse(string(value))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

const binaryEncodingVersion byte = 1
