package decimal

import (
	"hash/maphash"
	"math"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestConstructors_PreserveValuesScalesAndOwnership(t *testing.T) {
	if got := (Decimal{}).String(); got != "0" {
		t.Fatalf("zero value String = %q", got)
	}
	if got := New(-12, -2).String(); got != "-12e2" {
		t.Fatalf("negative scale String = %q", got)
	}

	coefficient := big.NewInt(123)
	copied := NewBig(coefficient, 2)
	coefficient.SetInt64(999)
	if copied.String() != "1.23" {
		t.Fatal("NewBig retained its coefficient argument")
	}
	extracted := copied.Coefficient()
	extracted.SetInt64(999)
	if copied.String() != "1.23" {
		t.Fatal("Coefficient exposed mutable internal storage")
	}
	other := copied
	if err := other.UnmarshalText([]byte("9.99")); err != nil || copied.String() != "1.23" {
		t.Fatalf("decoding a copy mutated the original: %s, %v", copied, err)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("NewBig did not panic")
			}
		}()
		NewBig(nil, 0)
	}()
}

func TestCanonical_RemovesOnlyInsignificantTrailingZeros(t *testing.T) {
	for _, test := range []struct {
		name        string
		input, want Decimal
		canonical   bool
	}{
		{"zero value", Decimal{}, Decimal{}, true},
		{"scaled zero", New(0, Scale(math.MaxInt64)), Decimal{}, false},
		{"already canonical", New(123, 2), New(123, 2), true},
		{"negative trailing zeros", New(-12_000, 4), New(-12, 1), false},
		{"integral trailing zeros", New(12_000, 0), New(12, -3), false},
		{"unsigned coefficient", MustParse("10000000000000000000"), New(1, -19), false},
		{"wide negative coefficient", MustParse("-10000000000000000000"), New(-1, -19), false},
		{"minimum scale", New(120, Scale(math.MinInt64)), New(120, Scale(math.MinInt64)), true},
		{"minimum scale limit", New(1_200, Scale(math.MinInt64+1)), New(120, Scale(math.MinInt64)), false},
		{"thousand trailing zeros", MustParse("123" + strings.Repeat("0", 1_000)), New(123, -1_000), false},
	} {
		t.Run("canonical handles "+test.name, func(t *testing.T) {
			if got := test.input.IsCanonical(); got != test.canonical {
				t.Fatalf("IsCanonical = %v, want %v", got, test.canonical)
			}
			got := test.input.Canonical()
			if !got.SameRepresentation(test.want) {
				t.Fatalf("Canonical = coefficient %s, scale %d; want coefficient %s, scale %d",
					got.Coefficient(), got.Scale(), test.want.Coefficient(), test.want.Scale())
			}
			if !got.Equal(test.input) {
				t.Fatalf("Canonical = %s, want numeric value %s", got, test.input)
			}
			if !got.IsCanonical() || !got.Canonical().SameRepresentation(got) {
				t.Fatal("canonicalization is not idempotent")
			}
		})
	}

	boundary := New(10, Scale(math.MinInt64))
	if got := boundary.Canonical(); !got.SameRepresentation(boundary) || !got.IsCanonical() {
		t.Fatalf("boundary canonical = coefficient %s, scale %d", got.Coefficient(), got.Scale())
	}
}

func TestClassification_ReportsSignScaleAndPrecision(t *testing.T) {
	d := MustParse("-1.20")
	if d.Precision() != 3 || d.Sign() != -1 || !d.IsNegative() || d.IsPositive() || d.IsZero() {
		t.Fatalf("unexpected classification for %s", d)
	}
	if !(Decimal{}).IsZero() || !FromInt(1).IsPositive() {
		t.Fatal("zero or positive classification failed")
	}
	for _, test := range []struct {
		value Decimal
		want  bool
	}{
		{Decimal{}, true},
		{New(0, Scale(math.MaxInt64)), true},
		{New(12, -2), true},
		{MustParse("12.00"), true},
		{MustParse("12.30"), false},
		{MustParse("0.1"), false},
		{MustParse("123456789012345678900.00"), true},
	} {
		if got := test.value.IsInt(); got != test.want {
			t.Errorf("%s.IsInt() = %v, want %v", test.value, got, test.want)
		}
	}

	large := New(1, Scale(math.MinInt64))
	if !large.IsInt() {
		t.Fatal("negative-scale value is not considered integral")
	}
}

func TestDecimal_AllowsConcurrentReads(t *testing.T) {
	x := MustParse("12345678901234567890.1234500")
	y := MustParse("987654321.00001")
	var wait sync.WaitGroup
	for range 32 {
		wait.Go(func() {
			for range 1_000 {
				_ = x.Add(y)
				if _, err := x.Mul(y); err != nil {
					t.Error(err)
					return
				}
				_ = x.Cmp(y)
				_ = x.Canonical()
				_ = x.String()
				var hash maphash.Hash
				NumericHasher{}.Hash(&hash, x)
			}
		})
	}
	wait.Wait()
}

func TestPowerOfTenHelpers_DoNotExposeSharedStorage(t *testing.T) {
	var first, second big.Int
	setPowerOfTen(&first, 3)
	first.SetInt64(-1)
	setPowerOfTen(&second, 3)
	if second.Cmp(big.NewInt(1_000)) != 0 {
		t.Fatalf("cached power changed after caller mutation: %s", &second)
	}

	product := multiplyByPowerOfTen(new(big.Int), big.NewInt(7), 3)
	if product.Cmp(big.NewInt(7_000)) != 0 {
		t.Fatalf("7*10^3 = %s", product)
	}
	product.SetInt64(-1)
	setPowerOfTen(&second, 3)
	if second.Cmp(big.NewInt(1_000)) != 0 {
		t.Fatalf("cached power changed after product mutation: %s", &second)
	}
}

