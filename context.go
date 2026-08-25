package decimal

import (
	"math"
	"math/big"
)

// Context controls operations that may round a result to a number of
// significant decimal digits. Its zero value requests unlimited precision and
// exact results.
//
// When Precision is zero, operations do not round. An operation whose exact
// result has no finite decimal representation returns [ErrInexact]. When
// Precision is non-zero, the final result is rounded to at most that many
// significant digits using Rounding. The Exact rounding mode instead rejects
// a result that would lose information.
//
// Context is a small value, contains no mutable status flags, and is safe to
// copy and use concurrently. Construct it with a keyed literal:
//
//	ctx := decimal.Context{Precision: 34, Rounding: decimal.HalfEven}
type Context struct {
	// Precision is the maximum number of significant decimal digits in a result.
	// Zero means unlimited precision.
	Precision uint

	// Rounding selects how a result is rounded when Precision is non-zero.
	// Its zero value is HalfEven.
	Rounding RoundingMode
}

// Validate reports whether c is a valid context. Arithmetic methods validate
// their context and return [ErrInvalidRoundingMode] when it is invalid.
func (c Context) Validate() error {
	if c.Rounding > Exact {
		return ErrInvalidRoundingMode
	}
	return nil
}

// Round applies c's precision and rounding mode to x. It returns x unchanged
// when c has unlimited precision. Round may return [ErrInvalidRoundingMode],
// [ErrInexact], or [ErrRange].
func (c Context) Round(x Decimal) (Decimal, error) {
	if err := c.Validate(); err != nil {
		return Decimal{}, err
	}
	return roundToPrecision(x, c.Precision, c.Rounding)
}

// Add returns x+y, rounded once according to c. It may return
// [ErrInvalidRoundingMode], [ErrInexact], or [ErrRange].
func (c Context) Add(x, y Decimal) (Decimal, error) {
	if err := c.Validate(); err != nil {
		return Decimal{}, err
	}
	return addToPrecision(x, y, c.Precision, c.Rounding)
}

// Sub returns x-y, rounded once according to c. It may return
// [ErrInvalidRoundingMode], [ErrInexact], or [ErrRange].
func (c Context) Sub(x, y Decimal) (Decimal, error) {
	if err := c.Validate(); err != nil {
		return Decimal{}, err
	}
	return addToPrecision(x, y.Neg(), c.Precision, c.Rounding)
}

// Mul returns x*y, rounded once according to c. It may return
// [ErrInvalidRoundingMode], [ErrInexact], or [ErrRange].
func (c Context) Mul(x, y Decimal) (Decimal, error) {
	if err := c.Validate(); err != nil {
		return Decimal{}, err
	}
	coefficient, scale, wideScale := multiplyParts(x, y)
	if wideScale == nil {
		return roundCoefficientToPrecision(coefficient, scale, c.Precision, c.Rounding)
	}
	var workingScale scaleAccumulator
	workingScale.set(wideScale)
	return roundCoefficientToPrecisionAtScale(coefficient, &workingScale, c.Precision, c.Rounding)
}

// Div returns x/y, rounded once according to c.
// It returns [ErrDivisionByZero] if y is zero. With unlimited precision, Div
// returns [ErrInexact] when the quotient has no finite decimal representation.
// Div may also return [ErrInvalidRoundingMode] or [ErrRange].
func (c Context) Div(x, y Decimal) (Decimal, error) {
	if err := c.Validate(); err != nil {
		return Decimal{}, err
	}
	return divideToPrecision(x, y, c.Precision, c.Rounding)
}

// FMA returns x*y+z with only one rounding, after the addition. It is more
// accurate than calling c.Mul followed by c.Add because the product is not
// rounded separately. FMA may return [ErrInvalidRoundingMode], [ErrInexact], or
// [ErrRange].
func (c Context) FMA(x, y, z Decimal) (Decimal, error) {
	if err := c.Validate(); err != nil {
		return Decimal{}, err
	}
	product, productScale, wideProductScale := multiplyParts(x, y)
	if wideProductScale == nil {
		return fmaToPrecision(product, productScale, z, c.Precision, c.Rounding)
	}
	zCoefficient, zScale := decimalParts(z)
	var workingScale scaleAccumulator
	workingScale.set(wideProductScale)
	if workingScale.large.Sign() < 0 || c.Precision == 0 {
		scale, err := workingScale.fitCoefficient(product)
		if err != nil {
			return Decimal{}, err
		}
		return fmaToPrecision(product, scale, z, c.Precision, c.Rounding)
	}
	if product.Sign() == 0 {
		return roundWithPreferredScale(z, Scale(math.MaxInt64), c.Precision, c.Rounding)
	}
	if zCoefficient.Sign() == 0 {
		return roundCoefficientToPrecisionAtScale(product, &workingScale, c.Precision, c.Rounding)
	}
	return addWideProductToPrecision(product, &workingScale, zCoefficient, zScale, c.Precision, c.Rounding)
}

