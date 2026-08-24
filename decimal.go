// Package decimal provides immutable, arbitrary-precision base-10 numbers.
//
// A Decimal is a finite value of the form coefficient × 10⁻ˢᶜᵃˡᵉ. The coefficient
// has arbitrary precision and the scale records the value's decimal quantum.
// Consequently, 1.20 and 1.2 are numerically equal but retain different scales.
// Decimal deliberately has no NaN, infinity, or signed zero values.
//
// The zero value of Decimal is the number 0 with scale 0. Decimal values may be
// copied freely. All value-returning operations leave their operands unchanged,
// and all methods other than decoding and scanning methods are safe for
// concurrent use.
//
// Add, Sub, and Mul are exact. Operations that are not necessarily finite, such
// as division and square root, either return an exact Decimal or an error. Use a
// Context when a rounded result is desired. There is intentionally no mutable
// package-wide context: every rounding decision is visible at the call site.
package decimal

import (
	"cmp"
	"errors"
	"math"
	"math/big"
	"math/bits"
)

// Scale is the number of digits to the right of the decimal point. A negative
// scale represents trailing integral zeros: a coefficient of 12 and a scale
// of -2 represent 1.2e3.
//
// Scale is a distinct type so that APIs cannot accidentally confuse a decimal
// scale with a count of significant digits.
type Scale int64

// Sentinel errors returned by this package. Errors may wrap one of these
// values; callers should test them with [errors.Is].
var (
	// ErrSyntax means that text is not a valid decimal literal.
	ErrSyntax = errors.New("invalid syntax")

	// ErrRange means that a value, scale, precision, or conversion target is
	// outside its supported range.
	ErrRange = errors.New("value out of range")

	// ErrInexact means that an operation requested an exact result but the
	// mathematical result cannot be represented under that requirement.
	// Examples include 1/3 in exact arithmetic and converting 1.5 to an integer.
	ErrInexact = errors.New("inexact result")

	// ErrDivisionByZero means that a divisor was zero.
	ErrDivisionByZero = errors.New("division by zero")

	// ErrInvalidOperation means that an operation has no real, finite decimal
	// result, such as the square root of a negative number.
	ErrInvalidOperation = errors.New("invalid operation")

	// ErrInvalidRoundingMode means that a RoundingMode is unknown.
	ErrInvalidRoundingMode = errors.New("invalid rounding mode")
)

// Integer is the set of built-in and user-defined integer types accepted by
// generic constructors and conversions.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

// Decimal is an immutable, finite, arbitrary-precision base-10 number.
//
// Decimal is intentionally not comparable with ==. Use [Decimal.Equal] for
// numeric equality or [Decimal.SameRepresentation] when scale is significant.
// This prevents representation details from silently becoming equality
// semantics and leaves the implementation free to evolve.
//
// The unexported fields make Decimal opaque. They have no user-visible meaning.
type Decimal struct {
	_     [0]func() // Keep accidental == and map-key use from compiling.
	value *decimalValue
}

// New returns coefficient × 10⁻ˢᶜᵃˡᵉ. It preserves scale, including for zero.
// The generic coefficient accepts built-in integer types and integer types
// declared by callers.
func New[T Integer](coefficient T, scale Scale) Decimal {
	var n big.Int
	// The all-ones value is negative only for signed integer types.
	if ^T(0) < T(0) {
		n.SetInt64(int64(coefficient))
	} else {
		n.SetUint64(uint64(coefficient))
	}
	return makeDecimal(&n, scale)
}

// NewBig returns coefficient × 10⁻ˢᶜᵃˡᵉ. It copies coefficient and does not retain
// it. NewBig panics if coefficient is nil.
func NewBig(coefficient *big.Int, scale Scale) Decimal {
	if coefficient == nil {
		panic("decimal: nil *big.Int")
	}
	return makeDecimal(new(big.Int).Set(coefficient), scale)
}

// FromInt returns the scale-zero Decimal equal to x.
func FromInt[T Integer](x T) Decimal {
	return New(x, 0)
}

