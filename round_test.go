package decimal

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"testing"
)

func TestRoundingModes_ApplyDocumentedDirections(t *testing.T) {
	tests := []struct {
		mode          RoundingMode
		positiveTie   string
		negativeTie   string
		positiveAbove string
		negativeAbove string
	}{
		{HalfEven, "2", "-2", "3", "-3"},
		{HalfUp, "3", "-3", "3", "-3"},
		{HalfDown, "2", "-2", "3", "-3"},
		{TowardZero, "2", "-2", "2", "-2"},
		{AwayFromZero, "3", "-3", "3", "-3"},
		{Floor, "2", "-3", "2", "-3"},
		{Ceiling, "3", "-2", "3", "-2"},
		{ZeroFiveUp, "2", "-2", "2", "-2"},
	}
	for _, test := range tests {
		t.Run("rounds with "+test.mode.String(), func(t *testing.T) {
			inputs := []struct{ input, want string }{
				{"2.5", test.positiveTie},
				{"-2.5", test.negativeTie},
				{"2.51", test.positiveAbove},
				{"-2.51", test.negativeAbove},
			}
			for _, input := range inputs {
				got, err := MustParse(input.input).Rescale(0, test.mode)
				if err != nil || got.String() != input.want {
					t.Errorf("Rescale(%s, %s) = %s, %v; want %s", input.input, test.mode, got, err, input.want)
				}
			}
		})
	}
	if _, err := MustParse("1.01").Rescale(1, Exact); !errors.Is(err, ErrInexact) {
		t.Fatalf("Exact rescale error = %v", err)
	}
	if got, err := MustParse("1.00").Rescale(1, Exact); err != nil || got.String() != "1.0" {
		t.Fatalf("exact zero discard = %s, %v", got, err)
	}
	if got, err := MustParse("9.99").Round(2, HalfEven); err != nil || got.String() != "10" {
		t.Fatalf("Round carry = %s, %v", got, err)
	}
	unlimited := MustParse("1.2300")
	if got, err := unlimited.Round(0, Exact); err != nil || !got.SameRepresentation(unlimited) {
		t.Fatalf("unlimited Round = %s, %v; want %s", got, err, unlimited)
	}
	for _, test := range []struct {
		input, want string
	}{
		{"10.01", "11"},
		{"-10.01", "-11"},
		{"15.01", "16"},
		{"-15.01", "-16"},
		{"14.99", "14"},
		{"-14.99", "-14"},
		{"0.001", "1"},
		{"-0.001", "-1"},
		{"10.00", "10"},
	} {
		if got, err := MustParse(test.input).Rescale(0, ZeroFiveUp); err != nil || got.String() != test.want {
			t.Errorf("Rescale(%s, ZeroFiveUp) = %s, %v; want %s", test.input, got, err, test.want)
		}
	}
}

func TestErrorsAndRoundingMode_ExposeDocumentedFailures(t *testing.T) {
	if got, want := ErrInexact.Error(), "inexact result"; got != want {
		t.Fatalf("ErrInexact.Error() = %q, want %q", got, want)
	}
	if got := RoundingMode(99).String(); got != "RoundingMode(99)" {
		t.Fatalf("invalid rounding String = %q", got)
	}
}

func TestOperations_RejectInvalidRoundingModes(t *testing.T) {
	mode := RoundingMode(255)
	value := FromInt(1)
	tests := []struct {
		name string
		fn   func() error
	}{
		{"rescale", func() error { _, err := value.Rescale(0, mode); return err }},
		{"round", func() error { _, err := value.Round(0, mode); return err }},
	}
	for _, test := range tests {
		t.Run("rejects an invalid mode for "+test.name, func(t *testing.T) {
			if err := test.fn(); !errors.Is(err, ErrInvalidRoundingMode) {
				t.Fatalf("error = %v, want ErrInvalidRoundingMode", err)
			}
		})
	}
}

