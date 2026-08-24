package decimal

import (
	"fmt"
	"hash/maphash"
	"math"
	"math/rand/v2"
	"testing"
)

func TestComparison_MatchesBigRatForRandomInputs(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 2_000 {
		x := New(int64(rng.Uint64()), Scale(int64(rng.Uint64()%17)-8))
		y := New(int64(rng.Uint64()), Scale(int64(rng.Uint64()%17)-8))
		if got := x.Cmp(y); got != x.BigRat().Cmp(y.BigRat()) {
			t.Fatalf("cmp mismatch: %s cmp %s = %d", x, y, got)
		}
	}
}

func TestComparison_HandlesExtremeScales(t *testing.T) {
	large := New(1, Scale(math.MinInt64))
	small := New(1, Scale(math.MaxInt64))
	if large.Cmp(small) <= 0 {
		t.Fatal("extreme-scale comparison is reversed")
	}
	for _, test := range []struct {
		name string
		x, y Decimal
		want int
	}{
		{"minimum-scale overflow", New(1, Scale(math.MinInt64)), New(1, Scale(math.MinInt64+1)), 1},
		{"negative minimum-scale overflow", New(-1, Scale(math.MinInt64)), New(-1, Scale(math.MinInt64+1)), -1},
		{"maximum-scale boundary", New(1, Scale(math.MaxInt64)), New(1, Scale(math.MaxInt64-1)), -1},
		{"equal at minimum scale", New(1, Scale(math.MinInt64)), New(10, Scale(math.MinInt64+1)), 0},
		{"equal at maximum scale", New(1, Scale(math.MaxInt64-1)), New(10, Scale(math.MaxInt64)), 0},
		{"scaled zeros", New(0, Scale(math.MinInt64)), New(0, Scale(math.MaxInt64)), 0},
	} {
		t.Run("comparison handles "+test.name, func(t *testing.T) {
			if got := test.x.Cmp(test.y); got != test.want {
				t.Fatalf("Cmp((%s, %d), (%s, %d)) = %d, want %d",
					test.x.Coefficient(), test.x.Scale(), test.y.Coefficient(), test.y.Scale(), got, test.want)
			}
			if got := test.y.Cmp(test.x); got != -test.want {
				t.Fatalf("reverse comparison = %d, want %d", got, -test.want)
			}
			if test.want == 0 {
				wantTotal := 0
				switch {
				case test.x.Scale() < test.y.Scale():
					wantTotal = -1
				case test.x.Scale() > test.y.Scale():
					wantTotal = 1
				}
				if got := test.x.CompareTotal(test.y); got != wantTotal {
					t.Fatalf("CompareTotal = %d, want %d", got, wantTotal)
				}
			}
		})
	}
}

func TestCompareTotal_OrdersEqualRepresentationsByScale(t *testing.T) {
	x := MustParse("1.20")
	y := MustParse("1.2")
	if x.CompareTotal(y) <= 0 || y.CompareTotal(x) >= 0 || x.CompareTotal(x) != 0 {
		t.Fatal("CompareTotal ordering failed")
	}
	if FromInt(1).CompareTotal(FromInt(2)) >= 0 {
		t.Fatal("CompareTotal ignored numeric ordering")
	}
}

func TestExtrema_PreserveSelectedRepresentations(t *testing.T) {
	x := MustParse("1.20")
	y := MustParse("1.2")
	z := FromInt(2)
	if !Min(x, z).SameRepresentation(x) || !Max(x, z).SameRepresentation(z) {
		t.Fatal("Min or Max failed")
	}
	if !Min(z, x).SameRepresentation(x) || !Max(z, x).SameRepresentation(z) {
		t.Fatal("reversed Min or Max failed")
	}
	if !Clamp(FromInt(3), x, z).SameRepresentation(z) || !Clamp(FromInt(0), x, z).SameRepresentation(x) {
		t.Fatal("Clamp boundary failed")
	}
	if got := Clamp(y, x, z); !got.SameRepresentation(y) {
		t.Fatal("Clamp replaced an equal representation")
	}
	assertComparisonPanics(t, func() { Clamp(Decimal{}, FromInt(1), Decimal{}) })
}