// Canonical returns a numerically equal Decimal with insignificant trailing
// coefficient zeros removed as far as the range of [Scale] permits. Canonical
// maps every representation of zero to the zero value.
func (d Decimal) Canonical() Decimal {
	coefficient, scale := decimalParts(d)
	if coefficient.Sign() == 0 {
		return Decimal{}
	}
	if scale == Scale(math.MinInt64) || !hasTrailingDecimalZero(coefficient) {
		return d
	}
	if coefficient.IsInt64() {
		value, reducedScale := removeSmallTrailingDecimalZeros(coefficient.Int64(), scale, Scale(math.MinInt64))
		return New(value, reducedScale)
	}
	if coefficient.IsUint64() {
		value, reducedScale := removeSmallTrailingDecimalZeros(coefficient.Uint64(), scale, Scale(math.MinInt64))
		return New(value, reducedScale)
	}
	coefficient = new(big.Int).Set(coefficient)
	coefficient.Quo(coefficient, bigTen)
	scale--
	if scale > Scale(math.MinInt64) && hasTrailingDecimalZero(coefficient) {
		scale = removeTrailingDecimalZeros(coefficient, scale, Scale(math.MinInt64))
	}
	return makeDecimal(coefficient, scale)
}

// IsCanonical reports whether d is in the representation produced by
// [Decimal.Canonical].
func (d Decimal) IsCanonical() bool {
	coefficient, scale := decimalParts(d)
	if coefficient.Sign() == 0 {
		return scale == 0
	}
	return scale == Scale(math.MinInt64) || !hasTrailingDecimalZero(coefficient)
}

// Sign returns -1, 0, or +1 according to whether d is negative, zero, or
// positive.
func (d Decimal) Sign() int {
	coefficient, _ := decimalParts(d)
	return coefficient.Sign()
}

// IsZero reports whether d is numerically zero, regardless of its scale.
// This method also gives Decimal useful omitzero behavior with encoding/json.
func (d Decimal) IsZero() bool {
	return d.Sign() == 0
}

// IsPositive reports whether d is greater than zero.
func (d Decimal) IsPositive() bool {
	return d.Sign() > 0
}

// IsNegative reports whether d is less than zero.
func (d Decimal) IsNegative() bool {
	return d.Sign() < 0
}

// IsInt reports whether d has no non-zero fractional digits.
func (d Decimal) IsInt() bool {
	coefficient, scale := decimalParts(d)
	if coefficient.Sign() == 0 || scale <= 0 {
		return true
	}
	digits := uint64(decimalDigitCount(coefficient))
	if uint64(scale) >= digits {
		return false
	}
	var divisor big.Int
	setPowerOfTen(&divisor, uint64(scale))
	return new(big.Int).Rem(coefficient, &divisor).Sign() == 0
}

// Scale returns d's represented scale. Numeric equality does not imply equal
// scales.
func (d Decimal) Scale() Scale {
	_, scale := decimalParts(d)
	return scale
}

// Precision returns the number of decimal digits in the absolute coefficient,
// including significant trailing zeros. The precision of every representation
// of zero is 1.
func (d Decimal) Precision() uint {
	coefficient, _ := decimalParts(d)
	if coefficient.Sign() == 0 {
		return 1
	}
	return uint(decimalDigitCount(coefficient))
}

// Coefficient returns a new big.Int containing d's signed coefficient. The
// caller may modify the result without affecting d.
func (d Decimal) Coefficient() *big.Int {
	coefficient, _ := decimalParts(d)
	return new(big.Int).Set(coefficient)
}

// scaledCoefficient keeps a borrowed immutable coefficient paired with the
// scale that gives it meaning inside arithmetic kernels.
type scaledCoefficient struct {
	coefficient *big.Int
	scale       Scale
}

// decimalParts borrows d's immutable coefficient. The zero value is represented
// by the package's immutable zero coefficient at scale 0.
func decimalParts(d Decimal) (*big.Int, Scale) {
	if d.value == nil {
		return &zeroCoefficient, 0
	}
	return &d.value.coefficient, d.value.scale
}

var (
	zeroCoefficient big.Int
	bigOne          = big.NewInt(1)
	bigTwo          = big.NewInt(2)
	bigFive         = big.NewInt(5)
	bigTen          = big.NewInt(10)
)

const decimalWordDigits = 9 + 10*(bits.UintSize/64)