// Sqrt returns the non-negative square root of x, rounded once according to c.
// It returns [ErrInvalidOperation] if x is negative. With unlimited precision,
// it returns [ErrInexact] unless the square root is a finite Decimal. Sqrt may
// also return [ErrInvalidRoundingMode] or [ErrRange].
func (c Context) Sqrt(x Decimal) (Decimal, error) {
	if err := c.Validate(); err != nil {
		return Decimal{}, err
	}
	return sqrtToPrecision(x, c.Precision, c.Rounding)
}

// Pow returns x raised to the integer power n, rounded once according to c.
// Negative powers are permitted. With unlimited precision, Pow returns
// [ErrInexact] if the result has no finite decimal representation. It returns
// [ErrDivisionByZero] for zero raised to a negative power and may also return
// [ErrInvalidRoundingMode] or [ErrRange].
func (c Context) Pow(x Decimal, n int64) (Decimal, error) {
	if err := c.Validate(); err != nil {
		return Decimal{}, err
	}
	return powToPrecision(x, n, c.Precision, c.Rounding)
}

// FromBigRat converts x to a Decimal under c. With unlimited precision, it
// returns [ErrInexact] unless x has a finite base-10 expansion. It does not
// retain x. It may also return [ErrInvalidRoundingMode] or [ErrRange]. FromBigRat
// panics if x is nil.
func (c Context) FromBigRat(x *big.Rat) (Decimal, error) {
	if x == nil {
		panic("decimal: nil *big.Rat")
	}
	if err := c.Validate(); err != nil {
		return Decimal{}, err
	}
	return divideToPrecision(NewBig(x.Num(), 0), NewBig(x.Denom(), 0), c.Precision, c.Rounding)
}

func fmaToPrecision(product *big.Int, productScale Scale, z Decimal, precision uint, mode RoundingMode) (Decimal, error) {
	zCoefficient, zScale := decimalParts(z)
	preferredScale := max(productScale, zScale)
	if product.Sign() == 0 {
		return roundWithPreferredScale(z, preferredScale, precision, mode)
	}
	if zCoefficient.Sign() == 0 {
		return roundCoefficientWithPreferredScale(product, product, productScale, preferredScale, precision, mode)
	}
	return addNonzeroPartsToPrecision(
		scaledCoefficient{coefficient: product, scale: productScale},
		scaledCoefficient{coefficient: zCoefficient, scale: zScale},
		precision,
		mode,
	)
}

// addWideProductToPrecision adds a product whose preferred scale is above
// Scale's range to a regular Decimal coefficient. Translating both scales by
// the same amount lets the established bounded-addition kernel perform the one
// final rounding required by FMA.
func addWideProductToPrecision(product *big.Int, productScale *scaleAccumulator, zCoefficient *big.Int, zScale Scale, precision uint, mode RoundingMode) (Decimal, error) {
	var gap, zScaleValue big.Int
	productScale.value(&gap)
	gap.Sub(&gap, zScaleValue.SetInt64(int64(zScale)))
	if gap.Sign() < 0 {
		panic("decimal: wide product scale is not above addend scale")
	}
	gapFits := gap.IsUint64()
	shiftedProductScale := Scale(math.MaxInt64)
	shiftedZScale := Scale(math.MinInt64)
	shiftedProduct := product
	var directionalRemainder big.Int
	actualScale := productScale.clone()
	if gapFits {
		shiftedZScale = Scale(uint64(math.MaxInt64) - gap.Uint64())
	} else {
		// A scale gap wider than uint64 is also wider than any in-memory
		// coefficient. The product can only act as a non-zero directional
		// remainder beside the dominant addend.
		shiftedProduct = directionalRemainder.SetInt64(int64(product.Sign()))
		actualScale = scaleAccumulator{small: zScale}
	}
	result, err := addNonzeroPartsToPrecision(
		scaledCoefficient{coefficient: shiftedProduct, scale: shiftedProductScale},
		scaledCoefficient{coefficient: zCoefficient, scale: shiftedZScale},
		precision,
		mode,
	)
	if err != nil {
		return Decimal{}, err
	}
	coefficient, resultScale := decimalParts(result)
	ownedCoefficient := new(big.Int).Set(coefficient)
	if gapFits {
		actualScale.subUint64(scaleDistance(shiftedProductScale, resultScale))
	} else {
		actualScale.addUint64(scaleDistance(resultScale, shiftedZScale))
	}
	target, err := actualScale.fitRoundedCoefficient(ownedCoefficient)
	if err != nil {
		return Decimal{}, err
	}
	return makeDecimal(ownedCoefficient, target), nil
}

