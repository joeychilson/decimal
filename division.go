package decimal

import (
	"errors"
	"math"
	"math/big"
	"math/bits"
)

// Div returns d/x exactly. It returns [ErrDivisionByZero] if x is zero and
// [ErrInexact] if the quotient has no finite base-10 representation. Use
// [Context.Div] or [Decimal.DivScale] when rounding is acceptable.
//
// The result's preferred scale is d.Scale()-x.Scale(). Its scale is increased
// only as far as needed to represent the exact quotient. Thus 1.00/2 is 0.50,
// while 1/2 is 0.5. If the preferred scale is outside the range of [Scale],
// the representation is adjusted only as far as needed to preserve an exact
// result, or Div returns [ErrRange] if none exists.
func (d Decimal) Div(x Decimal) (Decimal, error) {
	return divideExact(d, x)
}

// DivScale returns d/x represented at exactly scale, using mode if digits must
// be discarded. It returns [ErrDivisionByZero] if x is zero. With Exact mode,
// it returns [ErrInexact] rather than changing the value. DivScale may also
// return [ErrInvalidRoundingMode] or [ErrRange].
func (d Decimal) DivScale(x Decimal, scale Scale, mode RoundingMode) (Decimal, error) {
	if mode > Exact {
		return Decimal{}, ErrInvalidRoundingMode
	}
	xCoefficient, xScale := decimalParts(x)
	if xCoefficient.Sign() == 0 {
		return Decimal{}, ErrDivisionByZero
	}
	coefficient, currentScale := decimalParts(d)
	if coefficient.Sign() == 0 {
		return makeDecimal(coefficient, scale), nil
	}
	var quotient big.Int
	_, err := divideCoefficientAtScale(
		&quotient,
		scaledCoefficient{coefficient: coefficient, scale: currentScale},
		scaledCoefficient{coefficient: xCoefficient, scale: xScale},
		scale,
		mode,
	)
	if err != nil {
		return Decimal{}, err
	}
	return makeDecimal(&quotient, scale), nil
}

// QuoRem returns the integral quotient q, truncated toward zero, and remainder
// r for which d = q*x+r and |r| < |x|. The quotient has scale zero, and the
// remainder has the finer (larger) scale of d and x. QuoRem returns
// [ErrDivisionByZero] if x is zero.
func (d Decimal) QuoRem(x Decimal) (q, r Decimal, err error) {
	return quoRem(d, x)
}

// Rem returns the remainder from [Decimal.QuoRem]. The sign of a non-zero
// remainder is the sign of d. Rem returns [ErrDivisionByZero] if x is zero.
func (d Decimal) Rem(x Decimal) (Decimal, error) {
	_, remainder, err := quoRem(d, x)
	return remainder, err
}

func quoRem(d, x Decimal) (q, r Decimal, err error) {
	xCoefficient, xScale := decimalParts(x)
	if xCoefficient.Sign() == 0 {
		return Decimal{}, Decimal{}, ErrDivisionByZero
	}
	dCoefficient, dScale := decimalParts(d)
	if dCoefficient.Sign() == 0 {
		return Decimal{}, makeDecimal(new(big.Int), max(dScale, xScale)), nil
	}
	if dScale > xScale {
		shift := scaleDistance(dScale, xScale)
		if compareScaledByPowerOfTen(dCoefficient, xCoefficient, shift) < 0 {
			return Decimal{}, d, nil
		}
	}
	var aligned, quotient, remainder big.Int
	dCoefficient, xCoefficient, scale := alignedParts(
		&aligned,
		scaledCoefficient{coefficient: dCoefficient, scale: dScale},
		scaledCoefficient{coefficient: xCoefficient, scale: xScale},
	)
	quotient.QuoRem(dCoefficient, xCoefficient, &remainder)
	return makeDecimal(&quotient, 0), makeDecimal(&remainder, scale), nil
}