func TestRescale_HandlesExtremeScaleShortcuts(t *testing.T) {
	if got, err := FromInt(1).Rescale(Scale(math.MinInt64), HalfEven); err != nil || !got.IsZero() || got.Scale() != Scale(math.MinInt64) {
		t.Fatalf("extreme HalfEven rescale = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	if got, err := FromInt(1).Rescale(Scale(math.MinInt64), AwayFromZero); err != nil || got.Coefficient().Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("extreme AwayFromZero rescale = coefficient %s, %v", got.Coefficient(), err)
	}
	if got, err := FromInt(-1).Rescale(Scale(math.MinInt64), Floor); err != nil || got.Coefficient().Cmp(big.NewInt(-1)) != 0 {
		t.Fatalf("extreme Floor rescale = coefficient %s, %v", got.Coefficient(), err)
	}
}

func TestRoundToMultiple_AppliesRoundingModesAndPreservesIncrementScale(t *testing.T) {
	tests := []struct {
		mode         RoundingMode
		wantPositive string
		wantNegative string
	}{
		{HalfEven, "3.40", "-3.40"},
		{HalfUp, "3.45", "-3.45"},
		{HalfDown, "3.40", "-3.40"},
		{TowardZero, "3.40", "-3.40"},
		{AwayFromZero, "3.45", "-3.45"},
		{Floor, "3.40", "-3.45"},
		{Ceiling, "3.45", "-3.40"},
		{ZeroFiveUp, "3.40", "-3.40"},
	}
	increment := MustParse("0.05")
	for _, test := range tests {
		t.Run(test.mode.String(), func(t *testing.T) {
			positive, positiveErr := MustParse("3.425").RoundToMultiple(increment, test.mode)
			if positiveErr != nil || positive.String() != test.wantPositive {
				t.Errorf("positive result = %s, %v; want %s", positive, positiveErr, test.wantPositive)
			}
			negative, negativeErr := MustParse("-3.425").RoundToMultiple(increment, test.mode)
			if negativeErr != nil || negative.String() != test.wantNegative {
				t.Errorf("negative result = %s, %v; want %s", negative, negativeErr, test.wantNegative)
			}
		})
	}

	result, err := MustParse("3.4500").RoundToMultiple(MustParse("0.050"), Exact)
	if err != nil || result.String() != "3.450" {
		t.Fatalf("exact result = %s, %v; want 3.450", result, err)
	}
	zero, err := (Decimal{}).RoundToMultiple(MustParse("0.050"), HalfEven)
	if err != nil || zero.String() != "0.000" {
		t.Fatalf("zero result = %s, %v; want 0.000", zero, err)
	}
}

func TestRoundToMultiple_ReportsInvalidInputs(t *testing.T) {
	value := MustParse("3.43")
	if _, err := value.RoundToMultiple(MustParse("0.05"), Exact); !errors.Is(err, ErrInexact) {
		t.Fatalf("Exact error = %v, want ErrInexact", err)
	}
	for _, increment := range []Decimal{{}, MustParse("-0.05")} {
		if _, err := value.RoundToMultiple(increment, HalfEven); !errors.Is(err, ErrInvalidOperation) {
			t.Errorf("increment %s error = %v, want ErrInvalidOperation", increment, err)
		}
	}
	if _, err := value.RoundToMultiple(MustParse("0.05"), RoundingMode(255)); !errors.Is(err, ErrInvalidRoundingMode) {
		t.Fatalf("invalid mode error = %v, want ErrInvalidRoundingMode", err)
	}
}

func ExampleDecimal_RoundToMultiple() {
	amount, err := MustParse("3.43").RoundToMultiple(MustParse("0.05"), HalfUp)
	if err != nil {
		fmt.Println("round:", err)
		return
	}

	fmt.Println(amount)
	// Output: 3.45
}

func TestRound_MatchesRationalOracleAcrossChunkBoundaries(t *testing.T) {
	chunkDigits := decimalWordDigits
	modes := []RoundingMode{HalfEven, HalfUp, HalfDown, TowardZero, AwayFromZero, Floor, Ceiling, ZeroFiveUp, Exact}
	for _, remove := range []int{
		chunkDigits - 1,
		chunkDigits,
		chunkDigits + 1,
		2*chunkDigits + 1,
		4 * chunkDigits,
		4*chunkDigits + 1,
		len(smallPowersOfTen) - 1,
		len(smallPowersOfTen),
		len(smallPowersOfTen) + 1,
		512,
		2*(len(smallPowersOfTen)-1) + chunkDigits,
		2*(len(smallPowersOfTen)-1) + chunkDigits + 1,
		1_000,
	} {
		for _, discarded := range []string{
			strings.Repeat("0", remove),
			"4" + strings.Repeat("9", remove-1),
			"5" + strings.Repeat("0", remove-1),
			"5" + strings.Repeat("0", remove-2) + "1",
		} {
			for _, sign := range []string{"", "-"} {
				coefficient, _ := new(big.Int).SetString(sign+"12"+discarded, 10)
				input := NewBig(coefficient, 0)
				for _, mode := range modes {
					got, gotErr := input.Round(2, mode)
					want, exact := roundRatToPrecision(input.BigRat(), 0, 2, mode)
					if mode == Exact && !exact {
						if !errors.Is(gotErr, ErrInexact) {
							t.Fatalf("Round(%s, 2, Exact) error = %v, want ErrInexact", input, gotErr)
						}
						continue
					}
					if gotErr != nil || !got.SameRepresentation(want) {
						t.Fatalf("Round(%s, 2, %s) = %s [scale %d], %v; want %s [scale %d]", input, mode, got, got.Scale(), gotErr, want, want.Scale())
					}
				}
			}
		}
	}
}

func TestIntegralRounding_UsesDocumentedDirections(t *testing.T) {
	if got := MustParse("-1.9").Trunc(); got.String() != "-1" {
		t.Fatalf("Trunc = %s", got)
	}
	if got := MustParse("-1.1").Floor(); got.String() != "-2" {
		t.Fatalf("Floor = %s", got)
	}
	if got := MustParse("-1.1").Ceil(); got.String() != "-1" {
		t.Fatalf("Ceil = %s", got)
	}
}

// oracleRoundingIncrement is the independent rounding policy shared by the
// rational and square-root reference calculations. midpointComparison compares
// the discarded magnitude with half a unit in the last retained place.
func oracleRoundingIncrement(mode RoundingMode, sign, midpointComparison int, coefficient *big.Int) bool {
	switch mode {
	case HalfEven:
		if midpointComparison != 0 {
			return midpointComparison > 0
		}
		digits := coefficient.Text(10)
		switch digits[len(digits)-1] {
		case '1', '3', '5', '7', '9':
			return true
		default:
			return false
		}
	case HalfUp:
		return midpointComparison >= 0
	case HalfDown:
		return midpointComparison > 0
	case TowardZero:
		return false
	case AwayFromZero:
		return true
	case Floor:
		return sign < 0
	case Ceiling:
		return sign > 0
	case ZeroFiveUp:
		digits := coefficient.Text(10)
		lastDigit := digits[len(digits)-1]
		return lastDigit == '0' || lastDigit == '5'
	case Exact:
		panic("exact mode reached oracle rounding")
	default:
		panic("invalid rounding mode")
	}
}

func roundRatToScale(value *big.Rat, scale Scale, mode RoundingMode) (Decimal, bool) {
	scaled := new(big.Rat).Set(value)
	if scale >= 0 {
		scaled.Mul(scaled, new(big.Rat).SetInt(oraclePowerOfTen(uint64(scale))))
	} else {
		scaled.Quo(scaled, new(big.Rat).SetInt(oraclePowerOfTen(oracleScaleMagnitude(scale))))
	}
	numerator := scaled.Num()
	denominator := scaled.Denom()
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if remainder.Sign() == 0 {
		return NewBig(quotient, scale), true
	}
	if mode == Exact {
		return Decimal{}, false
	}

	sign := remainder.Sign()
	midpointComparison := 0
	switch mode {
	case HalfEven, HalfUp, HalfDown:
		away := new(big.Int).Add(new(big.Int).Set(quotient), big.NewInt(int64(sign)))
		towardDistance := new(big.Rat).Sub(scaled, new(big.Rat).SetInt(quotient))
		towardDistance.Abs(towardDistance)
		awayDistance := new(big.Rat).Sub(new(big.Rat).SetInt(away), scaled)
		awayDistance.Abs(awayDistance)
		midpointComparison = towardDistance.Cmp(awayDistance)
	case TowardZero, AwayFromZero, Floor, Ceiling, ZeroFiveUp, Exact:
	default:
		panic("invalid rounding mode")
	}
	if oracleRoundingIncrement(mode, sign, midpointComparison, quotient) {
		quotient.Add(quotient, big.NewInt(int64(sign)))
	}
	return NewBig(quotient, scale), false
}

func roundRatToPrecision(value *big.Rat, preferredScale Scale, precision uint, mode RoundingMode) (Decimal, bool) {
	if value.Sign() == 0 {
		return New(0, preferredScale), true
	}

	numerator := value.Num()
	denominator := value.Denom()
	exponent := int64(oracleDecimalDigitCount(numerator) - oracleDecimalDigitCount(denominator))
	var scaled big.Int
	if exponent >= 0 {
		scaled.Mul(denominator, oraclePowerOfTen(uint64(exponent)))
		if numerator.CmpAbs(&scaled) < 0 {
			exponent--
		}
	} else {
		scaled.Mul(numerator, oraclePowerOfTen(uint64(-exponent)))
		if scaled.CmpAbs(denominator) < 0 {
			exponent--
		}
	}

	scale := Scale(int64(precision-1) - exponent)
	result, exact := roundRatToScale(value, scale, mode)
	if mode == Exact && !exact {
		return Decimal{}, false
	}
	coefficient := result.Coefficient()
	if exact {
		for scale > preferredScale && oracleHasTrailingDecimalZero(coefficient) {
			coefficient.Quo(coefficient, big.NewInt(10))
			scale--
		}
	}
	for uint(oracleDecimalDigitCount(coefficient)) > precision {
		coefficient.Quo(coefficient, big.NewInt(10))
		scale--
	}
	return NewBig(coefficient, scale), exact
}

func oraclePowerOfTen(exponent uint64) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), new(big.Int).SetUint64(exponent), nil)
}

