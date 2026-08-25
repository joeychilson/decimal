package decimal

import (
	"cmp"
	"math"
	"math/big"
	"strconv"
)

// RoundingMode specifies how a discarded, non-zero portion affects a retained
// result. Operations return [ErrInvalidRoundingMode] for an unknown mode.
type RoundingMode uint8

const (
	// HalfEven rounds to the nearest value and resolves a tie toward an even
	// final digit. It is the zero value and the recommended general-purpose mode.
	HalfEven RoundingMode = iota

	// HalfUp rounds to the nearest value and resolves a tie away from zero.
	HalfUp

	// HalfDown rounds to the nearest value and resolves a tie toward zero.
	HalfDown

	// TowardZero rounds toward zero. It is also known as truncation.
	TowardZero

	// AwayFromZero rounds away from zero whenever any non-zero digits are
	// discarded.
	AwayFromZero

	// Floor rounds toward negative infinity.
	Floor

	// Ceiling rounds toward positive infinity.
	Ceiling

	// ZeroFiveUp rounds away from zero when the last retained digit is zero or
	// five, and toward zero otherwise.
	ZeroFiveUp

	// Exact rejects any operation that would discard non-zero digits, returning
	// an error that wraps [ErrInexact].
	Exact
)

// String returns the conventional name of m. For an unknown value, it returns
// "RoundingMode(n)" with n in base 10.
func (m RoundingMode) String() string {
	switch m {
	case HalfEven:
		return "HalfEven"
	case HalfUp:
		return "HalfUp"
	case HalfDown:
		return "HalfDown"
	case TowardZero:
		return "TowardZero"
	case AwayFromZero:
		return "AwayFromZero"
	case Floor:
		return "Floor"
	case Ceiling:
		return "Ceiling"
	case ZeroFiveUp:
		return "ZeroFiveUp"
	case Exact:
		return "Exact"
	default:
		return "RoundingMode(" + strconv.FormatUint(uint64(m), 10) + ")"
	}
}

// Rescale returns the same mathematical value represented at exactly scale.
// If reducing the scale requires discarding digits, it rounds using mode. With
// Exact mode, Rescale returns [ErrInexact] instead of changing the value. It
// returns [ErrInvalidRoundingMode] for an unknown mode.
func (d Decimal) Rescale(scale Scale, mode RoundingMode) (Decimal, error) {
	if mode > Exact {
		return Decimal{}, ErrInvalidRoundingMode
	}
	return rescale(d, scale, mode)
}

// Round returns d rounded to at most precision significant digits using mode.
// A precision of zero leaves d unchanged. With Exact mode, Round returns
// [ErrInexact] instead of changing the value. Round may also return
// [ErrInvalidRoundingMode] or [ErrRange].
func (d Decimal) Round(precision uint, mode RoundingMode) (Decimal, error) {
	if mode > Exact {
		return Decimal{}, ErrInvalidRoundingMode
	}
	if precision == 0 {
		return d, nil
	}
	coefficient, scale := decimalParts(d)
	digits := uint(decimalDigitCount(coefficient))
	if digits <= precision {
		return d, nil
	}
	return roundCoefficient(new(big.Int).Set(coefficient), scale, digits, precision, mode)
}

// Trunc returns d rounded toward zero at scale zero.
func (d Decimal) Trunc() Decimal {
	result, err := rescale(d, 0, TowardZero)
	if err != nil {
		panic(err)
	}
	return result
}

// Floor returns the greatest scale-zero Decimal less than or equal to d.
func (d Decimal) Floor() Decimal {
	result, err := rescale(d, 0, Floor)
	if err != nil {
		panic(err)
	}
	return result
}

// Ceil returns the least scale-zero Decimal greater than or equal to d.
func (d Decimal) Ceil() Decimal {
	result, err := rescale(d, 0, Ceiling)
	if err != nil {
		panic(err)
	}
	return result
}

// roundCoefficient removes coefficient digits and rounds once at the resulting
// quantum. It consumes coefficient.
func roundCoefficient(coefficient *big.Int, scale Scale, digits, precision uint, mode RoundingMode) (Decimal, error) {
	workingScale := scaleAccumulator{small: scale}
	return roundCoefficientAtScale(coefficient, &workingScale, digits, precision, mode)
}

