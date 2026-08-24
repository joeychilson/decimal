package decimal

import "math/big"

// Add returns d+x exactly. The result has the finer (larger) scale of its
// operands, so adding 1.2 and 3.40 produces 4.60.
func (d Decimal) Add(x Decimal) Decimal {
	dCoefficient, dScale := decimalParts(d)
	xCoefficient, xScale := decimalParts(x)
	var result big.Int
	dCoefficient, xCoefficient, scale := alignedParts(
		&result,
		scaledCoefficient{coefficient: dCoefficient, scale: dScale},
		scaledCoefficient{coefficient: xCoefficient, scale: xScale},
	)
	result.Add(dCoefficient, xCoefficient)
	return makeDecimal(&result, scale)
}

// Sub returns d-x exactly. The result has the finer (larger) scale of its
// operands, so subtracting 1.2 from 3.40 produces 2.20.
func (d Decimal) Sub(x Decimal) Decimal {
	dCoefficient, dScale := decimalParts(d)
	xCoefficient, xScale := decimalParts(x)
	var result big.Int
	dCoefficient, xCoefficient, scale := alignedParts(
		&result,
		scaledCoefficient{coefficient: dCoefficient, scale: dScale},
		scaledCoefficient{coefficient: xCoefficient, scale: xScale},
	)
	result.Sub(dCoefficient, xCoefficient)
	return makeDecimal(&result, scale)
}

// Mul returns d*x exactly. Its preferred scale is the sum of the operand
// scales. If that scale is outside the range of [Scale], Mul adjusts the
// representation only when it can preserve the exact value; otherwise it
// returns [ErrRange].
func (d Decimal) Mul(x Decimal) (Decimal, error) {
	coefficient, scale, err := multiplyParts(d, x)
	if err != nil {
		return Decimal{}, err
	}
	return makeDecimal(coefficient, scale), nil
}

// Neg returns -d. Zero has no negative representation, so Neg leaves zero's
// sign unchanged while preserving its scale.
func (d Decimal) Neg() Decimal {
	coefficient, scale := decimalParts(d)
	if coefficient.Sign() == 0 {
		return d
	}
	return makeDecimal(new(big.Int).Neg(coefficient), scale)
}

// Abs returns the absolute value of d.
func (d Decimal) Abs() Decimal {
	coefficient, scale := decimalParts(d)
	if coefficient.Sign() >= 0 {
		return d
	}
	return makeDecimal(new(big.Int).Abs(coefficient), scale)
}

// alignScales returns the common scale for two coefficients and the decimal
// shifts needed to express each coefficient at that scale.
func alignScales(x, y Scale) (scale Scale, xShift, yShift uint64) {
	if x > y {
		return x, 0, scaleDistance(x, y)
	}
	if y > x {
		return y, scaleDistance(y, x), 0
	}
	return x, 0, 0
}

// alignedParts expresses x and y at one scale, using storage for the sole
// coefficient that may need shifting. The returned coefficients are borrowed;
// storage may also be the receiver of an alias-safe big.Int operation on them.
func alignedParts(storage *big.Int, x, y scaledCoefficient) (xCoefficient, yCoefficient *big.Int, scale Scale) {
	scale, xShift, yShift := alignScales(x.scale, y.scale)
	xCoefficient = x.coefficient
	yCoefficient = y.coefficient
	if xShift != 0 {
		xCoefficient = multiplyByPowerOfTen(storage, xCoefficient, xShift)
	}
	if yShift != 0 {
		yCoefficient = multiplyByPowerOfTen(storage, yCoefficient, yShift)
	}
	return xCoefficient, yCoefficient, scale
}

// multiplyParts returns the exact product, adjusting its representation only
// when the operands' preferred scale lies outside Scale's range.
func multiplyParts(x, y Decimal) (*big.Int, Scale, error) {
	xCoefficient, xScale := decimalParts(x)
	yCoefficient, yScale := decimalParts(y)
	coefficient := new(big.Int).Mul(xCoefficient, yCoefficient)
	if scale, ok := addScales(xScale, yScale); ok {
		return coefficient, scale, nil
	}

	var scale, component big.Int
	scale.SetInt64(int64(xScale))
	component.SetInt64(int64(yScale))
	scale.Add(&scale, &component)
	fitted, err := fitCoefficientScale(coefficient, &scale)
	return coefficient, fitted, err
}