func oracleScaleMagnitude(scale Scale) uint64 {
	if scale >= 0 {
		return uint64(scale)
	}
	return uint64(-(scale + 1)) + 1
}

func oracleDecimalDigitCount(value *big.Int) int {
	return len(new(big.Int).Abs(new(big.Int).Set(value)).String())
}

func oracleHasTrailingDecimalZero(value *big.Int) bool {
	return new(big.Int).Rem(new(big.Int).Set(value), big.NewInt(10)).Sign() == 0
}

var (
	roundBenchmarkDecimal Decimal
	errRoundBenchmark     error
)

func BenchmarkRescale(b *testing.B) {
	for _, test := range []struct {
		name  string
		value Decimal
		scale Scale
		mode  RoundingMode
	}{
		{"unchanged", MustParse("123.45"), 2, HalfEven},
		{"zero_scale_change", New(0, 5), 2, HalfEven},
		{"increase_scale", MustParse("123.45"), 6, HalfEven},
		{"exact_decrease", MustParse("123.4500"), 2, Exact},
		{"rounded_decrease", MustParse("123.455"), 2, HalfEven},
		{"discard_beyond_digits", MustParse("0.001"), 0, AwayFromZero},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				roundBenchmarkDecimal, errRoundBenchmark = test.value.Rescale(test.scale, test.mode)
			}
			if errRoundBenchmark != nil {
				b.Fatal(errRoundBenchmark)
			}
		})
	}
}