func decimalDigitCount(coefficient *big.Int) int {
	bitLength := coefficient.BitLen()
	if bitLength <= 64 {
		value := coefficient.Uint64()
		switch {
		case value < 10:
			return 1
		case value < 100:
			return 2
		case value < 1_000:
			return 3
		case value < 10_000:
			return 4
		case value < 100_000:
			return 5
		case value < 1_000_000:
			return 6
		case value < 10_000_000:
			return 7
		case value < 100_000_000:
			return 8
		case value < 1_000_000_000:
			return 9
		case value < 10_000_000_000:
			return 10
		case value < 100_000_000_000:
			return 11
		case value < 1_000_000_000_000:
			return 12
		case value < 10_000_000_000_000:
			return 13
		case value < 100_000_000_000_000:
			return 14
		case value < 1_000_000_000_000_000:
			return 15
		case value < 10_000_000_000_000_000:
			return 16
		case value < 100_000_000_000_000_000:
			return 17
		case value < 1_000_000_000_000_000_000:
			return 18
		case value < 10_000_000_000_000_000_000:
			return 19
		default:
			return 20
		}
	}

	// floor(log10(2)*2^64). The fixed-point product underestimates the
	// decimal length by at most one for every possible big.Int bit length.
	// Comparing with the adjacent power of ten corrects that one-bit ambiguity
	// without formatting the entire coefficient.
	const log10Of2Q64 = uint64(0x4d104d427de7fbcc)
	digits, _ := bits.Mul64(uint64(bitLength-1), log10Of2Q64)
	count := int(digits) + 1
	var threshold *big.Int
	if count < len(smallPowersOfTen) {
		threshold = &smallPowersOfTen[count]
	} else {
		threshold = setPowerOfTen(new(big.Int), uint64(count))
	}
	if coefficient.CmpAbs(threshold) >= 0 {
		count++
	}
	return count
}

var smallPowersOfTen = func() [256]big.Int {
	var powers [256]big.Int
	powers[0].SetInt64(1)
	for i := range len(powers) - 1 {
		powers[i+1].Mul(&powers[i], bigTen)
	}
	return powers
}()

// setPowerOfTen sets z to 10^exponent. The returned Int is always z and is
// therefore owned by the caller.
func setPowerOfTen(z *big.Int, exponent uint64) *big.Int {
	if exponent < uint64(len(smallPowersOfTen)) {
		return z.Set(&smallPowersOfTen[exponent])
	}
	var power big.Int
	power.SetUint64(exponent)
	return z.Exp(bigTen, &power, nil)
}

// scaleDistance returns high-low across the full Scale range. Callers must
// establish high >= low.
func scaleDistance(high, low Scale) uint64 {
	return uint64(high) - uint64(low)
}

func makeDecimal(coefficient *big.Int, scale Scale) Decimal {
	if coefficient.Sign() == 0 && scale == 0 {
		return Decimal{}
	}
	value := &decimalValue{scale: scale}
	// All callers pass a private or already-immutable Int. Transferring its
	// representation avoids copying every arithmetic result. NewBig makes the
	// one required defensive copy at the public ownership boundary.
	value.coefficient = *coefficient
	return Decimal{value: value}
}

// decimalValue is immutable after construction. Keeping the coefficient in a
// separately allocated value makes copying Decimal cheap without exposing
// math/big's aliasing rules to callers.
type decimalValue struct {
	coefficient big.Int
	scale       Scale
}

// multiplyByPowerOfTen sets z to x*10^exponent. It reads cached small powers
// directly but never exposes their mutable big.Int values to callers.
func multiplyByPowerOfTen(z, x *big.Int, exponent uint64) *big.Int {
	if x.Sign() == 0 {
		return z.SetInt64(0)
	}
	if exponent < uint64(len(smallPowersOfTen)) {
		return z.Mul(x, &smallPowersOfTen[exponent])
	}
	var power big.Int
	setPowerOfTen(&power, exponent)
	return z.Mul(x, &power)
}

func adjustedExponentScale(coefficient *big.Int, scale Scale) (Scale, bool) {
	digits := Scale(decimalDigitCount(coefficient) - 1)
	return subtractScales(digits, scale)
}

// subtractScales returns x-y and reports whether the result fits Scale. On
// overflow, it returns zero and false.
func subtractScales(x, y Scale) (Scale, bool) {
	if y > 0 && x < Scale(math.MinInt64)+y {
		return 0, false
	}
	if y < 0 && x > Scale(math.MaxInt64)+y {
		return 0, false
	}
	return x - y, true
}

