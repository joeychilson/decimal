package decimal

import (
	"errors"
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
		return powPositiveToPrecision(d, uint64(n), precision, mode)
	}
	if d.IsZero() {
		return Decimal{}, ErrDivisionByZero
	}
	magnitude := uint64(-(n + 1)) + 1
	inverse, err := FromInt(1).Div(d)
	if err == nil {
		if precision != 0 {
			return powPositiveToPrecision(inverse, magnitude, precision, mode)
		}
		coefficient, resultScale, powerErr := powPositiveParts(inverse, magnitude)
		if powerErr != nil {
			return Decimal{}, powerErr
		}
		return makeDecimal(coefficient, resultScale), nil
	}
	if precision == 0 || mode == Exact || !errors.Is(err, ErrInexact) {
		return Decimal{}, err
	}
	return powNegativeToPrecision(d, magnitude, precision, mode)
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

func powPositiveToPrecision(d Decimal, exponent uint64, precision uint, mode RoundingMode) (Decimal, error) {
	coefficient, scale := decimalParts(d)
	if exponent == 0 {
		return FromInt(1), nil
	}
	if coefficient.Sign() == 0 {
		var resultScale, multiplier big.Int
		resultScale.SetInt64(int64(scale))
		resultScale.Mul(&resultScale, multiplier.SetUint64(exponent))
		fitted, err := fitCoefficientScale(new(big.Int), &resultScale)
		if err != nil {
			return Decimal{}, err
		}
		return New(0, fitted), nil
	}

	sign := 1
	if coefficient.Sign() < 0 && exponent&1 != 0 {
		sign = -1
	}
	magnitude := new(big.Int).Abs(coefficient)
	text := magnitude.Text(10)
	baseTrailingZeros := 0
	for baseTrailingZeros < len(text)-1 && text[len(text)-1-baseTrailingZeros] == '0' {
		baseTrailingZeros++
	}
	lastNonzeroDigit := int(text[len(text)-1-baseTrailingZeros] - '0')

	maximumInt := ^uint(0) >> 1
	guardDigits := uint(max(16, 2*bits.Len64(exponent)))
	for {
		if precision > maximumInt-guardDigits {
			return Decimal{}, ErrRange
		}
		interval := powCoefficientInterval(magnitude, exponent, int(precision+guardDigits))
		result, remove, resolved, err := roundPowerInterval(interval, exponent, precision, sign, mode, baseTrailingZeros, lastNonzeroDigit)
		if err != nil {
			return Decimal{}, err
		}
		if resolved {
			return makeRoundedPower(result, scale, exponent, remove, precision)
		}
		if guardDigits > maximumInt/2 {
			return Decimal{}, ErrRange
		}
		guardDigits *= 2
	}
}

func powNegativeToPrecision(d Decimal, exponent uint64, precision uint, mode RoundingMode) (Decimal, error) {
	coefficient, scale := decimalParts(d)
	sign := 1
	if coefficient.Sign() < 0 && exponent&1 != 0 {
		sign = -1
	}
	magnitude := new(big.Int).Abs(coefficient)

	maximumInt := ^uint(0) >> 1
	guardDigits := uint(max(16, 2*bits.Len64(exponent)))
	for {
		if precision > maximumInt-guardDigits {
			return Decimal{}, ErrRange
		}
		interval := powCoefficientInterval(magnitude, exponent, int(precision+guardDigits))
		result, resultScale, resolved := roundNegativePowerInterval(interval, scale, exponent, precision, sign, mode)
		if resolved {
			return makeRoundedPowerResult(result, &resultScale, precision)
		}
		if guardDigits > maximumInt/2 {
			return Decimal{}, ErrRange
		}
		guardDigits *= 2
	}
}

// powCoefficientInterval raises coefficient while retaining only enough
// leading digits to bound the exact result.
func powCoefficientInterval(coefficient *big.Int, exponent uint64, digits int) *powerInterval {
	result := &powerInterval{}
	result.lower.SetInt64(1)
	result.upper.SetInt64(1)
	factor := &powerInterval{}
	factor.lower.Set(coefficient)
	factor.upper.Set(coefficient)
	factor.truncate(digits)
	resultScratch := &powerInterval{}
	factorScratch := &powerInterval{}
	for exponent > 0 {
		if exponent&1 != 0 {
			resultScratch.multiply(result, factor, digits)
			result, resultScratch = resultScratch, result
		}
		exponent >>= 1
		if exponent != 0 {
			factorScratch.multiply(factor, factor, digits)
			factor, factorScratch = factorScratch, factor
		}
	}
	return result
}