func TestNumericHasher_UsesNumericEquality(t *testing.T) {
	seed := maphash.MakeSeed()
	hash := func(value Decimal) uint64 {
		var h maphash.Hash
		h.SetSeed(seed)
		NumericHasher{}.Hash(&h, value)
		return h.Sum64()
	}
	x := MustParse("1.20")
	y := MustParse("1.2")
	if !(NumericHasher{}).Equal(x, y) || hash(x) != hash(y) {
		t.Fatalf("numeric hashes differ: %x != %x", hash(x), hash(y))
	}
	for coefficient := int64(-100); coefficient <= 100; coefficient++ {
		for scale := Scale(-4); scale <= 4; scale++ {
			x := New(coefficient, scale)
			xHash := hash(x)
			multiple := int64(1)
			for trailingZeros := range 5 {
				y := New(coefficient*multiple, scale+Scale(trailingZeros))
				if !x.Equal(y) || xHash != hash(y) {
					t.Fatalf("equal representations %s and %s have different hashes", x, y)
				}
				multiple *= 10
			}
		}
	}
	assertComparisonPanics(t, func() { NumericHasher{}.Hash(nil, Decimal{}) })
}

func ExampleNumericHasher() {
	seed := maphash.MakeSeed()
	hasher := NumericHasher{}
	hash := func(value Decimal) uint64 {
		var state maphash.Hash
		state.SetSeed(seed)
		hasher.Hash(&state, value)
		return state.Sum64()
	}

	x := MustParse("1.20")
	y := MustParse("1.2")
	fmt.Println(hasher.Equal(x, y), hash(x) == hash(y))
	// Output: true true
}

func assertComparisonPanics(t *testing.T, operation func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("operation did not panic")
		}
	}()
	operation()
}

var (
	comparisonBenchmarkBool       bool
	comparisonBenchmarkComparison int
	comparisonBenchmarkUint64     uint64
)

func BenchmarkCmp(b *testing.B) {
	for _, test := range []struct {
		name string
		x, y Decimal
	}{
		{"equal_scales", New(12_345, 3), New(12_346, 3)},
		{"different_exponents", New(12_345, 3), New(6_789, 5)},
		{"equal_exponents", New(12_345, 3), New(1_234, 2)},
		{"negative", New(-12_345, 3), New(-6_789, 5)},
		{"opposite_signs", FromInt(-1), FromInt(1)},
		{"zero", New(0, Scale(math.MinInt64)), New(0, Scale(math.MaxInt64))},
		{"minimum_scale_overflow", New(1, Scale(math.MinInt64)), New(1, Scale(math.MinInt64+1))},
		{"maximum_scale_boundary", New(1, Scale(math.MaxInt64)), New(1, Scale(math.MaxInt64-1))},
		{"both_scale_boundaries", New(1, Scale(math.MinInt64)), New(1, Scale(math.MaxInt64))},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				comparisonBenchmarkComparison = test.x.Cmp(test.y)
			}
		})
	}
}

func BenchmarkSameRepresentation(b *testing.B) {
	positive := MustParse("12345678901234567890.123456789")
	b.ReportAllocs()
	for b.Loop() {
		comparisonBenchmarkBool = positive.SameRepresentation(positive)
	}
}

func BenchmarkNumericHasher(b *testing.B) {
	value := MustParse("123456789012345678901234567")
	var hash maphash.Hash
	hash.SetSeed(maphash.MakeSeed())
	b.ReportAllocs()
	for b.Loop() {
		hash.Reset()
		NumericHasher{}.Hash(&hash, value)
		comparisonBenchmarkUint64 = hash.Sum64()
	}
}