// roundCoefficientAtScale is the wide-scale form of roundCoefficient. It
// consumes coefficient and scale.
func roundCoefficientAtScale(coefficient *big.Int, scale *scaleAccumulator, digits, precision uint, mode RoundingMode) (Decimal, error) {
	remove := uint64(digits - precision)
	scale.subUint64(remove)
	if scale.promoted && scale.large.Cmp(scale.operand.SetInt64(math.MinInt64)) < 0 {
		return Decimal{}, ErrRange
	}
	chunkDigits := uint64(decimalWordDigits)
	chunkLimit := 4 * chunkDigits
	// Paired benchmarks show chunked division wins through four word-sized
	// blocks, or two largest cached blocks followed by one word-sized block.
	if remove >= uint64(len(smallPowersOfTen)) {
		chunkDigits = uint64(len(smallPowersOfTen) - 1)
		chunkLimit = 2*chunkDigits + uint64(decimalWordDigits)
	}
	if remove > chunkDigits && remove <= chunkLimit {
		divisor := &smallPowersOfTen[chunkDigits]
		sign := coefficient.Sign()
		discarded := false
		lowerDiscarded := false
		midpointComparison := 0
		var remainder big.Int
		for remaining := remove; remaining > 0; {
			chunk := min(remaining, chunkDigits)
			if chunk < chunkDigits {
				divisor = &smallPowersOfTen[chunk]
			}
			coefficient.QuoRem(coefficient, divisor, &remainder)
			if remainder.Sign() != 0 {
				discarded = true
			}
			remaining -= chunk
			if remaining != 0 {
				lowerDiscarded = discarded
				continue
			}
			if isHalfRounding(mode) {
				remainder.Abs(&remainder)
				remainder.Lsh(&remainder, 1)
				midpointComparison = remainder.CmpAbs(divisor)
				if midpointComparison == 0 && lowerDiscarded {
					midpointComparison = 1
				}
			}
		}
		if discarded {
			if mode == Exact {
				return Decimal{}, ErrInexact
			}
			if roundingIncrement(mode, sign, midpointComparison, coefficient) {
				if sign > 0 {
					coefficient.Add(coefficient, bigOne)
				} else {
					coefficient.Sub(coefficient, bigOne)
				}
			}
		}
	} else {
		var denominatorStorage, remainder big.Int
		denominator := &denominatorStorage
		if remove < uint64(len(smallPowersOfTen)) {
			denominator = &smallPowersOfTen[remove]
		} else {
			setPowerOfTen(denominator, remove)
		}
		coefficient.QuoRem(coefficient, denominator, &remainder)
		if err := roundQuotient(coefficient, &remainder, denominator, mode); err != nil {
			return Decimal{}, err
		}
	}

	// Rounding 9.99 to two digits initially produces coefficient 100. Removing
	// its final zero restores the requested precision without changing value.
	var remainder big.Int
	for uint(decimalDigitCount(coefficient)) > precision {
		coefficient.QuoRem(coefficient, bigTen, &remainder)
		if remainder.Sign() != 0 {
			return Decimal{}, ErrRange
		}
		scale.sub(1)
	}
	target, err := scale.fitRoundedCoefficient(coefficient)
	if err != nil {
		return Decimal{}, err
	}
	return makeDecimal(coefficient, target), nil
}

// roundQuotient rounds a quotient/remainder pair produced by QuoRem. It may
// reuse the caller-owned remainder as comparison scratch.
func roundQuotient(quotient, remainder, denominator *big.Int, mode RoundingMode) error {
	if remainder.Sign() == 0 {
		return nil
	}
	if mode == Exact {
		return ErrInexact
	}

	sign := remainder.Sign() * denominator.Sign()
	midpointComparison := 0
	if isHalfRounding(mode) {
		remainder.Abs(remainder)
		if denominator.BitLen() <= 64 {
			// Compare r with |d|-r without widening a word-sized remainder.
			value := remainder.Uint64()
			if denominator.Sign() > 0 {
				remainder.Sub(denominator, remainder)
			} else {
				remainder.Add(denominator, remainder)
				remainder.Neg(remainder)
			}
			midpointComparison = cmp.Compare(value, remainder.Uint64())
		} else {
			remainder.Lsh(remainder, 1)
			midpointComparison = remainder.CmpAbs(denominator)
		}
	}
	if roundingIncrement(mode, sign, midpointComparison, quotient) {
		if sign > 0 {
			quotient.Add(quotient, bigOne)
		} else {
			quotient.Sub(quotient, bigOne)
		}
	}
	return nil
}