// A powerInterval represents every value between lower*10^exponent and
// upper*10^exponent, inclusive.
type powerInterval struct {
	lower, upper big.Int
	exponent     big.Int
}

func (z *powerInterval) multiply(x, y *powerInterval, digits int) {
	z.lower.Mul(&x.lower, &y.lower)
	z.upper.Mul(&x.upper, &y.upper)
	z.exponent.Add(&x.exponent, &y.exponent)
	z.truncate(digits)
}

func (z *powerInterval) truncate(digits int) {
	remove := max(decimalDigitCount(&z.lower), decimalDigitCount(&z.upper)) - digits
	if remove <= 0 {
		return
	}
	var divisor, remainder big.Int
	setPowerOfTen(&divisor, uint64(remove))
	z.lower.Quo(&z.lower, &divisor)
	z.upper.QuoRem(&z.upper, &divisor, &remainder)
	if remainder.Sign() != 0 {
		z.upper.Add(&z.upper, bigOne)
	}
	z.exponent.Add(&z.exponent, remainder.SetUint64(uint64(remove)))
}

func roundPowerInterval(interval *powerInterval, exponent uint64, precision uint, sign int, mode RoundingMode, baseTrailingZeros, lastNonzeroDigit int) (big.Int, *big.Int, bool, error) {
	lowerDigits := decimalDigitCount(&interval.lower)
	if lowerDigits != decimalDigitCount(&interval.upper) {
		return big.Int{}, nil, false, nil
	}
	var digits, precisionValue big.Int
	digits.Set(&interval.exponent)
	digits.Add(&digits, precisionValue.SetUint64(uint64(lowerDigits)))
	remove := new(big.Int).Sub(&digits, precisionValue.SetUint64(uint64(precision)))
	if remove.Sign() < 0 {
		remove.SetInt64(0)
	}

	var trailingZeros, trailingFactor big.Int
	trailingZeros.SetUint64(uint64(baseTrailingZeros))
	trailingZeros.Mul(&trailingZeros, trailingFactor.SetUint64(exponent))
	exactAtTarget := trailingZeros.Cmp(remove) >= 0
	if mode == Exact && !exactAtTarget {
		return big.Int{}, nil, false, ErrInexact
	}
	if exactAtTarget {
		lower := roundPowerBound(&interval.lower, &interval.exponent, remove, 1, Ceiling)
		upper := roundPowerBound(&interval.upper, &interval.exponent, remove, 1, Floor)
		if lower.Cmp(&upper) != 0 {
			return big.Int{}, nil, false, nil
		}
		if sign < 0 {
			lower.Neg(&lower)
		}
		return lower, remove, true, nil
	}

	var beforeRemove big.Int
	beforeRemove.Sub(remove, bigOne)
	if trailingZeros.Cmp(&beforeRemove) == 0 && powerLastDigit(lastNonzeroDigit, exponent) == 5 {
		lower := roundPowerBound(&interval.lower, &interval.exponent, remove, 1, TowardZero)
		upper := roundPowerBound(&interval.upper, &interval.exponent, remove, 1, TowardZero)
		if lower.Cmp(&upper) != 0 {
			return big.Int{}, nil, false, nil
		}
		if sign < 0 {
			lower.Neg(&lower)
		}
		if roundingIncrement(mode, sign, 0, &lower) {
			if sign > 0 {
				lower.Add(&lower, bigOne)
			} else {
				lower.Sub(&lower, bigOne)
			}
		}
		return lower, remove, true, nil
	}

	lowerMagnitude := &interval.lower
	upperMagnitude := &interval.upper
	if sign < 0 {
		lowerMagnitude, upperMagnitude = upperMagnitude, lowerMagnitude
	}
	lower := roundPowerBound(lowerMagnitude, &interval.exponent, remove, sign, mode)
	upper := roundPowerBound(upperMagnitude, &interval.exponent, remove, sign, mode)
	if lower.Cmp(&upper) != 0 {
		return big.Int{}, nil, false, nil
	}
	return lower, remove, true, nil
}