// addToPrecision adds or subtracts two values with a single final rounding.
// Nearby operands use exact alignment. When their scales are far apart, it
// rounds from narrow integer bounds so the discarded operand remains visible
// to directed rounding and tie-breaking without materializing the scale gap.
func addToPrecision(x, y Decimal, precision uint, mode RoundingMode) (Decimal, error) {
	if precision == 0 {
		return x.Add(y), nil
	}

	xCoefficient, xScale := decimalParts(x)
	yCoefficient, yScale := decimalParts(y)
	preferredScale := max(xScale, yScale)
	if xCoefficient.Sign() == 0 {
		return roundWithPreferredScale(y, preferredScale, precision, mode)
	}
	if yCoefficient.Sign() == 0 {
		return roundWithPreferredScale(x, preferredScale, precision, mode)
	}
	return addNonzeroPartsToPrecision(
		scaledCoefficient{coefficient: xCoefficient, scale: xScale},
		scaledCoefficient{coefficient: yCoefficient, scale: yScale},
		precision,
		mode,
	)
}

// roundWithPreferredScale rounds d as though an exact zero operand had first
// extended its representation to preferredScale.
func roundWithPreferredScale(d Decimal, preferredScale Scale, precision uint, mode RoundingMode) (Decimal, error) {
	coefficient, scale := decimalParts(d)
	if preferredScale == scale {
		return roundToPrecision(d, precision, mode)
	}
	return roundCoefficientWithPreferredScale(nil, coefficient, scale, preferredScale, precision, mode)
}

// roundCoefficientWithPreferredScale rounds coefficient as though an exact
// zero operand had extended its representation to preferredScale. If dst is
// non-nil, it is caller-owned storage that may alias coefficient; otherwise the
// function allocates only when the coefficient must change. Callers must
// provide preferredScale >= scale.
func roundCoefficientWithPreferredScale(dst, coefficient *big.Int, scale, preferredScale Scale, precision uint, mode RoundingMode) (Decimal, error) {
	if preferredScale < scale {
		panic("decimal: preferred scale below coefficient scale")
	}
	if coefficient.Sign() == 0 {
		return makeDecimal(coefficient, preferredScale), nil
	}
	if precision == 0 {
		if preferredScale > scale {
			if dst == nil {
				dst = new(big.Int)
			}
			multiplyByPowerOfTen(dst, coefficient, scaleDistance(preferredScale, scale))
			coefficient = dst
		}
		return makeDecimal(coefficient, preferredScale), nil
	}
	digits := uint(decimalDigitCount(coefficient))
	if digits > precision {
		if dst == nil {
			dst = new(big.Int)
		}
		if dst != coefficient {
			dst.Set(coefficient)
		}
		return roundCoefficientToPrecision(dst, scale, precision, mode)
	}
	if preferredScale == scale {
		return makeDecimal(coefficient, scale), nil
	}

	available := uint64(precision - digits)
	gap := min(scaleDistance(preferredScale, scale), available)
	if dst == nil {
		dst = new(big.Int)
	}
	multiplyByPowerOfTen(dst, coefficient, gap)
	return makeDecimal(dst, Scale(uint64(scale)+gap)), nil
}

