package decimal

import (
	"math"
	"math/big"
	"math/bits"
	"strconv"
)

// ParseError describes an error at a byte offset in decimal text.
type ParseError struct {
	// Input is the complete input passed to Parse.
	Input string

	// Offset is the zero-based byte offset at which parsing failed. It is
	// len(Input) when the error occurred at end of input.
	Offset int

	// Err is the underlying error, normally ErrSyntax or ErrRange.
	Err error
}

// Error returns the quoted input, failure byte offset, and underlying error of
// e. For a nil receiver, it returns "decimal: <nil>".
func (e *ParseError) Error() string {
	if e == nil {
		return "decimal: <nil>"
	}
	if e.Err == nil {
		return "decimal: parsing " + strconv.Quote(e.Input) + " at byte " + strconv.Itoa(e.Offset)
	}
	return "decimal: parsing " + strconv.Quote(e.Input) + " at byte " + strconv.Itoa(e.Offset) + ": " + e.Err.Error()
}

// Unwrap returns e.Err, or nil if e is nil.
func (e *ParseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Parse parses s as a decimal and preserves its represented scale.
//
// The accepted grammar is:
//
//	decimal  = [ sign ] ( digits [ "." [ digits ] ] | "." digits )
//	           [ ( "e" | "E" ) [ sign ] digits ]
//	sign     = "+" | "-"
//	digits   = digit { digit }
//
// Parse does not accept surrounding space, digit separators, NaN, or infinity.
// A syntax error is a [*ParseError] wrapping [ErrSyntax]. An exponent outside
// the supported [Scale] range produces a [*ParseError] wrapping [ErrRange].
func Parse(s string) (Decimal, error) {
	if s == "" {
		return Decimal{}, &ParseError{Input: s, Offset: 0, Err: ErrSyntax}
	}

	i := 0
	negative := false
	if s[i] == '+' || s[i] == '-' {
		negative = s[i] == '-'
		i++
		if i == len(s) {
			return Decimal{}, &ParseError{Input: s, Offset: i, Err: ErrSyntax}
		}
	}

	digits := make([]byte, 0, len(s))
	integerDigits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		digits = append(digits, s[i])
		integerDigits++
		i++
	}

	fractionDigits := 0
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			digits = append(digits, s[i])
			fractionDigits++
			i++
		}
	}
	if integerDigits == 0 && fractionDigits == 0 {
		return Decimal{}, &ParseError{Input: s, Offset: i, Err: ErrSyntax}
	}

	var exponentMagnitude uint64
	exponentNegative := false
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			exponentNegative = s[i] == '-'
			i++
		}
		exponentStart := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if exponentStart == i {
			return Decimal{}, &ParseError{Input: s, Offset: i, Err: ErrSyntax}
		}
		if i == len(s) {
			var err error
			exponentMagnitude, err = strconv.ParseUint(s[exponentStart:], 10, 64)
			if err != nil {
				return Decimal{}, &ParseError{Input: s, Offset: len(s), Err: ErrRange}
			}
		}
	}
	if i != len(s) {
		return Decimal{}, &ParseError{Input: s, Offset: i, Err: ErrSyntax}
	}

	fraction := uint64(fractionDigits)
	var scale Scale
	if exponentNegative {
		if exponentMagnitude > uint64(math.MaxInt64)-fraction {
			return Decimal{}, &ParseError{Input: s, Offset: len(s), Err: ErrRange}
		}
		scale = Scale(fraction + exponentMagnitude)
	} else if exponentMagnitude <= fraction {
		scale = Scale(fraction - exponentMagnitude)
	} else {
		difference := exponentMagnitude - fraction
		if difference > uint64(math.MaxInt64)+1 {
			return Decimal{}, &ParseError{Input: s, Offset: len(s), Err: ErrRange}
		}
		if difference == uint64(math.MaxInt64)+1 {
			scale = Scale(math.MinInt64)
		} else {
			scale = -Scale(difference)
		}
	}

	var coefficient big.Int
	significant := 0
	for significant < len(digits) && digits[significant] == '0' {
		significant++
	}
	if significant < len(digits) {
		setDecimalDigits(&coefficient, digits[significant:])
	}
	if negative {
		coefficient.Neg(&coefficient)
	}
	return makeDecimal(&coefficient, scale), nil
}

// MustParse is like [Parse] but panics if s is invalid. It is intended for
// package initialization, tests, and other inputs known at development time.
func MustParse(s string) Decimal {
	d, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

var (
	bigTen9  = big.NewInt(1_000_000_000)
	bigTen19 = new(big.Int).SetUint64(10_000_000_000_000_000_000)
)

// setDecimalDigits sets z from a non-empty sequence of validated decimal
// digits. It uses nineteen-digit chunks on 64-bit systems and nine-digit
// chunks on 32-bit systems so each chunk fits in one machine word.
func setDecimalDigits(z *big.Int, digits []byte) {
	const chunkDigits = decimalWordDigits
	base := bigTen9
	if bits.UintSize == 64 {
		base = bigTen19
	}
	first := len(digits) % chunkDigits
	if first == 0 {
		first = chunkDigits
	}
	var chunk uint64
	for _, character := range digits[:first] {
		chunk = chunk*10 + uint64(character-'0')
	}
	z.SetUint64(chunk)

	var part big.Int
	for start := first; start < len(digits); start += chunkDigits {
		chunk = 0
		for _, character := range digits[start : start+chunkDigits] {
			chunk = chunk*10 + uint64(character-'0')
		}
		z.Mul(z, base)
		part.SetUint64(chunk)
		z.Add(z, &part)
	}
}