func BenchmarkRoundToMultiple(b *testing.B) {
	value := MustParse("123.43")
	increment := MustParse("0.05")
	b.ReportAllocs()
	for b.Loop() {
		roundBenchmarkDecimal, errRoundBenchmark = value.RoundToMultiple(increment, HalfEven)
	}
	if errRoundBenchmark != nil {
		b.Fatal(errRoundBenchmark)
	}
}

func BenchmarkRoundChunkBoundary(b *testing.B) {
	chunkDigits := decimalWordDigits
	for _, remove := range []int{chunkDigits, chunkDigits + 1, 4 * chunkDigits, 4*chunkDigits + 1} {
		input := MustParse("12" + "5" + strings.Repeat("0", remove-2) + "1")
		coefficient, scale := decimalParts(input)
		digits := uint(decimalDigitCount(coefficient))
		b.Run(strconv.Itoa(remove), func(b *testing.B) {
			b.Run("optimized", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					roundBenchmarkDecimal, errRoundBenchmark = roundCoefficient(new(big.Int).Set(coefficient), scale, digits, 2, HalfEven)
				}
			})
			b.Run("one_divisor", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					rounded := new(big.Int).Set(coefficient)
					var denominatorStorage, remainder big.Int
					denominator := &denominatorStorage
					if remove < len(smallPowersOfTen) {
						denominator = &smallPowersOfTen[remove]
					} else {
						setPowerOfTen(denominator, uint64(remove))
					}
					rounded.QuoRem(rounded, denominator, &remainder)
					errRoundBenchmark = roundQuotient(rounded, &remainder, denominator, HalfEven)
					roundBenchmarkDecimal = makeDecimal(rounded, Scale(uint64(scale)-uint64(remove)))
				}
			})
		})
	}
}

func BenchmarkRoundLargeDiscard(b *testing.B) {
	for _, remove := range []int{
		len(smallPowersOfTen) - 1,
		len(smallPowersOfTen),
		512,
		2*(len(smallPowersOfTen)-1) + decimalWordDigits,
		2*(len(smallPowersOfTen)-1) + decimalWordDigits + 1,
		1_000,
	} {
		input := MustParse("12" + "5" + strings.Repeat("0", remove-1))
		coefficient, scale := decimalParts(input)
		digits := uint(decimalDigitCount(coefficient))
		b.Run(strconv.Itoa(remove), func(b *testing.B) {
			b.Run("optimized", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					roundBenchmarkDecimal, errRoundBenchmark = roundCoefficient(new(big.Int).Set(coefficient), scale, digits, 2, HalfEven)
				}
			})
			b.Run("one_divisor", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					rounded := new(big.Int).Set(coefficient)
					var denominatorStorage, remainder big.Int
					denominator := &denominatorStorage
					if remove < len(smallPowersOfTen) {
						denominator = &smallPowersOfTen[remove]
					} else {
						setPowerOfTen(denominator, uint64(remove))
					}
					rounded.QuoRem(rounded, denominator, &remainder)
					errRoundBenchmark = roundQuotient(rounded, &remainder, denominator, HalfEven)
					roundBenchmarkDecimal = makeDecimal(rounded, Scale(uint64(scale)-uint64(remove)))
				}
			})
		})
	}
}