// addNonzeroPartsToPrecision executes either exact alignment or the bounded
// addition plan selected for two non-zero operands.
func addNonzeroPartsToPrecision(x, y scaledCoefficient, precision uint, mode RoundingMode) (Decimal, error) {
	plan, err := planAdditionToPrecision(x, y, precision, mode)
	if err != nil {
		return Decimal{}, err
	}
	if plan.alignExactly {
		var coefficient big.Int
		xCoefficient, yCoefficient, scale := alignedParts(&coefficient, x, y)
		coefficient.Add(xCoefficient, yCoefficient)
		return roundCoefficientToPrecision(&coefficient, scale, precision, mode)
	}
	if mode == ZeroFiveUp && precision == 1 && x.coefficient.Sign() != y.coefficient.Sign() {
		dominant := x
		if y.scale == plan.dominantScale {
			dominant = y
		}
		digits := decimalDigitCount(dominant.coefficient)
		if compareScaledByPowerOfTen(dominant.coefficient, bigOne, uint64(digits-1)) == 0 {
			// At the coarser power-of-ten quantum, 05up would conceal the
			// cancellation by rounding a truncated zero back to one.
			plan.targetScale++
		}
	}

	// The plan places the dominant operand at precision digits. The distant
	// operand can therefore leave that width unchanged, carry it up by one, or
	// cancel its leading boundary digit. A carry normalizes exactly; a
	// cancellation needs exactly one finer working scale.
	result, err := addAtScale(x, y, plan.dominantScale, plan.targetScale, mode)
	if err != nil {
		return Decimal{}, err
	}
	coefficient, _ := decimalParts(result)
	digits := uint(decimalDigitCount(coefficient))
	if coefficient.Sign() == 0 || digits < precision {
		// The distant operand can move a power-of-ten boundary down by one
		// digit. One finer scale restores the requested precision; the plan
		// guarantees targetScale < preferredScale.
		result, err = addAtScale(x, y, plan.dominantScale, plan.targetScale+1, mode)
		if err != nil {
			return Decimal{}, err
		}
		coefficient, _ = decimalParts(result)
		if coefficient.Sign() == 0 || uint(decimalDigitCount(coefficient)) != precision {
			panic("decimal: bounded addition plan invariant violated")
		}
		return result, nil
	}
	if digits == precision {
		return result, nil
	}
	if digits != precision+1 {
		panic("decimal: bounded addition plan invariant violated")
	}
	if plan.targetScale == Scale(math.MinInt64) {
		return Decimal{}, ErrRange
	}

	// Rounding can carry a p-digit coefficient only to ±10^p. Move that
	// trailing zero into the scale without recomputing the bounded sum.
	var normalized, remainder big.Int
	normalized.QuoRem(coefficient, bigTen, &remainder)
	if remainder.Sign() != 0 || uint(decimalDigitCount(&normalized)) != precision {
		panic("decimal: bounded addition plan invariant violated")
	}
	return makeDecimal(&normalized, plan.targetScale-1), nil
}

type additionPlan struct {
	alignExactly  bool
	dominantScale Scale
	targetScale   Scale
}

// planAdditionToPrecision chooses exact alignment when its cost is bounded;
// otherwise it establishes the dominant operand and working scale needed for
// bounded addition.
func planAdditionToPrecision(x, y scaledCoefficient, precision uint, mode RoundingMode) (additionPlan, error) {
	preferredScale := max(x.scale, y.scale)
	exactPlan := additionPlan{alignExactly: true}
	if precision == 0 {
		return exactPlan, nil
	}

	scaleGap := scaleDistance(max(x.scale, y.scale), min(x.scale, y.scale))
	if scaleGap < uint64(len(smallPowersOfTen)) {
		return exactPlan, nil
	}
	maximumDigits := max(decimalDigitCount(x.coefficient), decimalDigitCount(y.coefficient))
	alignmentLimit := uint64(maximumDigits) + 2
	if uint64(precision) > math.MaxUint64-alignmentLimit || scaleGap <= alignmentLimit+uint64(precision) {
		return exactPlan, nil
	}
	if mode == Exact {
		return additionPlan{}, ErrInexact
	}

	plan := additionPlan{dominantScale: x.scale}
	xExponent, xExponentOK := adjustedExponentScale(x.coefficient, x.scale)
	yExponent, yExponentOK := adjustedExponentScale(y.coefficient, y.scale)
	if xExponentOK && yExponentOK && uint64(precision-1) <= uint64(math.MaxInt64) {
		dominantExponent := xExponent
		if yExponent > xExponent {
			plan.dominantScale = y.scale
			dominantExponent = yExponent
		} else if yExponent == xExponent {
			// Equal adjusted exponents imply that exact alignment is proportional
			// to coefficient size even though the represented scales are distant.
			return exactPlan, nil
		}
		var ok bool
		plan.targetScale, ok = subtractScales(Scale(precision-1), dominantExponent)
		if !ok {
			if dominantExponent < 0 {
				return exactPlan, nil
			}
			return additionPlan{}, ErrRange
		}
	} else {
		var xExponentValue, yExponentValue big.Int
		setAdjustedExponent(&xExponentValue, x.coefficient, x.scale)
		setAdjustedExponent(&yExponentValue, y.coefficient, y.scale)
		dominantExponent := &xExponentValue
		switch yExponentValue.Cmp(&xExponentValue) {
		case -1:
			// x is already the planned dominant operand.
		case 1:
			plan.dominantScale = y.scale
			dominantExponent = &yExponentValue
		case 0:
			return exactPlan, nil
		default:
			panic("decimal: invalid comparison result")
		}

		var target big.Int
		target.SetUint64(uint64(precision - 1))
		target.Sub(&target, dominantExponent)
		if !target.IsInt64() {
			if target.Sign() > 0 {
				return exactPlan, nil
			}
			return additionPlan{}, ErrRange
		}
		plan.targetScale = Scale(target.Int64())
	}
	if preferredScale <= plan.targetScale {
		return exactPlan, nil
	}
	return plan, nil
}

