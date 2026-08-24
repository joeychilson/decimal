package decimal

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
)

func TestAdd_PreservesPreferredScale(t *testing.T) {
	if got := MustParse("1.2").Add(MustParse("3.40")); got.String() != "4.60" {
		t.Fatalf("Add = %s, want 4.60", got)
	}
}

func TestAdd_HandlesExtremeScaleShortcuts(t *testing.T) {
	small := New(1, Scale(math.MaxInt64))
	if got := New(0, Scale(math.MinInt64)).Add(small); !got.SameRepresentation(small) {
		t.Fatalf("extreme zero addition = coefficient %s, scale %d", got.Coefficient(), got.Scale())
	}
}

func TestAdd_MatchesBigRatForRandomInputs(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 2_000 {
		x := New(int64(rng.Uint64()), Scale(int64(rng.Uint64()%17)-8))
		y := New(int64(rng.Uint64()), Scale(int64(rng.Uint64()%17)-8))
		if got, want := x.Add(y).BigRat(), new(big.Rat).Add(x.BigRat(), y.BigRat()); got.Cmp(want) != 0 {
			t.Fatalf("add mismatch: %s + %s = %s, want %s", x, y, got, want)
		}
	}
}

func TestSub_PreservesPreferredScale(t *testing.T) {
	if got := MustParse("3.40").Sub(MustParse("1.2")); got.String() != "2.20" {
		t.Fatalf("Sub = %s, want 2.20", got)
	}
}

func TestSub_MatchesBigRatForRandomInputs(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 2_000 {
		x := New(int64(rng.Uint64()), Scale(int64(rng.Uint64()%17)-8))
		y := New(int64(rng.Uint64()), Scale(int64(rng.Uint64()%17)-8))
		if got, want := x.Sub(y).BigRat(), new(big.Rat).Sub(x.BigRat(), y.BigRat()); got.Cmp(want) != 0 {
			t.Fatalf("sub mismatch: %s - %s = %s, want %s", x, y, got, want)
		}
	}
}

func TestMul_PreservesPreferredScale(t *testing.T) {
	if got, err := MustParse("1.20").Mul(MustParse("3.0")); err != nil || got.String() != "3.600" {
		t.Fatalf("Mul = %s, %v; want 3.600", got, err)
	}
}

func TestMultiplication_HandlesScaleRangeBoundaries(t *testing.T) {
	maximumScale := Scale(math.MaxInt64)
	minimumScale := Scale(math.MinInt64)
	for _, test := range []struct {
		name       string
		x, y, want Decimal
	}{
		{"above maximum scale", New(10, maximumScale), New(1, 1), New(1, maximumScale)},
		{"negative above maximum scale", New(-10, maximumScale), New(1, 1), New(-1, maximumScale)},
		{"below minimum scale", New(1, minimumScale), New(1, -1), New(10, minimumScale)},
		{"zero above maximum scale", New(0, maximumScale), New(1, 1), New(0, maximumScale)},
		{"zero below minimum scale", New(0, minimumScale), New(1, -1), New(0, minimumScale)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.x.Mul(test.y)
			if err != nil || !got.SameRepresentation(test.want) {
				t.Fatalf("Mul = coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), err, test.want.Coefficient(), test.want.Scale())
			}
		})
	}

	if got, err := New(1, maximumScale).Mul(New(1, 1)); !errors.Is(err, ErrRange) || !got.SameRepresentation(Decimal{}) {
		t.Fatalf("unrepresentable Mul = %s, %v; want zero and ErrRange", got, err)
	}
}

func TestMul_MatchesBigRatForRandomInputs(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 2_000 {
		x := New(int64(rng.Uint64()), Scale(int64(rng.Uint64()%17)-8))
		y := New(int64(rng.Uint64()), Scale(int64(rng.Uint64()%17)-8))
		gotProduct, err := x.Mul(y)
		if err != nil {
			t.Fatalf("Mul(%s, %s): %v", x, y, err)
		}
		if got, want := gotProduct.BigRat(), new(big.Rat).Mul(x.BigRat(), y.BigRat()); got.Cmp(want) != 0 {
			t.Fatalf("mul mismatch: %s * %s = %s, want %s", x, y, got, want)
		}
	}
}

func TestNeg_PreservesScale(t *testing.T) {
	if got := MustParse("0.00").Neg(); got.String() != "0.00" {
		t.Fatalf("Neg = %s, want 0.00", got)
	}
}

func TestAbs_PreservesScale(t *testing.T) {
	if got := MustParse("-1.20").Abs(); got.String() != "1.20" {
		t.Fatalf("Abs = %s, want 1.20", got)
	}
}

func ExampleDecimal_Add() {
	measurement := MustParse("1.20")
	adjustment := MustParse("0.035")

	fmt.Println(measurement.Add(adjustment))
	// Output: 1.235
}

func ExampleDecimal_Mul() {
	price := MustParse("19.99")
	quantity := FromInt(3)
	subtotal, err := price.Mul(quantity)
	if err != nil {
		fmt.Println("multiply:", err)
		return
	}

	fmt.Println(subtotal)
	// Output: 59.97
}

var (
	arithmeticBenchmarkDecimal Decimal
	errArithmeticBenchmark     error
)

func BenchmarkAdd(b *testing.B) {
	x := MustParse("12345678901234567890.123456789")
	y := MustParse("98765432109876543210.987654321")
	b.ReportAllocs()
	for b.Loop() {
		arithmeticBenchmarkDecimal = x.Add(y)
	}
}

func BenchmarkAddDifferentScales(b *testing.B) {
	x := MustParse("12345678901234567890.123")
	y := MustParse("98765432109876543210.987654321")
	b.ReportAllocs()
	for b.Loop() {
		arithmeticBenchmarkDecimal = x.Add(y)
	}
}

func BenchmarkMul(b *testing.B) {
	x := MustParse("12345678901234567890.123456789")
	y := MustParse("98765432109876543210.987654321")
	b.ReportAllocs()
	for b.Loop() {
		arithmeticBenchmarkDecimal, errArithmeticBenchmark = x.Mul(y)
	}
}

func BenchmarkNegScaledZero(b *testing.B) {
	zero := MustParse("0.000000000")
	b.ReportAllocs()
	for b.Loop() {
		arithmeticBenchmarkDecimal = zero.Neg()
	}
}

func BenchmarkAbsPositive(b *testing.B) {
	positive := MustParse("12345678901234567890.123456789")
	b.ReportAllocs()
	for b.Loop() {
		arithmeticBenchmarkDecimal = positive.Abs()
	}
}
