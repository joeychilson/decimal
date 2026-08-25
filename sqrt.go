package decimal

import (
	"errors"
	"math"
	"math/big"
)

// Sqrt returns the exact, non-negative square root of d. It returns
// [ErrInvalidOperation] if d is negative and [ErrInexact] unless the root is a
// finite Decimal. Use [Context.Sqrt] for a rounded square root.
func (d Decimal) Sqrt() (Decimal, error) {
	return squareRootExact(d)
}

func squareRootExact(d Decimal) (Decimal, error) {
	coefficient, scale := decimalParts(d)
	if coefficient.Sign() < 0 {
		return Decimal{}, ErrInvalidOperation
	}
	if coefficient.Sign() == 0 {
		rootScale := scale / 2
		if scale > 0 && scale&1 != 0 {
			rootScale++
		}
		return makeDecimal(coefficient, rootScale), nil
	}

	var adjusted big.Int
	adjusted.Set(coefficient)
	if scale&1 != 0 {
		if scale == Scale(math.MaxInt64) {
			var quotient, remainder big.Int
			quotient.QuoRem(&adjusted, bigTen, &remainder)
			if remainder.Sign() != 0 {
				return Decimal{}, ErrInexact
			}
			adjusted.Set(&quotient)
			scale--
		} else {
			adjusted.Mul(&adjusted, bigTen)
			scale++
		}
	}
	var root, square big.Int
	root.Sqrt(&adjusted)
	if square.Mul(&root, &root).Cmp(&adjusted) != 0 {
		return Decimal{}, ErrInexact
	}
	return makeDecimal(&root, scale/2), nil
}

// sqrtToPrecision rounds without a floating-point approximation. After
// scaling x to a rational A/B, it finds floor(sqrt(A/B)) with integer square
// root and compares 4*A with B*(2*r+1)^2 to classify the exact midpoint.
func sqrtToPrecision(x Decimal, precision uint, mode RoundingMode) (Decimal, error) {
	if precision == 0 {
		return squareRootExact(x)
	}

	coefficient, scale := decimalParts(x)
	if coefficient.Sign() < 0 {
		return Decimal{}, ErrInvalidOperation
	}
	if coefficient.Sign() == 0 {
		rootScale := scale / 2
		if scale > 0 && scale&1 != 0 {
			rootScale++
		}
		return makeDecimal(coefficient, rootScale), nil
	}
	if exact, ok := smallExactSquareRoot(coefficient, scale); ok {
		return roundToPrecision(exact, precision, mode)
	}

	targetScale, err := squareRootTargetScale(coefficient, scale, precision)
	if err != nil {
		return squareRootAtRangeLimit(x, precision, mode)
	}

	shift := squareRootScaleShift(targetScale, scale)
	if !shift.exponentFits {
		return squareRootAtRangeLimit(x, precision, mode)
	}

	// Small coefficients were classified above. Check larger exact roots before
	// constructing an uncached power to avoid padding a result that already fits.
	if shift.exponent >= uint64(len(smallPowersOfTen)) && !coefficient.IsUint64() {
		exact, exactErr := squareRootExact(x)
		if exactErr == nil {
			return roundToPrecision(exact, precision, mode)
		}
		if mode == Exact || !errors.Is(exactErr, ErrInexact) {
			return Decimal{}, exactErr
		}
	}

	var numerator, denominatorStorage big.Int
	numerator.Set(coefficient)
	denominator := bigOne
	if shift.scaleNumerator && shift.exponent != 0 {
		multiplyByPowerOfTen(&numerator, &numerator, shift.exponent)
	} else if !shift.scaleNumerator {
		setPowerOfTen(&denominatorStorage, shift.exponent)
		denominator = &denominatorStorage
	}

	var integer, root, square big.Int
	radicand := &numerator
	if denominator != bigOne {
		integer.Quo(&numerator, denominator)
		radicand = &integer
	}
	root.Sqrt(radicand)
	square.Mul(&root, &root)
	if denominator != bigOne {
		square.Mul(&square, denominator)
	}
	exact := square.Cmp(&numerator) == 0
	if !exact {
		if mode == Exact {
			return Decimal{}, ErrInexact
		}
		midpointComparison := 0
		if isHalfRounding(mode) {
			var twiceRootPlusOne, midpointSquare, fourNumerator big.Int
			twiceRootPlusOne.Lsh(&root, 1)
			twiceRootPlusOne.Add(&twiceRootPlusOne, bigOne)
			midpointSquare.Mul(&twiceRootPlusOne, &twiceRootPlusOne)
			if denominator != bigOne {
				midpointSquare.Mul(&midpointSquare, denominator)
			}
			fourNumerator.Lsh(&numerator, 2)
			midpointComparison = fourNumerator.Cmp(&midpointSquare)
		}
		if roundingIncrement(mode, 1, midpointComparison, &root) {
			root.Add(&root, bigOne)
		}
	}

	if exact {
		preferredScale := scale / 2
		if scale > 0 && scale&1 != 0 && scale != Scale(math.MaxInt64) {
			preferredScale++
		}
		if targetScale > preferredScale && hasTrailingDecimalZero(&root) {
			targetScale = removeTrailingDecimalZeros(&root, targetScale, preferredScale)
		}
	}
	return roundCoefficientToPrecision(&root, targetScale, precision, mode)
}

