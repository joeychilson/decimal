package decimal

import (
	"errors"
	"math"
	"math/big"
	"testing"
)

func TestPower_ReturnsExactResults(t *testing.T) {
	if got, err := MustParse("1.20").Pow(2); err != nil || got.String() != "1.4400" {
		t.Fatalf("1.20^2 = %s, %v", got, err)
	}
	if got, err := FromInt(2).Pow(-3); err != nil || got.String() != "0.125" {
		t.Fatalf("2^-3 = %s, %v", got, err)
	}
	if _, err := FromInt(3).Pow(-1); !errors.Is(err, ErrInexact) {
		t.Fatalf("3^-1 error = %v", err)
	}
	for _, test := range []struct {
		name     string
		base     Decimal
		exponent int64
		want     Decimal
	}{
		{"above maximum scale", New(10, Scale(math.MaxInt64/2+1)), 2, New(10, Scale(math.MaxInt64))},
		{"negative above maximum scale", New(-10, Scale(math.MaxInt64/3+1)), 3, New(-10, Scale(math.MaxInt64))},
		{"below minimum scale", New(1, Scale(math.MinInt64/2-1)), 2, New(100, Scale(math.MinInt64))},
		{"negative power with unrepresentable positive intermediate", New(1, Scale(math.MaxInt64/2+1)), -2, New(1, Scale(math.MinInt64))},
		{"zero above maximum scale", New(0, Scale(math.MaxInt64/2+1)), 2, New(0, Scale(math.MaxInt64))},
		{"zero below minimum scale", New(0, Scale(math.MinInt64/2-1)), 2, New(0, Scale(math.MinInt64))},
	} {
		t.Run("power matches the oracle for "+test.name, func(t *testing.T) {
			got, err := test.base.Pow(test.exponent)
			if err != nil || !got.SameRepresentation(test.want) {
				t.Fatalf("Pow = coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), err, test.want.Coefficient(), test.want.Scale())
			}
		})
	}
	unrepresentablePower := New(1, Scale(math.MaxInt64/2+1))
	if _, err := unrepresentablePower.Pow(2); !errors.Is(err, ErrRange) {
		t.Fatalf("unrepresentable power error = %v", err)
	}
	if _, err := New(3, Scale(math.MaxInt64/2+1)).Pow(-2); !errors.Is(err, ErrInexact) {
		t.Fatalf("non-terminating negative power error = %v, want ErrInexact", err)
	}
	extremeZero := New(0, Scale(math.MaxInt64))
	if _, err := extremeZero.Pow(-2); !errors.Is(err, ErrDivisionByZero) {
		t.Fatalf("extreme zero negative power error = %v", err)
	}
}

func TestPower_MatchesRepeatedMultiplicationAndRationals(t *testing.T) {
	for coefficient := int64(-10); coefficient <= 10; coefficient++ {
		for scale := Scale(-2); scale <= 2; scale++ {
			base := New(coefficient, scale)
			want := FromInt(1)
			for exponent := range int64(9) {
				if exponent > 0 {
					next, err := want.Mul(base)
					if err != nil {
						t.Fatalf("Mul(%s, %s): %v", want, base, err)
					}
					want = next
				}
				got, err := base.Pow(exponent)
				if err != nil || !got.SameRepresentation(want) {
					t.Fatalf("Pow(%s, %d) = %s [scale %d], %v; want %s [scale %d]", base, exponent, got, got.Scale(), err, want, want.Scale())
				}
			}
		}
	}

	for _, coefficient := range []int64{-50, -40, -25, -10, -8, -5, -4, -2, -1, 1, 2, 4, 5, 8, 10, 25, 40, 50} {
		for scale := Scale(-2); scale <= 2; scale++ {
			base := New(coefficient, scale)
			factor := base.BigRat()
			power := new(big.Rat).SetInt64(1)
			for exponent := int64(1); exponent <= 6; exponent++ {
				power.Mul(power, factor)
				want := new(big.Rat).Inv(power)
				got, err := base.Pow(-exponent)
				if err != nil || got.BigRat().Cmp(want) != 0 {
					t.Fatalf("Pow(%s, %d) = %s, %v; want %s", base, -exponent, got, err, want)
				}
			}
		}
	}

	for _, coefficient := range []int64{-11, -9, -7, -6, -3, 3, 6, 7, 9, 11} {
		for scale := Scale(-2); scale <= 2; scale++ {
			base := New(coefficient, scale)
			for exponent := int64(1); exponent <= 4; exponent++ {
				if _, err := base.Pow(-exponent); !errors.Is(err, ErrInexact) {
					t.Fatalf("Pow(%s, %d) error = %v, want ErrInexact", base, -exponent, err)
				}
			}
		}
	}
}

func TestContextPower_MatchesExactPowerRounding(t *testing.T) {
	modes := [...]RoundingMode{HalfEven, HalfUp, HalfDown, TowardZero, AwayFromZero, Floor, Ceiling, ZeroFiveUp, Exact}
	for coefficient := int64(-17); coefficient <= 17; coefficient++ {
		for scale := Scale(-2); scale <= 2; scale++ {
			base := New(coefficient, scale)
			for exponent := range int64(11) {
				exact, err := base.Pow(exponent)
				if err != nil {
					t.Fatalf("Pow(%s, %d): %v", base, exponent, err)
				}
				for precision := uint(1); precision <= 8; precision++ {
					for _, mode := range modes {
						ctx := Context{Precision: precision, Rounding: mode}
						want, wantErr := ctx.Round(exact)
						got, gotErr := ctx.Pow(base, exponent)
						if wantErr != nil {
							if !errors.Is(gotErr, wantErr) {
								t.Fatalf("Context{%d, %s}.Pow(%s, %d) error = %v; want %v", precision, mode, base, exponent, gotErr, wantErr)
							}
							continue
						}
						if gotErr != nil || !got.SameRepresentation(want) {
							t.Fatalf("Context{%d, %s}.Pow(%s, %d) = %s [scale %d], %v; want %s [scale %d]", precision, mode, base, exponent, got, got.Scale(), gotErr, want, want.Scale())
						}
					}
				}
			}
		}
	}
}

func TestContextPower_NegativeExponentsMatchBigRat(t *testing.T) {
	modes := [...]RoundingMode{HalfEven, HalfUp, HalfDown, TowardZero, AwayFromZero, Floor, Ceiling, ZeroFiveUp, Exact}
	for coefficient := int64(-12); coefficient <= 12; coefficient++ {
		if coefficient == 0 {
			continue
		}
		for scale := Scale(-2); scale <= 2; scale++ {
			base := New(coefficient, scale)
			factor := base.BigRat()
			for _, exponent := range [...]int64{1, 2, 5, 31, 67} {
				powerNumerator := new(big.Int).Exp(factor.Num(), big.NewInt(exponent), nil)
				powerDenominator := new(big.Int).Exp(factor.Denom(), big.NewInt(exponent), nil)
				exact := new(big.Rat).SetFrac(powerDenominator, powerNumerator)
				preferredScale := scale * Scale(-exponent)
				for precision := uint(1); precision <= 8; precision++ {
					for _, mode := range modes {
						want, resultExact := roundRatToPrecision(exact, preferredScale, precision, mode)
						got, gotErr := (Context{Precision: precision, Rounding: mode}).Pow(base, -exponent)
						if mode == Exact && !resultExact {
							if !errors.Is(gotErr, ErrInexact) {
								t.Fatalf("Context{%d, %s}.Pow(%s, %d) error = %v; want ErrInexact", precision, mode, base, -exponent, gotErr)
							}
							continue
						}
						if gotErr != nil || !got.SameRepresentation(want) {
							t.Fatalf("Context{%d, %s}.Pow(%s, %d) = %s [scale %d], %v; want %s [scale %d]", precision, mode, base, -exponent, got, got.Scale(), gotErr, want, want.Scale())
						}
					}
				}
			}
		}
	}
}

func TestContextPower_MatchesExactRoundingForLargeCoefficients(t *testing.T) {
	trailingZeros := new(big.Int).Mul(big.NewInt(12_345), setPowerOfTen(new(big.Int), 100))
	bases := [...]Decimal{
		MustParse("123456789012345678901234567890123456789"),
		MustParse("-987654321098765432109876543210987654321"),
		NewBig(trailingZeros, 0),
	}
	modes := [...]RoundingMode{HalfEven, HalfUp, HalfDown, TowardZero, AwayFromZero, Floor, Ceiling, ZeroFiveUp, Exact}
	precisions := [...]uint{1, 2, 5, 12, 20, 40}
	for _, base := range bases {
		for _, exponent := range [...]int64{2, 3, 7} {
			exact, err := base.Pow(exponent)
			if err != nil {
				t.Fatalf("Pow(%s, %d): %v", base, exponent, err)
			}
			for _, precision := range precisions {
				for _, mode := range modes {
					ctx := Context{Precision: precision, Rounding: mode}
					want, wantErr := ctx.Round(exact)
					got, gotErr := ctx.Pow(base, exponent)
					if wantErr != nil {
						if !errors.Is(gotErr, wantErr) {
							t.Fatalf("Context{%d, %s}.Pow(%s, %d) error = %v; want %v", precision, mode, base, exponent, gotErr, wantErr)
						}
						continue
					}
					if gotErr != nil || !got.SameRepresentation(want) {
						t.Fatalf("Context{%d, %s}.Pow(%s, %d) = %s [scale %d], %v; want %s [scale %d]", precision, mode, base, exponent, got, got.Scale(), gotErr, want, want.Scale())
					}
				}
			}
		}
	}
}

func TestContextPower_BoundsIntermediateCoefficient(t *testing.T) {
	ctx := Context{Precision: 20, Rounding: HalfEven}
	base := FromInt(2)
	var got Decimal
	var gotErr error
	measurement := testing.Benchmark(func(b *testing.B) {
		b.Helper()
		b.ReportAllocs()
		for b.Loop() {
			got, gotErr = ctx.Pow(base, 1_000_000)
		}
	})

	wantCoefficient, ok := new(big.Int).SetString("99006562292958982507", 10)
	if !ok {
		t.Fatal("invalid expected coefficient")
	}
	want := NewBig(wantCoefficient, -301_010)
	if gotErr != nil || !got.SameRepresentation(want) {
		t.Fatalf("Context.Pow = coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), gotErr, want.Coefficient(), want.Scale())
	}
	if allocated := measurement.AllocedBytesPerOp(); allocated > 64<<10 {
		t.Fatalf("Context.Pow allocated %d bytes; want at most %d", allocated, 64<<10)
	}
}

func TestContextPower_FitsScaleBelowMinimumWithinPrecision(t *testing.T) {
	base := New(1, Scale(math.MinInt64/2-1))
	got, err := (Context{Precision: 3, Rounding: HalfEven}).Pow(base, 2)
	want := New(100, Scale(math.MinInt64))
	if err != nil || !got.SameRepresentation(want) {
		t.Fatalf("Context.Pow = coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), err, want.Coefficient(), want.Scale())
	}
	if _, err := (Context{Precision: 2, Rounding: HalfEven}).Pow(base, 2); !errors.Is(err, ErrRange) {
		t.Fatalf("Context.Pow with insufficient precision error = %v; want ErrRange", err)
	}
}

func TestContextPower_RoundsNegativePowerAcrossScaleBoundary(t *testing.T) {
	base := New(3, Scale(math.MaxInt64/2+1))
	got, err := (Context{Precision: 5, Rounding: HalfEven}).Pow(base, -2)
	want := New(11_111, Scale(math.MinInt64+5))
	if err != nil || !got.SameRepresentation(want) {
		t.Fatalf("Context.Pow = coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), err, want.Coefficient(), want.Scale())
	}
}

func TestContextPower_BoundsNegativePowerIntermediate(t *testing.T) {
	ctx := Context{Precision: 20, Rounding: HalfEven}
	for _, test := range []struct {
		name        string
		base        Decimal
		coefficient string
		scale       Scale
	}{
		{"terminating reciprocal", FromInt(2), "10100340591980302247", 301_049},
		{"non-terminating reciprocal", FromInt(3), "55626320991571288659", 477_141},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got Decimal
			var gotErr error
			measurement := testing.Benchmark(func(b *testing.B) {
				b.Helper()
				b.ReportAllocs()
				for b.Loop() {
					got, gotErr = ctx.Pow(test.base, -1_000_000)
				}
			})

			wantCoefficient, ok := new(big.Int).SetString(test.coefficient, 10)
			if !ok {
				t.Fatal("invalid expected coefficient")
			}
			want := NewBig(wantCoefficient, test.scale)
			if gotErr != nil || !got.SameRepresentation(want) {
				t.Fatalf("Context.Pow = coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), gotErr, want.Coefficient(), want.Scale())
			}
			if allocated := measurement.AllocedBytesPerOp(); allocated > 64<<10 {
				t.Fatalf("Context.Pow allocated %d bytes; want at most %d", allocated, 64<<10)
			}
		})
	}
}

var (
	powerBenchmarkDecimal Decimal
	errPowerBenchmark     error
)

func BenchmarkPow(b *testing.B) {
	x := MustParse("1.0000000000000000001")
	b.ReportAllocs()
	for b.Loop() {
		powerBenchmarkDecimal, errPowerBenchmark = x.Pow(12)
	}
}
