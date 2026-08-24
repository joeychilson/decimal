package decimal

import (
	"errors"
	"math"
	"math/big"
	"testing"
)

func TestSquareRoot_ReturnsExactResults(t *testing.T) {
	exactRoots := []struct{ input, want string }{
		{"2.25", "1.5"},
		{"0.0004", "0.02"},
		{"100e2", "10e1"},
		{"0.00", "0.0"},
	}
	for _, test := range exactRoots {
		got, err := MustParse(test.input).Sqrt()
		if err != nil || got.String() != test.want {
			t.Errorf("Sqrt(%s) = %s, %v; want %s", test.input, got, err, test.want)
		}
	}
	if _, err := FromInt(2).Sqrt(); !errors.Is(err, ErrInexact) {
		t.Fatalf("Sqrt(2) error = %v", err)
	}
	if _, err := FromInt(-1).Sqrt(); !errors.Is(err, ErrInvalidOperation) {
		t.Fatalf("Sqrt(-1) error = %v", err)
	}
	if got, err := MustParse("0.040").Sqrt(); err != nil || !got.SameRepresentation(MustParse("0.20")) {
		t.Fatalf("odd-scale Sqrt(0.040) = %s [scale %d], %v", got, got.Scale(), err)
	}
	maximumScale := Scale(math.MaxInt64)
	if got, err := New(10, maximumScale).Sqrt(); err != nil || !got.SameRepresentation(New(1, maximumScale/2)) {
		t.Fatalf("maximum-scale exact Sqrt = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	if got, err := New(1, maximumScale).Sqrt(); !errors.Is(err, ErrInexact) || !got.SameRepresentation(Decimal{}) {
		t.Fatalf("maximum-scale inexact Sqrt = %s, %v; want zero and ErrInexact", got, err)
	}
	if got, err := New(0, -3).Sqrt(); err != nil || !got.SameRepresentation(New(0, -1)) {
		t.Fatalf("negative odd-scale zero Sqrt = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	largeRoot := MustParse("1234567890123456789012345678901234")
	largeSquare, err := largeRoot.Mul(largeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := largeSquare.Sqrt(); err != nil || !got.SameRepresentation(largeRoot) {
		t.Fatalf("large exact Sqrt = %s, %v; want %s", got, err, largeRoot)
	}
}

func TestSquareRoot_ReturnsExactIntegerRoots(t *testing.T) {
	for lower := int64(1); lower <= 99; lower++ {
		root := FromInt(lower)
		if got, err := FromInt(lower * lower).Sqrt(); err != nil || !got.SameRepresentation(root) {
			t.Fatalf("Sqrt(%d) = %s, %v; want %s", lower*lower, got, err, root)
		}
	}
}

func TestSquareRoot_HandlesExtremeScaleShortcuts(t *testing.T) {
	if got, err := New(0, Scale(math.MaxInt64)).Sqrt(); err != nil || got.Scale() != Scale(math.MaxInt64)/2+1 {
		t.Fatalf("extreme zero Sqrt scale = %d, %v", got.Scale(), err)
	}
}

func TestContextSquareRootRangeFallback_PreservesOnlyExactRoots(t *testing.T) {
	precision := ^uint(0)
	rangeLimitedSquareRoot := squareRootAtRangeLimit
	if precision > uint(math.MaxUint32) {
		rangeLimitedSquareRoot = func(x Decimal, precision uint, mode RoundingMode) (Decimal, error) {
			return (Context{Precision: precision, Rounding: mode}).Sqrt(x)
		}
	}

	root := MustParse("1234567890123456789012345678901234")
	square, err := root.Mul(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := rangeLimitedSquareRoot(square, precision, HalfEven); err != nil || !got.SameRepresentation(root) {
		t.Fatalf("exact square root = coefficient %s, scale %d, %v; want coefficient %s, scale %d",
			got.Coefficient(), got.Scale(), err, root.Coefficient(), root.Scale())
	}
	if _, err := rangeLimitedSquareRoot(FromInt(2), precision, HalfEven); !errors.Is(err, ErrRange) {
		t.Fatalf("rounded square root error = %v, want ErrRange", err)
	}
	if _, err := rangeLimitedSquareRoot(FromInt(2), precision, Exact); !errors.Is(err, ErrInexact) {
		t.Fatalf("exact square root error = %v, want ErrInexact", err)
	}
}

func roundSquareRootToPrecision(x Decimal, precision uint, mode RoundingMode) (Decimal, bool) {
	coefficient, scale := x.Coefficient(), x.Scale()
	if coefficient.Sign() == 0 {
		rootScale := scale / 2
		if scale > 0 && scale&1 != 0 {
			rootScale++
		}
		return New(0, rootScale), true
	}

	adjusted := int64(len(coefficient.String())-1) - int64(scale)
	rootExponent := adjusted / 2
	if adjusted < 0 && adjusted%2 != 0 {
		rootExponent--
	}
	targetScale := Scale(int64(precision-1) - rootExponent)
	scaled := x.BigRat()
	if targetScale >= 0 {
		scaled.Mul(scaled, new(big.Rat).SetInt(oraclePowerOfTen(uint64(targetScale)*2)))
	} else {
		scaled.Quo(scaled, new(big.Rat).SetInt(oraclePowerOfTen(oracleScaleMagnitude(targetScale)*2)))
	}

	numerator := scaled.Num()
	denominator := scaled.Denom()
	var integer, root, square big.Int
	integer.Quo(numerator, denominator)
	root.Sqrt(&integer)
	square.Mul(&root, &root)
	exact := square.Mul(&square, denominator).Cmp(numerator) == 0
	if mode == Exact && !exact {
		return Decimal{}, false
	}
	if !exact {
		midpointComparison := 0
		if mode == HalfEven || mode == HalfUp || mode == HalfDown {
			var twiceRootPlusOne, midpointSquare, fourNumerator big.Int
			twiceRootPlusOne.Lsh(&root, 1)
			twiceRootPlusOne.Add(&twiceRootPlusOne, big.NewInt(1))
			midpointSquare.Mul(&twiceRootPlusOne, &twiceRootPlusOne)
			midpointSquare.Mul(&midpointSquare, denominator)
			fourNumerator.Lsh(numerator, 2)
			midpointComparison = fourNumerator.Cmp(&midpointSquare)
		}
		if oracleRoundingIncrement(mode, 1, midpointComparison, &root) {
			root.Add(&root, big.NewInt(1))
		}
	}

	preferredScale := scale / 2
	if scale > 0 && scale&1 != 0 {
		preferredScale++
	}
	if exact {
		for targetScale > preferredScale && oracleHasTrailingDecimalZero(&root) {
			root.Quo(&root, big.NewInt(10))
			targetScale--
		}
	}
	for uint(oracleDecimalDigitCount(&root)) > precision {
		root.Quo(&root, big.NewInt(10))
		targetScale--
	}
	return NewBig(&root, targetScale), exact
}

var (
	sqrtBenchmarkDecimal Decimal
	sqrtBenchmarkBool    bool
	errSqrtBenchmark     error
)

func BenchmarkSmallExactSquareRoot(b *testing.B) {
	maximumUint32 := uint64(math.MaxUint32)
	for _, test := range []struct {
		name  string
		value Decimal
		exact bool
	}{
		{"exact_even_scale", New(225, 2), true},
		{"exact_odd_scale", New(40, 3), true},
		{"inexact", FromInt(2), false},
		{"uint64_limit_square", FromInt(maximumUint32 * maximumUint32), true},
		{"uint64_limit_inexact", FromInt(uint64(math.MaxUint64)), false},
	} {
		want, wantErr := squareRootExact(test.value)
		coefficient, scale := decimalParts(test.value)
		b.Run(test.name, func(b *testing.B) {
			b.Run("small", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					sqrtBenchmarkDecimal, sqrtBenchmarkBool = smallExactSquareRoot(coefficient, scale)
				}
				if sqrtBenchmarkBool != test.exact || test.exact && !sqrtBenchmarkDecimal.SameRepresentation(want) {
					b.Fatalf("small root = %s, %t; want %s, %t", sqrtBenchmarkDecimal, sqrtBenchmarkBool, want, test.exact)
				}
			})
			b.Run("integer", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					sqrtBenchmarkDecimal, errSqrtBenchmark = squareRootExact(test.value)
				}
				if !errors.Is(errSqrtBenchmark, wantErr) || wantErr == nil && !sqrtBenchmarkDecimal.SameRepresentation(want) {
					b.Fatalf("integer root = %s, %v; want %s, %v", sqrtBenchmarkDecimal, errSqrtBenchmark, want, wantErr)
				}
			})
		})
	}
}