// divideToPrecision divides directly at the scale needed for precision. An
// exact quotient discards only zeros beyond the operands' preferred quotient
// scale; unlimited precision remains on the exact factorization path.
func divideToPrecision(x, y Decimal, precision uint, mode RoundingMode) (Decimal, error) {
	if precision == 0 {
		return divideExact(x, y)
	}
	divisor, divisorScale := decimalParts(y)
	if divisor.Sign() == 0 {
		return Decimal{}, ErrDivisionByZero
	}

	dividend, dividendScale := decimalParts(x)
	preferredScale, ok := subtractScales(dividendScale, divisorScale)
	if !ok {
		if divisorScale > 0 {
			preferredScale = Scale(math.MinInt64)
		} else {
			preferredScale = Scale(math.MaxInt64)
		}
	}
	if dividend.Sign() == 0 {
		return makeDecimal(dividend, preferredScale), nil
	}

	ratioExponent := int64(decimalDigitCount(dividend)) - int64(decimalDigitCount(divisor))
	var scaled big.Int
	if ratioExponent >= 0 {
		multiplyByPowerOfTen(&scaled, divisor, uint64(ratioExponent))
		if dividend.CmpAbs(&scaled) < 0 {
			ratioExponent--
		}
	} else {
		multiplyByPowerOfTen(&scaled, dividend, uint64(-ratioExponent))
		if scaled.CmpAbs(divisor) < 0 {
			ratioExponent--
		}
	}

	targetScale, err := divisionTargetScale(dividendScale, divisorScale, ratioExponent, precision)
	if err != nil {
		exact, exactErr := divideExact(x, y)
		if exactErr == nil {
			return roundToPrecision(exact, precision, mode)
		}
		if mode == Exact || !errors.Is(exactErr, ErrInexact) {
			return Decimal{}, exactErr
		}
		return Decimal{}, err
	}
	if digits, terminating := decimalPrimeFactorDigits(divisor); terminating {
		if exactScale, ok := addScales(preferredScale, Scale(digits)); ok && exactScale < targetScale {
			targetScale = exactScale
		}
	}
	var quotient big.Int
	exact, err := divideCoefficientAtScale(
		&quotient,
		scaledCoefficient{coefficient: dividend, scale: dividendScale},
		scaledCoefficient{coefficient: divisor, scale: divisorScale},
		targetScale,
		mode,
	)
	if err != nil {
		return Decimal{}, err
	}
	if exact && targetScale > preferredScale && hasTrailingDecimalZero(&quotient) {
		targetScale = removeTrailingDecimalZeros(&quotient, targetScale, preferredScale)
	}
	return roundCoefficientToPrecision(&quotient, targetScale, precision, mode)
}

// decimalPrimeFactorDigits reports the number of decimal places sufficient to
// divide by a non-zero machine-word integer whose prime factors are limited to 2 and 5.
func decimalPrimeFactorDigits(x *big.Int) (uint64, bool) {
	if x.BitLen() > 64 {
		return 0, false
	}
	remaining := x.Uint64()
	twos := uint64(bits.TrailingZeros64(remaining))
	remaining >>= twos
	var fives uint64
	for remaining%5 == 0 {
		remaining /= 5
		fives++
	}
	if remaining != 1 {
		return 0, false
	}
	return max(twos, fives), true
}

