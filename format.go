package decimal

import (
	"bytes"
	"fmt"
	"math/big"
	"strconv"
)

// String returns a lossless decimal representation of d that round-trips
// through [Parse], including its scale. It uses fixed-point notation for
// ordinary non-negative scales and exponent notation for negative scales or
// when fixed-point notation would require excessive leading-zero padding. It
// never uses locale-specific separators.
func (d Decimal) String() string {
	return string(d.Append(nil))
}

// Append appends the same representation as [Decimal.String] to dst and
// returns the extended buffer. Append does not retain dst.
func (d Decimal) Append(dst []byte) []byte {
	const maximumFixedLeadingZeros = 64

	coefficient, scale := decimalParts(d)
	if scale < 0 {
		dst = coefficient.Append(dst, 10)
		dst = append(dst, 'e')
		return strconv.AppendUint(dst, scaleMagnitude(scale), 10)
	}

	start := len(dst)
	dst = coefficient.Append(dst, 10)
	if scale == 0 {
		return dst
	}

	digitsStart := start
	if dst[digitsStart] == '-' {
		digitsStart++
	}
	digits := len(dst) - digitsStart
	fractionDigits := uint64(scale)
	if fractionDigits < uint64(digits) {
		point := len(dst) - int(fractionDigits)
		dst = append(dst, 0)
		copy(dst[point+1:], dst[point:len(dst)-1])
		dst[point] = '.'
		return dst
	}
	if fractionDigits-uint64(digits) > maximumFixedLeadingZeros {
		dst = append(dst, 'e', '-')
		return strconv.AppendUint(dst, fractionDigits, 10)
	}

	shift := fractionDigits - uint64(digits) + 2
	oldEnd := len(dst)
	dst = appendZeros(dst, shift)
	shiftLength := int(shift)
	copy(dst[digitsStart+shiftLength:], dst[digitsStart:oldEnd])
	dst[digitsStart] = '0'
	dst[digitsStart+1] = '.'
	for i := range shiftLength - 2 {
		dst[digitsStart+2+i] = '0'
	}
	return dst
}

// Format implements [fmt.Formatter]. The supported verbs are:
//
//	%s, %v  the lossless representation returned by String
//	%f, %F  fixed-point notation; precision is digits after the point
//	%e, %E  scientific notation; precision is digits after the point
//	%g, %G  compact notation; precision is significant digits
//	%q      a quoted lossless representation
//
// The +, space, -, 0, #, and width flags follow the corresponding conventions
// in package fmt. The default precision for %f, %F, %e, %E, %#g, and %#G is 6.
// Without an explicit precision or the # flag, %g and %G preserve all
// significant digits after removing insignificant trailing zeros, using
// exponent notation for exponents below -4 or at least 6. Formatting rounds
// only the rendered text and never d itself; ties use HalfEven. Format writes
// a formatting-error diagnostic for an unsafe width, precision, or fixed-point
// result. It panics if state is nil.
func (d Decimal) Format(state fmt.State, verb rune) {
	if state == nil {
		panic("decimal: Format on nil fmt.State")
	}
	width, hasWidth := state.Width()
	precision, hasPrecision := state.Precision()
	invalidWidth := hasWidth && (width < 0 || width > maximumFormatParameter)
	invalidPrecision := hasPrecision && (precision < 0 || precision > maximumFormatParameter)
	if invalidWidth || invalidPrecision {
		_, _ = state.Write([]byte("%!" + string(verb) + "(decimal.Decimal=" + d.String() + ")"))
		return
	}
	text, numeric := formatDecimal(d, state, verb)
	writePadded(state, text, numeric)
}

// maximumFormatParameter matches the bound enforced by package fmt before it
// constructs a State for a Formatter.
const maximumFormatParameter = 1_000_000

func appendZeros(dst []byte, count uint64) []byte {
	const zeros = "0000000000000000000000000000000000000000000000000000000000000000"
	maximum := uint64(int(^uint(0) >> 1))
	if count > maximum-uint64(len(dst)) {
		panic("decimal: formatted value too large")
	}
	for count >= uint64(len(zeros)) {
		dst = append(dst, zeros...)
		count -= uint64(len(zeros))
	}
	return append(dst, zeros[:int(count)]...)
}

