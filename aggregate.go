package decimal

import "math/big"

// Sum returns the exact sum of values. With no arguments it returns zero.
func Sum(values ...Decimal) Decimal {
	if len(values) == 0 {
		return Decimal{}
	}
	coefficient, scale := decimalParts(values[0])
	var total, shifted big.Int
	total.Set(coefficient)
	for _, value := range values[1:] {
		coefficient, valueScale := decimalParts(value)
		alignedScale, totalShift, coefficientShift := alignScales(scale, valueScale)
		if totalShift != 0 {
			multiplyByPowerOfTen(&total, &total, totalShift)
		}
		if coefficientShift != 0 {
			coefficient = multiplyByPowerOfTen(&shifted, coefficient, coefficientShift)
		}
		total.Add(&total, coefficient)
		scale = alignedScale
	}
	return makeDecimal(&total, scale)
}

// Product returns the exact product of values. With no arguments it returns
// one with scale zero. Its preferred scale is the sum of the operand scales.
// If that scale is outside the range of [Scale], Product adjusts the
// representation only when it can preserve the exact value; otherwise it
// returns [ErrRange].
func Product(values ...Decimal) (Decimal, error) {
	coefficient := big.NewInt(1)
	var scale scaleAccumulator
	for _, value := range values {
		valueCoefficient, valueScale := decimalParts(value)
		scale.add(valueScale)
		coefficient.Mul(coefficient, valueCoefficient)
	}
	resultScale, err := scale.fitCoefficient(coefficient)
	if err != nil {
		return Decimal{}, err
	}
	return makeDecimal(coefficient, resultScale), nil
}