// divideExact reduces the coefficient ratio and accepts it only when the
// remaining denominator contains no primes other than 2 and 5.
func divideExact(x, y Decimal) (Decimal, error) {
	yCoefficient, yScale := decimalParts(y)
	if yCoefficient.Sign() == 0 {
		return Decimal{}, ErrDivisionByZero
	}
	xCoefficient, xScale := decimalParts(x)
	var scale scaleAccumulator
	scale.add(xScale)
	scale.sub(yScale)

	var numerator big.Int
	numerator.Set(xCoefficient)
	if numerator.Sign() == 0 {
		resultScale, err := scale.fitCoefficient(&numerator)
		if err != nil {
			return Decimal{}, err
		}
		return makeDecimal(&numerator, resultScale), nil
	}

	var denominator big.Int
	denominator.Set(yCoefficient)
	if denominator.Sign() < 0 {
		numerator.Neg(&numerator)
		denominator.Neg(&denominator)
	}

	var absoluteNumerator, gcd big.Int
	absoluteNumerator.Abs(&numerator)
	gcd.GCD(nil, nil, &absoluteNumerator, &denominator)
	numerator.Quo(&numerator, &gcd)
	denominator.Quo(&denominator, &gcd)

	twos := uint64(denominator.TrailingZeroBits())
	denominator.Rsh(&denominator, uint(twos))
	var fives uint64
	var quotient, remainder big.Int
	for {
		quotient.QuoRem(&denominator, bigFive, &remainder)
		if remainder.Sign() != 0 {
			break
		}
		denominator.Set(&quotient)
		fives++
	}
	if denominator.Cmp(bigOne) != 0 {
		return Decimal{}, ErrInexact
	}

	digits := max(twos, fives)
	if twos < digits {
		numerator.Lsh(&numerator, uint(digits-twos))
	}
	if fives < digits {
		var exponent, factor big.Int
		exponent.SetUint64(digits - fives)
		factor.Exp(bigFive, &exponent, nil)
		numerator.Mul(&numerator, &factor)
	}
	scale.addUint64(digits)
	resultScale, err := scale.fitCoefficient(&numerator)
	if err != nil {
		return Decimal{}, err
	}
	return makeDecimal(&numerator, resultScale), nil
}

// divisionTargetScale computes dividendScale-divisorScale-ratioExponent+
// precision-1 and returns ErrRange when that scale is not representable.
func divisionTargetScale(dividendScale, divisorScale Scale, ratioExponent int64, precision uint) (Scale, error) {
	var target scaleAccumulator
	target.add(dividendScale)
	target.sub(divisorScale)
	target.sub(Scale(ratioExponent))
	target.addUint64(uint64(precision - 1))
	return target.representableScale()
}

// divideCoefficientAtScale divides borrowed non-zero coefficient pairs at
// exactly scale, stores the quotient in z, and reports whether it was exact.
func divideCoefficientAtScale(z *big.Int, dividend, divisor scaledCoefficient, scale Scale, mode RoundingMode) (bool, error) {
	// Determine which side of the coefficient ratio must be multiplied by a
	// power of ten to produce the requested quotient scale.
	var scaleShift scaleAccumulator
	scaleShift.add(scale)
	scaleShift.add(divisor.scale)
	scaleShift.sub(dividend.scale)
	shift := scaleShift.coefficientShift()
	numerator := dividend.coefficient
	denominator := divisor.coefficient
	var numeratorStorage, denominatorStorage big.Int

	// Materialize the scale adjustment only on the side that needs it. An
	// unrepresentably large denominator shift proves a sub-quantum quotient.
	if shift.scaleNumerator {
		if !shift.exponentFits {
			return false, ErrRange
		}
		if shift.exponent != 0 {
			multiplyByPowerOfTen(&numeratorStorage, numerator, shift.exponent)
			numerator = &numeratorStorage
		}
	} else {
		comparison := -1
		if shift.exponentFits {
			comparison = compareScaledByPowerOfTen(numerator, denominator, shift.exponent)
		}
		if comparison < 0 {
			if mode == Exact {
				return false, ErrInexact
			}
			midpointComparison := -1
			if shift.exponentFits && isHalfRounding(mode) {
				var twiceNumerator big.Int
				twiceNumerator.Lsh(numerator, 1)
				midpointComparison = compareScaledByPowerOfTen(&twiceNumerator, denominator, shift.exponent)
			}
			sign := numerator.Sign() * denominator.Sign()
			z.SetInt64(0)
			if roundingIncrement(mode, sign, midpointComparison, z) {
				z.SetInt64(int64(sign))
			}
			return false, nil
		}
		if comparison == 0 {
			z.SetInt64(int64(numerator.Sign() * denominator.Sign()))
			return true, nil
		}
		multiplyByPowerOfTen(&denominatorStorage, denominator, shift.exponent)
		denominator = &denominatorStorage
	}

	// At this point both coefficients express the quotient at the target scale.
	var remainder big.Int
	z.QuoRem(numerator, denominator, &remainder)
	exact := remainder.Sign() == 0
	if err := roundQuotient(z, &remainder, denominator, mode); err != nil {
		return false, err
	}
	return exact, nil
}
