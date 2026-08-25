package decimal

import (
	"math"
	"math/big"
	"math/bits"
)

// Pow returns d raised to the integer power n exactly. Negative powers are
// permitted. Pow returns [ErrInexact] when the exact result has no finite
// decimal representation and [ErrDivisionByZero] for zero raised to a negative
// power. It returns [ErrRange] if the result's scale cannot be represented. Use
// [Context.Pow] for rounded powers.
func (d Decimal) Pow(n int64) (Decimal, error) {
	return powToPrecision(d, n, 0, HalfEven)
}

func powToPrecision(d Decimal, n int64, precision uint, mode RoundingMode) (Decimal, error) {
	if n >= 0 {
		if precision == 0 {
			coefficient, scale, err := powPositiveParts(d, uint64(n))
			if err != nil {
				return Decimal{}, err
			}
			return makeDecimal(coefficient, scale), nil
		}
		coefficient, scale, wideScale := powPositivePartsAtScale(d, uint64(n))
		if wideScale == nil {
			return roundCoefficientToPrecision(coefficient, scale, precision, mode)
		}
		var workingScale scaleAccumulator
		workingScale.set(wideScale)
		return roundCoefficientToPrecisionAtScale(coefficient, &workingScale, precision, mode)
	}
	if d.IsZero() {
		return Decimal{}, ErrDivisionByZero
	}
	magnitude := uint64(-(n + 1)) + 1
	coefficient, resultScale, err := powPositiveParts(d, magnitude)
	if err != nil {
		return Decimal{}, err
	}
	return divideToPrecision(FromInt(1), makeDecimal(coefficient, resultScale), precision, mode)
}

// powPositiveParts returns a caller-owned coefficient and exact fitted scale
// for d raised to exponent.
func powPositiveParts(d Decimal, exponent uint64) (*big.Int, Scale, error) {
	coefficient, scale := decimalParts(d)
	var power big.Int
	power.SetUint64(exponent)
	result := new(big.Int).Exp(coefficient, &power, nil)
	if resultScale, ok := multiplyScale(scale, exponent); ok {
		return result, resultScale, nil
	}

	var resultScale, multiplier big.Int
	resultScale.SetInt64(int64(scale))
	resultScale.Mul(&resultScale, multiplier.SetUint64(exponent))
	fitted, err := fitCoefficientScale(result, &resultScale)
	return result, fitted, err
}

// powPositivePartsAtScale leaves an overflowed preferred scale unfitted so a
// finite-precision context can round before enforcing Scale's range.
func powPositivePartsAtScale(d Decimal, exponent uint64) (*big.Int, Scale, *big.Int) {
	coefficient, scale := decimalParts(d)
	var power big.Int
	power.SetUint64(exponent)
	result := new(big.Int).Exp(coefficient, &power, nil)
	if resultScale, ok := multiplyScale(scale, exponent); ok {
		return result, resultScale, nil
	}

	resultScale := new(big.Int).SetInt64(int64(scale))
	var multiplier big.Int
	resultScale.Mul(resultScale, multiplier.SetUint64(exponent))
	return result, 0, resultScale
}

// multiplyScale returns scale*multiplier and reports whether the result fits
// Scale. On overflow, it returns zero and false.
func multiplyScale(scale Scale, multiplier uint64) (Scale, bool) {
	high, product := bits.Mul64(scaleMagnitude(scale), multiplier)
	limit := uint64(math.MaxInt64)
	if scale < 0 {
		limit++
	}
	if high != 0 || product > limit {
		return 0, false
	}
	if scale >= 0 {
		return Scale(product), true
	}
	if product == uint64(math.MaxInt64)+1 {
		return Scale(math.MinInt64), true
	}
	return -Scale(product), true
}