// If the positive coefficient power lies in [lower*10^e, upper*10^e],
// its reciprocal lies between 10^-e/upper and 10^-e/lower. Choosing the
// significant-digit scale before dividing cancels both e and the wide base
// scale, leaving endpoint divisions bounded by the retained interval digits.
func roundNegativePowerInterval(interval *powerInterval, scale Scale, exponent uint64, precision uint, sign int, mode RoundingMode) (big.Int, big.Int, bool) {
	lowerDigits := decimalDigitCount(&interval.lower)
	if lowerDigits != decimalDigitCount(&interval.upper) {
		return big.Int{}, big.Int{}, false
	}

	shift := uint64(lowerDigits) + uint64(precision) - 1
	lowerDenominator := &interval.upper
	upperDenominator := &interval.lower
	if sign < 0 {
		lowerDenominator, upperDenominator = upperDenominator, lowerDenominator
	}
	lower := roundReciprocalPowerBound(lowerDenominator, shift, sign, mode)
	upper := roundReciprocalPowerBound(upperDenominator, shift, sign, mode)
	if lower.Cmp(&upper) != 0 {
		return big.Int{}, big.Int{}, false
	}

	var resultScale, component, scaleProduct big.Int
	resultScale.Set(&interval.exponent)
	resultScale.Add(&resultScale, component.SetInt64(int64(lowerDigits)))
	resultScale.Add(&resultScale, component.SetUint64(uint64(precision-1)))
	scaleProduct.SetInt64(int64(scale))
	scaleProduct.Mul(&scaleProduct, component.SetUint64(exponent))
	resultScale.Sub(&resultScale, &scaleProduct)
	return lower, resultScale, true
}

func roundReciprocalPowerBound(denominator *big.Int, shift uint64, sign int, mode RoundingMode) big.Int {
	var numerator, quotient, remainder big.Int
	setPowerOfTen(&numerator, shift)
	quotient.QuoRem(&numerator, denominator, &remainder)
	if sign < 0 {
		quotient.Neg(&quotient)
		remainder.Neg(&remainder)
	}
	if err := roundQuotient(&quotient, &remainder, denominator, mode); err != nil {
		panic("decimal: exact mode reached bounded reciprocal power rounding")
	}
	return quotient
}

func roundPowerBound(bound, exponent, remove *big.Int, sign int, mode RoundingMode) big.Int {
	var quotient, remainder, divisor, shift big.Int
	comparison := exponent.Cmp(remove)
	if comparison >= 0 {
		shift.Sub(exponent, remove)
	} else {
		shift.Sub(remove, exponent)
	}
	if !shift.IsUint64() {
		panic("decimal: bounded power shift does not fit uint64")
	}
	if comparison >= 0 {
		multiplyByPowerOfTen(&quotient, bound, shift.Uint64())
		divisor.SetInt64(1)
	} else {
		setPowerOfTen(&divisor, shift.Uint64())
		quotient.QuoRem(bound, &divisor, &remainder)
	}
	if sign < 0 {
		quotient.Neg(&quotient)
		remainder.Neg(&remainder)
	}
	if err := roundQuotient(&quotient, &remainder, &divisor, mode); err != nil {
		panic("decimal: exact mode reached bounded power rounding")
	}
	return quotient
}

func powerLastDigit(digit int, exponent uint64) int {
	result := 1
	for exponent > 0 {
		if exponent&1 != 0 {
			result = result * digit % 10
		}
		digit = digit * digit % 10
		exponent >>= 1
	}
	return result
}

func makeRoundedPower(coefficient big.Int, scale Scale, exponent uint64, remove *big.Int, precision uint) (Decimal, error) {
	var resultScale, multiplier big.Int
	resultScale.SetInt64(int64(scale))
	resultScale.Mul(&resultScale, multiplier.SetUint64(exponent))
	resultScale.Sub(&resultScale, remove)
	return makeRoundedPowerResult(coefficient, &resultScale, precision)
}

func makeRoundedPowerResult(coefficient big.Int, resultScale *big.Int, precision uint) (Decimal, error) {
	digits := uint(decimalDigitCount(&coefficient))
	if digits > precision {
		var remainder big.Int
		coefficient.QuoRem(&coefficient, bigTen, &remainder)
		if remainder.Sign() != 0 {
			panic("decimal: bounded power normalization was inexact")
		}
		resultScale.Sub(resultScale, bigOne)
		digits--
	}
	if digits > precision {
		panic("decimal: bounded power result exceeds requested precision")
	}

	var minimumScale, shift big.Int
	minimumScale.SetInt64(math.MinInt64)
	if resultScale.Cmp(&minimumScale) < 0 {
		shift.Sub(&minimumScale, resultScale)
		if !shift.IsUint64() || shift.Uint64() > uint64(precision-digits) {
			return Decimal{}, ErrRange
		}
		multiplyByPowerOfTen(&coefficient, &coefficient, shift.Uint64())
		resultScale.Set(&minimumScale)
	}
	var accumulatedScale scaleAccumulator
	accumulatedScale.set(resultScale)
	target, err := accumulatedScale.fitRoundedCoefficient(&coefficient)
	if err != nil {
		return Decimal{}, err
	}
	return makeDecimal(&coefficient, target), nil
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
