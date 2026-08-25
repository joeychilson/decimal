package decimal

import (
	"math"
	"math/big"
	"reflect"
	"strconv"
)

// Float is the set of built-in and user-defined IEEE binary floating-point
// types accepted by generic conversions.
type Float interface {
	~float32 | ~float64
}

// FromFloat returns the Decimal represented by the shortest decimal text that
// round-trips to x. This is normally the desired conversion for human-entered
// or externally formatted floating-point values. It rejects NaN and infinities
// with [ErrInvalidOperation].
//
// FromFloat does not expose the usually surprising exact binary value of x;
// use [FromFloatExact] when that distinction matters.
func FromFloat[T Float](x T) (Decimal, error) {
	f := float64(x)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return Decimal{}, ErrInvalidOperation
	}
	bits := reflect.TypeFor[T]().Bits()
	return Parse(strconv.FormatFloat(f, 'g', -1, bits))
}

// FromFloatExact returns the exact mathematical value of the finite binary
// floating-point number x. For example, the result for float64(0.1) is not the
// Decimal 0.1. It rejects NaN and infinities with [ErrInvalidOperation].
func FromFloatExact[T Float](x T) (Decimal, error) {
	f := float64(x)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return Decimal{}, ErrInvalidOperation
	}
	return fromBigRatExact(new(big.Rat).SetFloat64(f))
}

// FromBigRat converts x exactly. It returns [ErrInexact] unless x has a finite
// base-10 expansion, and it does not retain x. Use [Context.FromBigRat] to
// request a rounded conversion. It returns [ErrRange] if no exact
// representation has a supported scale. FromBigRat panics if x is nil.
func FromBigRat(x *big.Rat) (Decimal, error) {
	if x == nil {
		panic("decimal: nil *big.Rat")
	}
	return fromBigRatExact(x)
}

// BigRat returns a new big.Rat equal to d. The caller may modify the result
// without affecting d.
func (d Decimal) BigRat() *big.Rat {
	coefficient, scale := decimalParts(d)
	if coefficient.Sign() == 0 {
		return new(big.Rat)
	}
	if scale >= 0 {
		denominator := setPowerOfTen(new(big.Int), uint64(scale))
		return new(big.Rat).SetFrac(new(big.Int).Set(coefficient), denominator)
	}
	integer := multiplyByPowerOfTen(new(big.Int), coefficient, scaleMagnitude(scale))
	return new(big.Rat).SetInt(integer)
}

// BigInt returns d as an integer. It returns [ErrInexact] if d has a non-zero
// fractional part. The caller may modify the result without affecting d.
func (d Decimal) BigInt() (*big.Int, error) {
	return integerCoefficient(d)
}

// Int converts d exactly to T. It returns [ErrInexact] if d has a non-zero
// fractional part and [ErrRange] if the integer does not fit in T.
//
// This generic method is useful with named application types:
//
//	type Cents int64
//	cents, err := amount.Rescale(0, decimal.Exact)
//	n, err := cents.Int[Cents]()
func (d Decimal) Int[T Integer]() (T, error) {
	var zero T
	integer, err := integerCoefficient(d)
	if err != nil {
		return zero, err
	}

	// The all-ones value is negative only for signed integer types.
	if ^T(0) < T(0) {
		if !integer.IsInt64() {
			return zero, ErrRange
		}
		value := integer.Int64()
		converted := T(value)
		if int64(converted) != value {
			return zero, ErrRange
		}
		return converted, nil
	}
	if !integer.IsUint64() {
		return zero, ErrRange
	}
	value := integer.Uint64()
	converted := T(value)
	if uint64(converted) != value {
		return zero, ErrRange
	}
	return converted, nil
}

// Int64 converts d exactly to int64. It returns [ErrInexact] for a non-integer
// and [ErrRange] when d does not fit in int64.
func (d Decimal) Int64() (int64, error) {
	return d.Int[int64]()
}

// Uint64 converts d exactly to uint64. It returns [ErrInexact] for a
// non-integer and [ErrRange] when d is negative or does not fit in uint64.
func (d Decimal) Uint64() (uint64, error) {
	return d.Int[uint64]()
}

// Float converts d to T using IEEE round-to-nearest, ties-to-even. Exact reports
// whether the conversion preserved d exactly. A finite d may convert to an
// infinity when it is outside T's finite range; in that case exact is false.
func (d Decimal) Float[T Float]() (value T, exact bool) {
	bits := reflect.TypeFor[T]().Bits()
	maximumExponent := int64(308)
	zeroBelowExponent := int64(-324)
	if bits == 32 {
		maximumExponent = 38
		zeroBelowExponent = -46
	}
	coefficient, scale := decimalParts(d)
	if coefficient.Sign() == 0 {
		return 0, true
	}
	exponent, ok := subtractScales(Scale(decimalDigitCount(coefficient)-1), scale)
	if !ok {
		if scale < 0 {
			return T(math.Inf(coefficient.Sign())), false
		}
		return T(math.Copysign(0, float64(coefficient.Sign()))), false
	}
	if exponent > Scale(maximumExponent) {
		return T(math.Inf(coefficient.Sign())), false
	}
	if exponent < Scale(zeroBelowExponent) {
		return T(math.Copysign(0, float64(coefficient.Sign()))), false
	}

	var rational big.Rat
	if scale >= 0 {
		var denominator big.Int
		setPowerOfTen(&denominator, uint64(scale))
		rational.SetFrac(coefficient, &denominator)
	} else {
		var integer big.Int
		multiplyByPowerOfTen(&integer, coefficient, scaleMagnitude(scale))
		rational.SetInt(&integer)
	}
	if bits == 32 {
		v, exact := rational.Float32()
		return T(v), exact
	}
	v, ok := rational.Float64()
	return T(v), ok
}

// Float64 converts d to float64 using IEEE round-to-nearest, ties-to-even.
// Exact reports whether the conversion preserved d exactly. A finite d may
// convert to an infinity, in which case exact is false.
func (d Decimal) Float64() (value float64, exact bool) {
	return d.Float[float64]()
}

func fromBigRatExact(x *big.Rat) (Decimal, error) {
	result, err := divideExact(NewBig(x.Num(), 0), NewBig(x.Denom(), 0))
	return result.Canonical(), err
}

func integerCoefficient(d Decimal) (*big.Int, error) {
	coefficient, scale := decimalParts(d)
	if coefficient.Sign() == 0 {
		return new(big.Int), nil
	}
	if scale <= 0 {
		return multiplyByPowerOfTen(new(big.Int), coefficient, scaleMagnitude(scale)), nil
	}
	if uint64(scale) >= uint64(decimalDigitCount(coefficient)) {
		return nil, ErrInexact
	}
	quotient := new(big.Int)
	remainder := new(big.Int)
	divisor := setPowerOfTen(new(big.Int), uint64(scale))
	quotient.QuoRem(coefficient, divisor, remainder)
	if remainder.Sign() != 0 {
		return nil, ErrInexact
	}
	return quotient, nil
}