func isHalfRounding(mode RoundingMode) bool {
	return mode == HalfEven || mode == HalfUp || mode == HalfDown
}

// roundingIncrement reports whether rounding a non-exact value should move
// the truncated result one quantum away from zero. midpointComparison compares
// the discarded magnitude with half a quantum; coefficient is the truncated
// result before any increment.
func roundingIncrement(mode RoundingMode, sign, midpointComparison int, coefficient *big.Int) bool {
	switch mode {
	case HalfEven:
		return midpointComparison > 0 || midpointComparison == 0 && coefficient.Bit(0) == 1
	case HalfUp:
		return midpointComparison >= 0
	case HalfDown:
		return midpointComparison > 0
	case ZeroFiveUp:
		var digit big.Int
		digit.Rem(coefficient, bigTen)
		return digit.Sign() == 0 || digit.CmpAbs(bigFive) == 0
	case AwayFromZero:
		return true
	case Floor:
		return sign < 0
	case Ceiling:
		return sign > 0
	case TowardZero:
		return false
	case Exact:
		panic("decimal: exact mode reached rounding increment")
	default:
		panic("decimal: invalid rounding mode")
	}
}

// rescale performs the package's single fixed-scale rounding operation. Other
// rounded operations reduce to this form or use the equivalent integer kernel.
func rescale(d Decimal, scale Scale, mode RoundingMode) (Decimal, error) {
	coefficient, current := decimalParts(d)
	if scale == current {
		return d, nil
	}
	if coefficient.Sign() == 0 {
		return makeDecimal(coefficient, scale), nil
	}
	if scale > current {
		result := multiplyByPowerOfTen(new(big.Int), coefficient, scaleDistance(scale, current))
		return makeDecimal(result, scale), nil
	}

	discard := scaleDistance(current, scale)
	digits := uint64(decimalDigitCount(coefficient))
	if discard > digits {
		if mode == Exact {
			return Decimal{}, ErrInexact
		}
		result := new(big.Int)
		if mode == AwayFromZero || mode == ZeroFiveUp || mode == Ceiling && coefficient.Sign() > 0 || mode == Floor && coefficient.Sign() < 0 {
			result.SetInt64(int64(coefficient.Sign()))
		}
		return makeDecimal(result, scale), nil
	}

	var denominator, quotient, remainder big.Int
	setPowerOfTen(&denominator, discard)
	quotient.QuoRem(coefficient, &denominator, &remainder)
	if err := roundQuotient(&quotient, &remainder, &denominator, mode); err != nil {
		return Decimal{}, err
	}
	return makeDecimal(&quotient, scale), nil
}

// roundCoefficientToPrecision rounds an owned coefficient and transfers its
// storage to the result when no digits need to be discarded.
func roundCoefficientToPrecision(coefficient *big.Int, scale Scale, precision uint, mode RoundingMode) (Decimal, error) {
	if precision == 0 {
		return makeDecimal(coefficient, scale), nil
	}
	digits := uint(decimalDigitCount(coefficient))
	if digits <= precision {
		return makeDecimal(coefficient, scale), nil
	}
	return roundCoefficient(coefficient, scale, digits, precision, mode)
}

// roundCoefficientToPrecisionAtScale delays fitting a wide exact scale until
// the context's final significant-digit rounding has been applied.
func roundCoefficientToPrecisionAtScale(coefficient *big.Int, scale *scaleAccumulator, precision uint, mode RoundingMode) (Decimal, error) {
	if !scale.promoted || scale.large.IsInt64() {
		valueScale := scale.small
		if scale.promoted {
			valueScale = Scale(scale.large.Int64())
		}
		return roundCoefficientToPrecision(coefficient, valueScale, precision, mode)
	}
	if coefficient.Sign() == 0 || precision == 0 || scale.large.Sign() < 0 {
		valueScale, err := scale.fitCoefficient(coefficient)
		if err != nil {
			return Decimal{}, err
		}
		return roundCoefficientToPrecision(coefficient, valueScale, precision, mode)
	}

	digits := uint(decimalDigitCount(coefficient))
	if digits <= precision {
		valueScale, err := scale.fitCoefficient(coefficient)
		if err != nil {
			return Decimal{}, err
		}
		return makeDecimal(coefficient, valueScale), nil
	}
	return roundCoefficientAtScale(coefficient, scale, digits, precision, mode)
}