// smallExactSquareRoot uses floating point only for an initial estimate; the
// integer correction and quotient checks prove the returned root is exact.
func smallExactSquareRoot(coefficient *big.Int, scale Scale) (Decimal, bool) {
	if !coefficient.IsUint64() {
		return Decimal{}, false
	}
	value := coefficient.Uint64()
	rootMultiplier := uint64(1)
	if scale&1 != 0 {
		if scale == Scale(math.MaxInt64) {
			if value%10 != 0 {
				return Decimal{}, false
			}
			value /= 10
			scale--
		} else {
			if value <= math.MaxUint64/10 {
				value *= 10
			} else {
				if value%10 != 0 {
					return Decimal{}, false
				}
				value /= 10
				rootMultiplier = 10
			}
			scale++
		}
	}

	root := uint64(math.Sqrt(float64(value)))
	for root > value/root {
		root--
	}
	for next := root + 1; next <= value/next; next++ {
		root = next
	}
	if value%root != 0 || value/root != root {
		return Decimal{}, false
	}
	return New(root*rootMultiplier, scale/2), true
}

func squareRootTargetScale(coefficient *big.Int, scale Scale, precision uint) (Scale, error) {
	digits := uint64(decimalDigitCount(coefficient) - 1)
	if digits <= uint64(math.MaxInt64) && uint64(precision-1) <= uint64(math.MaxInt64) {
		adjusted, ok := subtractScales(Scale(digits), scale)
		if ok {
			rootExponent := adjusted / 2
			if adjusted < 0 && adjusted%2 != 0 {
				rootExponent--
			}
			if target, ok := subtractScales(Scale(precision-1), rootExponent); ok {
				return target, nil
			}
		}
	}

	var adjusted, component, rootExponent, remainder, target big.Int
	adjusted.SetUint64(digits)
	component.SetInt64(int64(scale))
	adjusted.Sub(&adjusted, &component)
	rootExponent.QuoRem(&adjusted, bigTwo, &remainder)
	if remainder.Sign() < 0 {
		rootExponent.Sub(&rootExponent, bigOne)
	}
	target.SetUint64(uint64(precision - 1))
	target.Sub(&target, &rootExponent)
	return representableScale(&target)
}

// squareRootAtRangeLimit preserves exact roots when only the
// precision-derived working scale lies outside the supported range.
func squareRootAtRangeLimit(x Decimal, precision uint, mode RoundingMode) (Decimal, error) {
	exact, err := squareRootExact(x)
	if err == nil {
		return roundToPrecision(exact, precision, mode)
	}
	if mode == Exact || !errors.Is(err, ErrInexact) {
		return Decimal{}, err
	}
	return Decimal{}, ErrRange
}

// squareRootScaleShift determines which side of the radicand must receive a
// power of ten so an integer root has the requested scale.
func squareRootScaleShift(targetScale, scale Scale) coefficientScaleShift {
	var shift scaleAccumulator
	shift.add(targetScale)
	shift.add(targetScale)
	shift.sub(scale)
	return shift.coefficientShift()
}