func setAdjustedExponent(z *big.Int, coefficient *big.Int, scale Scale) *big.Int {
	if coefficient.Sign() == 0 {
		return z.SetInt64(0)
	}
	z.SetInt64(int64(scale))
	z.Neg(z)
	digits := decimalDigitCount(coefficient) - 1
	if digits == 0 {
		return z
	}
	var digitValue big.Int
	digitValue.SetInt64(int64(digits))
	return z.Add(z, &digitValue)
}

// addScales returns x+y and reports whether the result fits Scale. On overflow,
// it returns zero and false.
func addScales(x, y Scale) (Scale, bool) {
	if y > 0 && x > Scale(math.MaxInt64)-y {
		return 0, false
	}
	if y < 0 && x < Scale(math.MinInt64)-y {
		return 0, false
	}
	return x + y, true
}

// scaleAccumulator evaluates a linear scale expression without allocating
// until fixed-width arithmetic overflows. Once promoted, it remains wide so
// later terms may cancel the overflow before the final scale is validated.
type scaleAccumulator struct {
	small          Scale
	promoted       bool
	large, operand big.Int
}

func (a *scaleAccumulator) add(value Scale) {
	if !a.promoted {
		if sum, ok := addScales(a.small, value); ok {
			a.small = sum
			return
		}
		a.large.SetInt64(int64(a.small))
		a.promoted = true
	}
	a.large.Add(&a.large, a.operand.SetInt64(int64(value)))
}

func (a *scaleAccumulator) sub(value Scale) {
	if !a.promoted {
		if difference, ok := subtractScales(a.small, value); ok {
			a.small = difference
			return
		}
		a.large.SetInt64(int64(a.small))
		a.promoted = true
	}
	a.large.Sub(&a.large, a.operand.SetInt64(int64(value)))
}

func (a *scaleAccumulator) addUint64(value uint64) {
	if value <= uint64(math.MaxInt64) {
		a.add(Scale(value))
		return
	}
	if !a.promoted {
		a.large.SetInt64(int64(a.small))
		a.promoted = true
	}
	a.large.Add(&a.large, a.operand.SetUint64(value))
}

func (a *scaleAccumulator) mulUint64(multiplier uint64) {
	if !a.promoted {
		if product, ok := multiplyScale(a.small, multiplier); ok {
			a.small = product
			return
		}
		a.large.SetInt64(int64(a.small))
		a.promoted = true
	}
	a.large.Mul(&a.large, a.operand.SetUint64(multiplier))
}

func (a *scaleAccumulator) fitCoefficient(coefficient *big.Int) (Scale, error) {
	if a.promoted {
		return fitCoefficientScale(coefficient, &a.large)
	}
	return a.small, nil
}

func (a *scaleAccumulator) representableScale() (Scale, error) {
	if !a.promoted {
		return a.small, nil
	}
	return representableScale(&a.large)
}

func (a *scaleAccumulator) coefficientShift() coefficientScaleShift {
	if !a.promoted {
		return coefficientScaleShift{
			scaleNumerator: a.small >= 0,
			exponent:       scaleMagnitude(a.small),
			exponentFits:   true,
		}
	}

	// coefficientScaleShiftFromBig negates a negative shift in place.
	var shift big.Int
	shift.Set(&a.large)
	return coefficientScaleShiftFromBig(&shift)
}

// fitCoefficientScale moves powers of ten between an owned coefficient and
// its scale when the preferred scale lies outside Scale's range.
func fitCoefficientScale(coefficient *big.Int, scale *big.Int) (Scale, error) {
	if scale.IsInt64() {
		return Scale(scale.Int64()), nil
	}
	if coefficient.Sign() == 0 {
		if scale.Sign() < 0 {
			return Scale(math.MinInt64), nil
		}
		return Scale(math.MaxInt64), nil
	}

	var boundary, shift big.Int
	if scale.Sign() < 0 {
		boundary.SetInt64(math.MinInt64)
		shift.Sub(&boundary, scale)
		if !shift.IsUint64() {
			return 0, ErrRange
		}
		multiplyByPowerOfTen(coefficient, coefficient, shift.Uint64())
		return Scale(math.MinInt64), nil
	}

	boundary.SetInt64(math.MaxInt64)
	shift.Sub(scale, &boundary)
	if !shift.IsUint64() {
		return 0, ErrRange
	}
	discard := shift.Uint64()
	if discard >= uint64(decimalDigitCount(coefficient)) {
		return 0, ErrRange
	}
	var divisor, remainder big.Int
	setPowerOfTen(&divisor, discard)
	coefficient.QuoRem(coefficient, &divisor, &remainder)
	if remainder.Sign() != 0 {
		return 0, ErrRange
	}
	return Scale(math.MaxInt64), nil
}

