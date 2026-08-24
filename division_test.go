package decimal

import (
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
)

func TestDiv_PreservesPreferredScaleAndReportsErrors(t *testing.T) {
	divisions := []struct {
		x, y string
		want string
	}{
		{"1", "2", "0.5"},
		{"1.00", "2", "0.50"},
		{"1.00", "0.2", "5.0"},
		{"-1", "8", "-0.125"},
		{"0.00", "2", "0.00"},
	}
	for _, test := range divisions {
		got, err := MustParse(test.x).Div(MustParse(test.y))
		if err != nil {
			t.Fatalf("%s/%s: %v", test.x, test.y, err)
		}
		if got.String() != test.want {
			t.Errorf("%s/%s = %s, want %s", test.x, test.y, got, test.want)
		}
	}
	if _, err := FromInt(1).Div(FromInt(3)); !errors.Is(err, ErrInexact) {
		t.Fatalf("1/3 error = %v", err)
	}
	if _, err := FromInt(1).Div(Decimal{}); !errors.Is(err, ErrDivisionByZero) {
		t.Fatalf("1/0 error = %v", err)
	}
}

func TestDiv_HandlesExtremeScaleShortcuts(t *testing.T) {
	large := New(1, Scale(math.MinInt64))
	small := New(1, Scale(math.MaxInt64))
	if got, err := large.Div(New(1, 1)); err != nil || got.Coefficient().Cmp(big.NewInt(10)) != 0 || got.Scale() != Scale(math.MinInt64) {
		t.Fatalf("minimum-scale quotient = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	if got, err := New(10, Scale(math.MaxInt64)).Div(New(1, -1)); err != nil || got.Coefficient().Cmp(big.NewInt(1)) != 0 || got.Scale() != Scale(math.MaxInt64) {
		t.Fatalf("maximum-scale quotient = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	if _, err := small.Div(New(1, -1)); !errors.Is(err, ErrRange) {
		t.Fatalf("unrepresentable maximum-scale quotient error = %v", err)
	}
	if got, err := New(0, Scale(math.MaxInt64)).Div(New(1, -1)); err != nil || !got.SameRepresentation(New(0, Scale(math.MaxInt64))) {
		t.Fatalf("maximum-scale zero quotient = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	if got, err := New(0, Scale(math.MinInt64)).Div(New(1, 1)); err != nil || !got.SameRepresentation(New(0, Scale(math.MinInt64))) {
		t.Fatalf("minimum-scale zero quotient = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
}

func TestDivScale_RejectsInvalidRoundingMode(t *testing.T) {
	mode := RoundingMode(255)
	value := FromInt(1)
	if _, err := value.DivScale(FromInt(2), 0, mode); !errors.Is(err, ErrInvalidRoundingMode) {
		t.Fatalf("error = %v, want ErrInvalidRoundingMode", err)
	}
}

func TestDivScale_ReportsDivisionByZero(t *testing.T) {
	if _, err := FromInt(1).DivScale(Decimal{}, 2, HalfEven); !errors.Is(err, ErrDivisionByZero) {
		t.Fatalf("scaled 1/0 error = %v", err)
	}
}

func TestDivScale_MatchesBigRatForRandomInputs(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	modes := []RoundingMode{HalfEven, HalfUp, HalfDown, TowardZero, AwayFromZero, Floor, Ceiling, ZeroFiveUp, Exact}
	for range 2_000 {
		x := New(int64(rng.Uint64()), Scale(int64(rng.Uint64()%17)-8))
		yCoefficient := int64(rng.Uint64())
		if yCoefficient == 0 {
			yCoefficient = 1
		}
		y := New(yCoefficient, Scale(int64(rng.Uint64()%17)-8))
		target := Scale(int64(rng.Uint64()%17) - 8)
		mode := modes[rng.Uint64()%uint64(len(modes))]

		got, gotErr := x.DivScale(y, target, mode)
		want, exact := roundRatToScale(new(big.Rat).Quo(x.BigRat(), y.BigRat()), target, mode)
		if mode == Exact && !exact {
			if !errors.Is(gotErr, ErrInexact) {
				t.Fatalf("DivScale(%s, %s, %d, Exact) error = %v", x, y, target, gotErr)
			}
			continue
		}
		if gotErr != nil {
			t.Fatalf("DivScale(%s, %s, %d, %s): %v", x, y, target, mode, gotErr)
		}
		if !got.SameRepresentation(want) {
			t.Fatalf("DivScale(%s, %s, %d, %s) = %s, want %s", x, y, target, mode, got, want)
		}
	}
}

func TestDivScale_MatchesRationalOracleAcrossWordBoundaries(t *testing.T) {
	maximumInt64 := new(big.Int).SetInt64(math.MaxInt64 - 1)
	maximumUint64 := new(big.Int).SetUint64(math.MaxUint64 - 1)
	firstMultiword := new(big.Int).Lsh(big.NewInt(1), 64)
	modes := []RoundingMode{HalfEven, HalfUp, HalfDown, TowardZero, AwayFromZero, Floor, Ceiling, ZeroFiveUp, Exact}
	for _, divisorMagnitude := range []*big.Int{maximumInt64, maximumUint64, firstMultiword} {
		half := new(big.Int).Rsh(new(big.Int).Set(divisorMagnitude), 1)
		for _, delta := range []int64{-1, 0, 1} {
			numeratorMagnitude := new(big.Int).Mul(new(big.Int).Set(divisorMagnitude), bigTwo)
			numeratorMagnitude.Add(numeratorMagnitude, half)
			numeratorMagnitude.Add(numeratorMagnitude, big.NewInt(delta))
			for _, numeratorSign := range []int64{-1, 1} {
				for _, divisorSign := range []int64{-1, 1} {
					numerator := new(big.Int).Mul(new(big.Int).Set(numeratorMagnitude), big.NewInt(numeratorSign))
					divisor := new(big.Int).Mul(new(big.Int).Set(divisorMagnitude), big.NewInt(divisorSign))
					x := NewBig(numerator, 0)
					y := NewBig(divisor, 0)
					for _, mode := range modes {
						got, gotErr := x.DivScale(y, 0, mode)
						want, exact := roundRatToScale(new(big.Rat).SetFrac(numerator, divisor), 0, mode)
						if mode == Exact && !exact {
							if !errors.Is(gotErr, ErrInexact) {
								t.Fatalf("DivScale(%s, %s, 0, Exact) error = %v, want ErrInexact", x, y, gotErr)
							}
							continue
						}
						if gotErr != nil || !got.SameRepresentation(want) {
							t.Fatalf("DivScale(%s, %s, 0, %s) = %s, %v; want %s", x, y, mode, got, gotErr, want)
						}
					}
				}
			}
		}
	}
}

func TestDivScale_HandlesExtremeScaleShortcuts(t *testing.T) {
	small := New(1, Scale(math.MaxInt64))
	divisor := New(1, Scale(math.MinInt64))
	for _, mode := range [...]RoundingMode{HalfEven, HalfUp, HalfDown} {
		if got, err := small.DivScale(divisor, Scale(math.MinInt64), mode); err != nil || !got.IsZero() || got.Scale() != Scale(math.MinInt64) {
			t.Fatalf("extreme %s DivScale = coefficient %s, scale %d, %v", mode, got.Coefficient(), got.Scale(), err)
		}
	}
	if got, err := small.DivScale(divisor, Scale(math.MinInt64), AwayFromZero); err != nil || got.Coefficient().Cmp(big.NewInt(1)) != 0 || got.Scale() != Scale(math.MinInt64) {
		t.Fatalf("extreme away DivScale = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	if got, err := small.Neg().DivScale(divisor, Scale(math.MinInt64), Floor); err != nil || got.Coefficient().Cmp(big.NewInt(-1)) != 0 || got.Scale() != Scale(math.MinInt64) {
		t.Fatalf("extreme floor DivScale = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	if _, err := small.DivScale(divisor, Scale(math.MinInt64), Exact); !errors.Is(err, ErrInexact) {
		t.Fatalf("extreme exact DivScale error = %v", err)
	}
	if got, err := New(0, Scale(math.MaxInt64)).DivScale(FromInt(3), Scale(math.MinInt64), HalfEven); err != nil || got.Scale() != Scale(math.MinInt64) || !got.IsZero() {
		t.Fatalf("extreme zero DivScale = %s (scale %d), %v", got, got.Scale(), err)
	}
}

func TestQuoRem_PreservesIdentityAndScale(t *testing.T) {
	for _, test := range []struct {
		name                string
		dividend, divisor   Decimal
		quotient, remainder Decimal
	}{
		{"positive", MustParse("7.5"), FromInt(2), FromInt(3), MustParse("1.5")},
		{"negative divisor", MustParse("7.5"), FromInt(-2), FromInt(-3), MustParse("1.5")},
		{"negative dividend", MustParse("-7.5"), FromInt(2), FromInt(-3), MustParse("-1.5")},
		{"both negative", MustParse("-7.5"), FromInt(-2), FromInt(3), MustParse("-1.5")},
		{"finer divisor", FromInt(7), MustParse("2.0"), FromInt(3), MustParse("1.0")},
		{"zero quotient", MustParse("1.00"), MustParse("2.0"), Decimal{}, MustParse("1.00")},
		{"zero remainder", FromInt(6), MustParse("2.0"), FromInt(3), MustParse("0.0")},
		{"scaled zero", MustParse("0.00"), MustParse("2.0"), Decimal{}, MustParse("0.00")},
		{"oversized divisor", New(123, -1), New(1_000, -999_999), Decimal{}, New(123, -1)},
		{"oversized negative divisor", New(123, -1), New(-1_000, -999_999), Decimal{}, New(123, -1)},
		{"oversized divisor and negative dividend", New(-123, -1), New(1_000, -999_999), Decimal{}, New(-123, -1)},
		{"zero with finer divisor", New(0, -1), New(1, 7), Decimal{}, New(0, 7)},
		{"equal magnitude scale boundary", New(1_000, 0), New(1, -3), FromInt(1), New(0, 0)},
		{"smaller divisor scale boundary", New(1_001, 0), New(1, -3), FromInt(1), New(1, 0)},
	} {
		t.Run("quo rem preserves its identity for "+test.name, func(t *testing.T) {
			quotient, remainder, err := test.dividend.QuoRem(test.divisor)
			if err != nil || !quotient.SameRepresentation(test.quotient) || !remainder.SameRepresentation(test.remainder) {
				t.Fatalf("got (%s [scale %d], %s [scale %d], %v); want (%s [scale %d], %s [scale %d])", quotient, quotient.Scale(), remainder, remainder.Scale(), err, test.quotient, test.quotient.Scale(), test.remainder, test.remainder.Scale())
			}
			product, err := quotient.Mul(test.divisor)
			if err != nil {
				t.Fatal(err)
			}
			if got := product.Add(remainder); !got.Equal(test.dividend) {
				t.Fatalf("q*x+r = %s, want %s", got, test.dividend)
			}
		})
	}
	if _, _, err := FromInt(1).QuoRem(Decimal{}); !errors.Is(err, ErrDivisionByZero) {
		t.Fatalf("QuoRem division by zero error = %v", err)
	}
}

func TestQuoRem_MatchesBigRatForRandomInputs(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 2_000 {
		x := New(int64(rng.Uint64()), Scale(int64(rng.Uint64()%17)-8))
		y := New(int64(rng.Uint64()), Scale(int64(rng.Uint64()%17)-8))
		if y.IsZero() {
			continue
		}
		quotient, remainder, err := x.QuoRem(y)
		if err != nil {
			t.Fatalf("QuoRem(%s, %s): %v", x, y, err)
		}
		product, err := quotient.Mul(y)
		if err != nil {
			t.Fatalf("Mul(%s, %s): %v", quotient, y, err)
		}
		if quotient.Scale() != 0 || !product.Add(remainder).Equal(x) {
			t.Fatalf("QuoRem(%s, %s) = (%s, %s) violates x = q*y+r", x, y, quotient, remainder)
		}
		remainderMagnitude := remainder.Abs()
		divisorMagnitude := y.Abs()
		if remainderMagnitude.Cmp(divisorMagnitude) >= 0 {
			t.Fatalf("QuoRem(%s, %s) remainder magnitude %s is not less than %s", x, y, remainderMagnitude, divisorMagnitude)
		}
		if !remainder.IsZero() && remainder.Sign() != x.Sign() {
			t.Fatalf("QuoRem(%s, %s) remainder %s has the wrong sign", x, y, remainder)
		}
	}
}

func TestRem_PreservesExpectedScale(t *testing.T) {
	if got, err := MustParse("7.5").Rem(FromInt(2)); err != nil || got.String() != "1.5" {
		t.Fatalf("Rem = %s, %v", got, err)
	}
	if got, err := FromInt(1).Rem(Decimal{}); !errors.Is(err, ErrDivisionByZero) || !got.SameRepresentation(Decimal{}) {
		t.Fatalf("Rem division by zero = %s, %v; want zero and ErrDivisionByZero", got, err)
	}
}

func TestDecimalPrimeFactorDigits_WordBoundary(t *testing.T) {
	twoTo63 := new(big.Int).Lsh(big.NewInt(1), 63)
	fiveTo27 := new(big.Int).Exp(big.NewInt(5), big.NewInt(27), nil)
	tenTo20 := new(big.Int).Exp(big.NewInt(10), big.NewInt(20), nil)
	tests := []struct {
		name   string
		value  *big.Int
		digits uint64
		ok     bool
	}{
		{"one", big.NewInt(1), 0, true},
		{"negative one", big.NewInt(-1), 0, true},
		{"eight", big.NewInt(8), 3, true},
		{"one hundred twenty-five", big.NewInt(125), 3, true},
		{"ten", big.NewInt(10), 1, true},
		{"two to sixty-three", twoTo63, 63, true},
		{"negative two to sixty-three", new(big.Int).Neg(new(big.Int).Set(twoTo63)), 63, true},
		{"five to twenty-seven", fiveTo27, 27, true},
		{"mixed prime", big.NewInt(6), 0, false},
		{"larger than word", tenTo20, 0, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			digits, ok := decimalPrimeFactorDigits(test.value)
			if digits != test.digits || ok != test.ok {
				t.Fatalf("decimalPrimeFactorDigits(%s) = %d, %t; want %d, %t", test.value, digits, ok, test.digits, test.ok)
			}
		})
	}
}

var (
	divisionBenchmarkDecimal   Decimal
	divisionBenchmarkRemainder Decimal
	errDivisionBenchmark       error
)

func BenchmarkDivScale(b *testing.B) {
	x := MustParse("12345678901234567890.123456789")
	y := MustParse("987654321.0123456789")
	b.ReportAllocs()
	for b.Loop() {
		divisionBenchmarkDecimal, errDivisionBenchmark = x.DivScale(y, 18, HalfEven)
	}
}

func BenchmarkQuoRem(b *testing.B) {
	x := MustParse("12345678901234567890.123456789")
	y := MustParse("987654321.0123456789")
	b.ReportAllocs()
	for b.Loop() {
		divisionBenchmarkDecimal, divisionBenchmarkRemainder, errDivisionBenchmark = x.QuoRem(y)
	}
}

func BenchmarkQuoRemOversizedDivisor(b *testing.B) {
	x := New(123, -1)
	y := New(1_000, -1_000_000_000)
	b.ReportAllocs()
	for b.Loop() {
		divisionBenchmarkDecimal, divisionBenchmarkRemainder, errDivisionBenchmark = x.QuoRem(y)
	}
}

func BenchmarkTerminatingDivisionShortcut(b *testing.B) {
	const precision = 34
	x := MustParse("12345678901234567890.123456788")
	y := FromInt(8)
	want, wantErr := (Context{Precision: precision, Rounding: HalfEven}).Div(x, y)
	if wantErr != nil {
		b.Fatal(wantErr)
	}
	dividend, dividendScale := decimalParts(x)
	divisor, divisorScale := decimalParts(y)
	preferredScale, ok := subtractScales(dividendScale, divisorScale)
	if !ok {
		b.Fatal("preferred scale overflow")
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
	generalScale, err := divisionTargetScale(dividendScale, divisorScale, ratioExponent, precision)
	if err != nil {
		b.Fatal(err)
	}

	b.Run("shortcut", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			targetScale := generalScale
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
				HalfEven,
			)
			if err != nil {
				errDivisionBenchmark = err
				continue
			}
			if exact && targetScale > preferredScale && hasTrailingDecimalZero(&quotient) {
				targetScale = removeTrailingDecimalZeros(&quotient, targetScale, preferredScale)
			}
			divisionBenchmarkDecimal, errDivisionBenchmark = roundCoefficientToPrecision(&quotient, targetScale, precision, HalfEven)
		}
		if errDivisionBenchmark != nil || !divisionBenchmarkDecimal.SameRepresentation(want) {
			b.Fatalf("shortcut result = %s, %v; want %s", divisionBenchmarkDecimal, errDivisionBenchmark, want)
		}
	})

	b.Run("general_scale", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			targetScale := generalScale
			var quotient big.Int
			exact, err := divideCoefficientAtScale(
				&quotient,
				scaledCoefficient{coefficient: dividend, scale: dividendScale},
				scaledCoefficient{coefficient: divisor, scale: divisorScale},
				targetScale,
				HalfEven,
			)
			if err != nil {
				errDivisionBenchmark = err
				continue
			}
			if exact && targetScale > preferredScale && hasTrailingDecimalZero(&quotient) {
				targetScale = removeTrailingDecimalZeros(&quotient, targetScale, preferredScale)
			}
			divisionBenchmarkDecimal, errDivisionBenchmark = roundCoefficientToPrecision(&quotient, targetScale, precision, HalfEven)
		}
		if errDivisionBenchmark != nil || !divisionBenchmarkDecimal.SameRepresentation(want) {
			b.Fatalf("general result = %s, %v; want %s", divisionBenchmarkDecimal, errDivisionBenchmark, want)
		}
	})
}