func TestDecimalDigitCount_MatchesFormattedLength(t *testing.T) {
	for _, exponent := range []uint64{0, 1, 2, 18, 19, 20, 34, 127, 255, 256, 1_000, 4_096} {
		var power big.Int
		setPowerOfTen(&power, exponent)
		values := []*big.Int{
			new(big.Int).Sub(new(big.Int).Set(&power), bigOne),
			new(big.Int).Set(&power),
			new(big.Int).Add(new(big.Int).Set(&power), bigOne),
		}
		for _, value := range values {
			for _, candidate := range []*big.Int{value, new(big.Int).Neg(new(big.Int).Set(value))} {
				absolute := new(big.Int).Abs(new(big.Int).Set(candidate))
				want := len(absolute.String())
				if got := decimalDigitCount(candidate); got != want {
					t.Fatalf("digit count for 10^%d neighbor %s = %d, want %d", exponent, candidate, got, want)
				}
			}
		}
	}
}

var (
	benchmarkDecimal    Decimal
	benchmarkComparison int
	benchmarkBool       bool
	benchmarkBigInt     *big.Int
)

func BenchmarkCanonical(b *testing.B) {
	for _, test := range []struct {
		name  string
		value Decimal
	}{
		{"already_canonical", MustParse("123456789012345678901234567")},
		{"small_one_trailing_zero", New(12_340, 1)},
		{"large_one_trailing_zero", MustParse("123456789012345678901234567.0")},
		{"thousand_trailing_zeros", MustParse("123" + strings.Repeat("0", 1_000))},
	} {
		want := test.value.Canonical()
		coefficient, scale := decimalParts(test.value)
		b.Run(test.name, func(b *testing.B) {
			b.Run("optimized", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					benchmarkDecimal = test.value.Canonical()
				}
				if !benchmarkDecimal.SameRepresentation(want) {
					b.Fatalf("optimized result = %s; want %s", benchmarkDecimal, want)
				}
			})
			b.Run("repeated_division", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					result := new(big.Int).Set(coefficient)
					targetScale := scale
					var quotient, remainder big.Int
					for targetScale > Scale(math.MinInt64) {
						quotient.QuoRem(result, bigTen, &remainder)
						if remainder.Sign() != 0 {
							break
						}
						result.Set(&quotient)
						targetScale--
					}
					benchmarkDecimal = makeDecimal(result, targetScale)
				}
				if !benchmarkDecimal.SameRepresentation(want) {
					b.Fatalf("repeated-division result = %s; want %s", benchmarkDecimal, want)
				}
			})
		})
	}
}

func BenchmarkIsCanonical(b *testing.B) {
	for _, test := range []struct {
		name  string
		value Decimal
	}{
		{"odd", MustParse("123456789012345678901234567")},
		{"small_even", FromInt(12_346)},
		{"large_even", MustParse("123456789012345678901234568")},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkBool = test.value.IsCanonical()
			}
		})
	}
}

func BenchmarkDecimalDigitCount(b *testing.B) {
	for _, test := range []struct {
		name        string
		coefficient *big.Int
	}{
		{"uint64", new(big.Int).SetUint64(math.MaxUint64)},
		{"cached_34_digits", setPowerOfTen(new(big.Int), 33)},
		{"uncached_1000_digits", setPowerOfTen(new(big.Int), 999)},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.Run("optimized", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					benchmarkComparison = decimalDigitCount(test.coefficient)
				}
			})
			b.Run("string", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					benchmarkComparison = len(test.coefficient.String())
				}
			})
		})
	}
}

func BenchmarkSetPowerOfTen(b *testing.B) {
	for _, exponent := range []uint64{18, 255} {
		b.Run(strconv.FormatUint(exponent, 10), func(b *testing.B) {
			b.Run("cached", func(b *testing.B) {
				b.ReportAllocs()
				var result big.Int
				for b.Loop() {
					benchmarkBigInt = setPowerOfTen(&result, exponent)
				}
			})
			b.Run("exp", func(b *testing.B) {
				b.ReportAllocs()
				var result, power big.Int
				power.SetUint64(exponent)
				for b.Loop() {
					benchmarkBigInt = result.Exp(bigTen, &power, nil)
				}
			})
		})
	}
}

func BenchmarkMultiplyByPowerOfTen(b *testing.B) {
	coefficient := MustParse("1234567890123456789012345678901234").Coefficient()
	for _, exponent := range []uint64{18, 255} {
		b.Run(strconv.FormatUint(exponent, 10), func(b *testing.B) {
			b.Run("cached", func(b *testing.B) {
				b.ReportAllocs()
				var result big.Int
				for b.Loop() {
					benchmarkBigInt = multiplyByPowerOfTen(&result, coefficient, exponent)
				}
			})
			b.Run("exp", func(b *testing.B) {
				b.ReportAllocs()
				var result, power, exponentValue big.Int
				exponentValue.SetUint64(exponent)
				for b.Loop() {
					power.Exp(bigTen, &exponentValue, nil)
					benchmarkBigInt = result.Mul(coefficient, &power)
				}
			})
		})
	}
}
