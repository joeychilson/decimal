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