// addAtScale rounds x+y to target using open integer bounds at a nearby guard
// scale. Once the dominant operand is exact at the guard scale, the remaining
// interval is narrower than one unit and cannot straddle a rounding boundary.
func addAtScale(x, y scaledCoefficient, dominantScale, target Scale, mode RoundingMode) (Decimal, error) {
	maximumGuard := scaleDistance(Scale(math.MaxInt64), target)
	guard := min(uint64(2), maximumGuard)
	var xLower, xUpper, yLower, yUpper big.Int
	var lower, upper, denominator big.Int
	var lowerNumerator, upperNumerator big.Int
	var lowerRounded, lowerRemainder, upperRounded, upperRemainder big.Int
	for {
		workScale := Scale(uint64(target) + guard)
		decimalBoundsAtScale(&xLower, &xUpper, x, workScale)
		decimalBoundsAtScale(&yLower, &yUpper, y, workScale)
		lower.Add(&xLower, &yLower)
		upper.Add(&xUpper, &yUpper)
		if lower.Cmp(&upper) == 0 {
			return rescale(makeDecimal(&lower, workScale), target, mode)
		}

		// Convert the open integer bounds to exact half-unit numerators. If
		// both endpoints round alike, every value between them does as well.
		setPowerOfTen(&denominator, guard)
		denominator.Lsh(&denominator, 1)
		lowerNumerator.Lsh(&lower, 1)
		lowerNumerator.Add(&lowerNumerator, bigOne)
		upperNumerator.Lsh(&upper, 1)
		upperNumerator.Sub(&upperNumerator, bigOne)

		lowerRounded.QuoRem(&lowerNumerator, &denominator, &lowerRemainder)
		if err := roundQuotient(&lowerRounded, &lowerRemainder, &denominator, mode); err != nil {
			return Decimal{}, err
		}
		upperRounded.QuoRem(&upperNumerator, &denominator, &upperRemainder)
		if err := roundQuotient(&upperRounded, &upperRemainder, &denominator, mode); err != nil {
			return Decimal{}, err
		}
		if lowerRounded.Cmp(&upperRounded) == 0 {
			return makeDecimal(&lowerRounded, target), nil
		}

		// Once the dominant operand is exact at the guard scale, each extra
		// digit strictly narrows the remaining rounding uncertainty.
		needed := uint64(0)
		if dominantScale > target {
			needed = scaleDistance(dominantScale, target)
		}
		if guard < needed {
			guard = needed
			continue
		}
		if guard == maximumGuard {
			return Decimal{}, ErrRange
		}
		guard++
	}
}

// decimalBoundsAtScale returns integer bounds lower and upper such that d at
// scale lies in [lower, upper]. For a non-integral scaled value, both bounds are
// strict; for an integral value they are equal.
func decimalBoundsAtScale(lower, upper *big.Int, value scaledCoefficient, scale Scale) {
	if scale >= value.scale {
		multiplyByPowerOfTen(lower, value.coefficient, scaleDistance(scale, value.scale))
		upper.Set(lower)
		return
	}

	discard := scaleDistance(value.scale, scale)
	var remainder big.Int
	if discard < uint64(decimalDigitCount(value.coefficient)) {
		var divisor big.Int
		setPowerOfTen(&divisor, discard)
		lower.QuoRem(value.coefficient, &divisor, &remainder)
	} else {
		lower.SetInt64(0)
		remainder.Set(value.coefficient)
	}
	upper.Set(lower)
	if remainder.Sign() > 0 {
		upper.Add(upper, bigOne)
	} else if remainder.Sign() < 0 {
		lower.Sub(lower, bigOne)
	}
}