func formatDecimal(d Decimal, state fmt.State, verb rune) ([]byte, bool) {
	precision, hasPrecision := state.Precision()
	switch verb {
	case 's', 'v':
		return d.Append(nil), true
	case 'q':
		text := d.String()
		if state.Flag('#') && strconv.CanBackquote(text) {
			quoted := make([]byte, 0, len(text)+2)
			quoted = append(quoted, '`')
			quoted = append(quoted, text...)
			return append(quoted, '`'), false
		}
		if state.Flag('+') {
			return strconv.AppendQuoteToASCII(nil, text), false
		}
		return strconv.AppendQuote(nil, text), false
	case 'f', 'F':
		if !hasPrecision {
			precision = 6
		}
		coefficient, scale := decimalParts(d)
		exponent := setAdjustedExponent(new(big.Int), coefficient, scale)
		if !exponent.IsInt64() || exponent.Int64() > maximumFormatParameter {
			return []byte("%!" + string(verb) + "(decimal.Decimal=" + d.String() + ")"), false
		}
		negative := d.IsNegative()
		rounded, err := d.Rescale(Scale(precision), HalfEven)
		if err != nil {
			return []byte("%!" + string(verb) + "(decimal.Decimal=" + d.String() + ")"), false
		}
		text := rounded.Append(nil)
		if negative && rounded.IsZero() {
			text = append([]byte{'-'}, text...)
		}
		if precision == 0 && state.Flag('#') {
			text = append(text, '.')
		}
		return text, true
	case 'e', 'E':
		if !hasPrecision {
			precision = 6
		}
		rounded, err := d.Round(uint(precision+1), HalfEven)
		if err != nil {
			return []byte("%!" + string(verb) + "(decimal.Decimal=" + d.String() + ")"), false
		}
		return scientificText(rounded, precision, verb == 'E', state.Flag('#')), true
	case 'g', 'G':
		alternate := state.Flag('#')
		exponentLimit := 6
		rounded := d
		if !hasPrecision && !alternate {
			precision = int(d.Precision())
		} else {
			if !hasPrecision {
				precision = 6
			} else if precision == 0 {
				precision = 1
			}
			exponentLimit = precision
			var err error
			rounded, err = d.Round(uint(precision), HalfEven)
			if err != nil {
				return []byte("%!" + string(verb) + "(decimal.Decimal=" + d.String() + ")"), false
			}
		}
		coefficient, scale := decimalParts(rounded)
		exponent := setAdjustedExponent(new(big.Int), coefficient, scale)
		useExponent := !exponent.IsInt64()
		if !useExponent {
			exponentValue := exponent.Int64()
			useExponent = exponentValue < -4 || exponentValue >= int64(exponentLimit)
		}
		if useExponent {
			text := scientificText(rounded, precision-1, verb == 'G', alternate)
			if !alternate {
				text = trimTrailingFractionalZeros(text)
			}
			return text, true
		}

		exponentValue := exponent.Int64()
		fractional := max(int64(precision)-exponentValue-1, 0)
		fixed, err := rounded.Rescale(Scale(fractional), HalfEven)
		if err != nil {
			return []byte("%!" + string(verb) + "(decimal.Decimal=" + d.String() + ")"), false
		}
		text := fixed.Append(nil)
		if alternate && fractional == 0 {
			text = append(text, '.')
		} else if !alternate {
			text = trimTrailingFractionalZeros(text)
		}
		return text, true
	default:
		return []byte("%!" + string(verb) + "(decimal.Decimal=" + d.String() + ")"), false
	}
}

func scientificText(d Decimal, fractional int, upper, alternate bool) []byte {
	coefficient, scale := decimalParts(d)
	digits := coefficient.String()
	negative := digits[0] == '-'
	if negative {
		digits = digits[1:]
	}
	wanted := fractional + 1
	if len(digits) < wanted {
		digits += string(appendZeros(nil, uint64(wanted-len(digits))))
	}

	text := make([]byte, 0, wanted+8)
	if negative {
		text = append(text, '-')
	}
	text = append(text, digits[0])
	if fractional > 0 || alternate {
		text = append(text, '.')
	}
	if fractional > 0 {
		text = append(text, digits[1:wanted]...)
	}
	if upper {
		text = append(text, 'E')
	} else {
		text = append(text, 'e')
	}

	exponent := setAdjustedExponent(new(big.Int), coefficient, scale)
	if exponent.Sign() < 0 {
		text = append(text, '-')
		exponent.Neg(exponent)
	} else {
		text = append(text, '+')
	}
	exponentText := exponent.String()
	if len(exponentText) < 2 {
		text = append(text, '0')
	}
	return append(text, exponentText...)
}

func trimTrailingFractionalZeros(text []byte) []byte {
	exponent := len(text)
	for i, b := range text {
		if b == 'e' || b == 'E' {
			exponent = i
			break
		}
	}
	point := -1
	for i := range exponent {
		if text[i] == '.' {
			point = i
			break
		}
	}
	if point < 0 {
		return text
	}
	end := exponent
	for end > point+1 && text[end-1] == '0' {
		end--
	}
	if end == point+1 {
		end = point
	}
	if exponent == len(text) {
		return text[:end]
	}
	copy(text[end:], text[exponent:])
	return text[:end+len(text)-exponent]
}

func writePadded(state fmt.State, text []byte, numeric bool) {
	// fmt.Formatter cannot report errors from the supplied State.Write method.
	if numeric && len(text) > 0 && text[0] != '-' {
		if state.Flag('+') {
			text = append([]byte{'+'}, text...)
		} else if state.Flag(' ') {
			text = append([]byte{' '}, text...)
		}
	}
	width, hasWidth := state.Width()
	if !hasWidth || width <= len(text) {
		_, _ = state.Write(text)
		return
	}
	padding := width - len(text)
	if state.Flag('-') {
		_, _ = state.Write(text)
		_, _ = state.Write(bytes.Repeat([]byte{' '}, padding))
		return
	}
	if numeric && state.Flag('0') {
		if text[0] == '+' || text[0] == '-' || text[0] == ' ' {
			_, _ = state.Write(text[:1])
			_, _ = state.Write(appendZeros(nil, uint64(padding)))
			_, _ = state.Write(text[1:])
			return
		}
		_, _ = state.Write(appendZeros(nil, uint64(padding)))
		_, _ = state.Write(text)
		return
	}
	_, _ = state.Write(bytes.Repeat([]byte{' '}, padding))
	_, _ = state.Write(text)
}