func representableScale(scale *big.Int) (Scale, error) {
	if !scale.IsInt64() {
		return 0, ErrRange
	}
	return Scale(scale.Int64()), nil
}

type coefficientScaleShift struct {
	scaleNumerator bool
	exponent       uint64
	exponentFits   bool
}

// scaleMagnitude returns |scale| without overflowing at math.MinInt64.
func scaleMagnitude(scale Scale) uint64 {
	if scale >= 0 {
		return uint64(scale)
	}
	return uint64(-(scale + 1)) + 1
}

// coefficientScaleShiftFromBig classifies a caller-owned signed shift and
// reports whether its magnitude fits the exponent type used by math/big.
func coefficientScaleShiftFromBig(shift *big.Int) coefficientScaleShift {
	result := coefficientScaleShift{scaleNumerator: shift.Sign() >= 0}
	if !result.scaleNumerator {
		shift.Neg(shift)
	}
	if shift.IsUint64() {
		result.exponent = shift.Uint64()
		result.exponentFits = true
	}
	return result
}

// compareScaledByPowerOfTen compares |x| with |y|*10^exponent without
// constructing the power when their decimal digit counts determine the result.
// Both operands must be non-zero.
func compareScaledByPowerOfTen(x, y *big.Int, exponent uint64) int {
	if exponent == 0 {
		return x.CmpAbs(y)
	}
	xDigits := uint64(decimalDigitCount(x))
	yDigits := uint64(decimalDigitCount(y))
	if exponent > math.MaxUint64-yDigits {
		return -1
	}
	scaledYDigits := yDigits + exponent
	if comparison := cmp.Compare(xDigits, scaledYDigits); comparison != 0 {
		return comparison
	}

	// Equal digit counts imply exponent is bounded by the already-materialized
	// digits in x, so forming this power cannot dwarf the operands.
	var scaledY big.Int
	multiplyByPowerOfTen(&scaledY, y, exponent)
	return x.CmpAbs(&scaledY)
}

func hasTrailingDecimalZero(coefficient *big.Int) bool {
	if coefficient.Bit(0) == 1 {
		return false
	}
	if coefficient.IsInt64() {
		return coefficient.Int64()%10 == 0
	}
	if coefficient.IsUint64() {
		return coefficient.Uint64()%10 == 0
	}
	var remainder big.Int
	return remainder.Rem(coefficient, bigTen).Sign() == 0
}

// removeTrailingDecimalZeros reduces a non-zero, caller-owned coefficient known
// to be divisible by ten without reducing scale below minimum. Large values use
// cached powers of ten; once the result fits in 64 bits, the remainder is removed
// directly.
func removeTrailingDecimalZeros(coefficient *big.Int, scale, minimum Scale) Scale {
	var quotient, remainder big.Int
	knownDivisible := true
	for scale > minimum {
		if coefficient.IsInt64() {
			value, reducedScale := removeSmallTrailingDecimalZeros(coefficient.Int64(), scale, minimum)
			coefficient.SetInt64(value)
			return reducedScale
		}
		if coefficient.IsUint64() {
			value, reducedScale := removeSmallTrailingDecimalZeros(coefficient.Uint64(), scale, minimum)
			coefficient.SetUint64(value)
			return reducedScale
		}
		limit := min(scaleDistance(scale, minimum), uint64(len(smallPowersOfTen)-1))
		for remove := limit; ; remove /= 2 {
			if remove == 1 && knownDivisible {
				coefficient.Quo(coefficient, bigTen)
				scale--
				break
			}
			quotient.QuoRem(coefficient, &smallPowersOfTen[remove], &remainder)
			if remainder.Sign() == 0 {
				coefficient.Set(&quotient)
				scale -= Scale(remove)
				break
			}
			if remove == 1 {
				return scale
			}
		}
		knownDivisible = false
	}
	return scale
}

func removeSmallTrailingDecimalZeros[T int64 | uint64](coefficient T, scale, minimum Scale) (T, Scale) {
	for coefficient != 0 && scale > minimum && coefficient%10 == 0 {
		coefficient /= 10
		scale--
	}
	return coefficient, scale
}
