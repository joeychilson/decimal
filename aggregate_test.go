package decimal

import (
	"errors"
	"math"
	"math/big"
	"testing"
)

func TestAggregates_PreserveScaleAndValidateScaleRange(t *testing.T) {
	values := []Decimal{MustParse("1.0"), MustParse("2.00"), MustParse("3")}
	if got := Sum(values...); got.String() != "6.00" {
		t.Fatalf("Sum = %s", got)
	}
	if got, err := Product(values...); err != nil || got.String() != "6.000" {
		t.Fatalf("Product = %s, %v", got, err)
	}
	extremeScales := []Decimal{
		New(1, Scale(math.MaxInt64)),
		New(1, 1),
		New(1, -1),
	}
	wantExtremeProduct := New(1, Scale(math.MaxInt64))
	if got, err := Product(extremeScales...); err != nil || !got.SameRepresentation(wantExtremeProduct) {
		t.Fatalf("Product with canceling scale overflow = %s, %v", got, err)
	}
	underflowingScales := []Decimal{
		New(1, Scale(math.MinInt64)),
		New(1, -1),
		New(1, 1),
	}
	wantUnderflowingProduct := New(1, Scale(math.MinInt64))
	if got, err := Product(underflowingScales...); err != nil || !got.SameRepresentation(wantUnderflowingProduct) {
		t.Fatalf("Product with canceling scale underflow = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	overflowingScales := []Decimal{New(1, Scale(math.MaxInt64)), New(1, 1)}
	if got, err := Product(overflowingScales...); !errors.Is(err, ErrRange) || !got.SameRepresentation(Decimal{}) {
		t.Fatalf("unrepresentable Product = %s, %v; want zero and ErrRange", got, err)
	}
	if got, err := Product(New(10, Scale(math.MaxInt64)), New(1, 1)); err != nil || !got.SameRepresentation(New(1, Scale(math.MaxInt64))) {
		t.Fatalf("Product above maximum scale = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	underflowingResultScales := []Decimal{New(1, Scale(math.MinInt64)), New(1, -1)}
	if got, err := Product(underflowingResultScales...); err != nil || !got.SameRepresentation(New(10, Scale(math.MinInt64))) {
		t.Fatalf("Product below minimum scale = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	if got, err := Product(New(0, Scale(math.MaxInt64)), New(1, 1)); err != nil || !got.SameRepresentation(New(0, Scale(math.MaxInt64))) {
		t.Fatalf("zero Product above maximum scale = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	emptyProduct, err := Product()
	if Sum().String() != "0" || err != nil || emptyProduct.String() != "1" {
		t.Fatal("empty aggregate identities failed")
	}
}

var (
	aggregateBenchmarkDecimal Decimal
	errAggregateBenchmark     error
)

func BenchmarkSum100(b *testing.B) {
	values := make([]Decimal, 100)
	for i := range values {
		values[i] = New(int64(i+1), Scale(i%5))
	}
	b.ReportAllocs()
	for b.Loop() {
		aggregateBenchmarkDecimal = Sum(values...)
	}
}

func BenchmarkProduct100(b *testing.B) {
	coefficientValues := make([]Decimal, 100)
	unitValues := make([]Decimal, 100)
	overflowCancellation := make([]Decimal, 100)
	for i := range coefficientValues {
		coefficientValues[i] = New(int64(i+1), Scale(i%5))
		unitValues[i] = New(1, Scale(i%5))
		overflowCancellation[i] = FromInt(1)
	}
	overflowCancellation[0] = New(1, Scale(math.MaxInt64))
	overflowCancellation[1] = New(1, 1)
	overflowCancellation[2] = New(1, -1)

	for _, test := range []struct {
		name   string
		values []Decimal
	}{
		{"coefficients", coefficientValues},
		{"unit_coefficients", unitValues},
		{"overflow_cancellation", overflowCancellation},
	} {
		want, wantErr := Product(test.values...)
		b.Run(test.name, func(b *testing.B) {
			b.Run("small_wide", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					aggregateBenchmarkDecimal, errAggregateBenchmark = Product(test.values...)
				}
				if !errors.Is(errAggregateBenchmark, wantErr) || wantErr == nil && !aggregateBenchmarkDecimal.SameRepresentation(want) {
					b.Fatalf("small/wide result = %s, %v; want %s, %v", aggregateBenchmarkDecimal, errAggregateBenchmark, want, wantErr)
				}
			})
			b.Run("big_int", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					coefficient := big.NewInt(1)
					var scale, operand big.Int
					for _, value := range test.values {
						valueCoefficient, valueScale := decimalParts(value)
						scale.Add(&scale, operand.SetInt64(int64(valueScale)))
						coefficient.Mul(coefficient, valueCoefficient)
					}
					resultScale, err := fitCoefficientScale(coefficient, &scale)
					if err != nil {
						aggregateBenchmarkDecimal = Decimal{}
						errAggregateBenchmark = err
						continue
					}
					aggregateBenchmarkDecimal = makeDecimal(coefficient, resultScale)
					errAggregateBenchmark = nil
				}
				if !errors.Is(errAggregateBenchmark, wantErr) || wantErr == nil && !aggregateBenchmarkDecimal.SameRepresentation(want) {
					b.Fatalf("big.Int result = %s, %v; want %s, %v", aggregateBenchmarkDecimal, errAggregateBenchmark, want, wantErr)
				}
			})
		})
	}
}
