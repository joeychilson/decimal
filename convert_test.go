package decimal

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"testing"
)

func TestFromFloat_UsesShortestDecimalAndRejectsNonFinite(t *testing.T) {
	short, err := FromFloat(0.1)
	if err != nil || short.String() != "0.1" {
		t.Fatalf("FromFloat(0.1) = %s, %v", short, err)
	}
	if _, err := FromFloat(math.Inf(1)); !errors.Is(err, ErrInvalidOperation) {
		t.Fatalf("FromFloat(+Inf) error = %v", err)
	}
}

func TestFromFloatExact_PreservesBinaryValueAndRejectsNonFinite(t *testing.T) {
	exact, err := FromFloatExact(0.1)
	if err != nil || exact.Equal(MustParse("0.1")) {
		t.Fatalf("FromFloatExact(0.1) = %s, %v", exact, err)
	}
	wantFloat64 := new(big.Rat).SetFloat64(0.1)
	if exact.BigRat().Cmp(wantFloat64) != 0 {
		t.Fatalf("FromFloatExact(0.1) = %s, want %s", exact, wantFloat64)
	}
	if _, gotErr := FromFloatExact(math.NaN()); !errors.Is(gotErr, ErrInvalidOperation) {
		t.Fatalf("FromFloatExact(NaN) error = %v", gotErr)
	}
	if _, gotErr := FromFloatExact(math.Inf(-1)); !errors.Is(gotErr, ErrInvalidOperation) {
		t.Fatalf("FromFloatExact(-Inf) error = %v", gotErr)
	}
	float32Exact, err := FromFloatExact(float32(0.1))
	wantFloat32 := new(big.Rat).SetFloat64(float64(float32(0.1)))
	if err != nil || float32Exact.BigRat().Cmp(wantFloat32) != 0 {
		t.Fatalf("FromFloatExact(float32(0.1)) = %s, %v; want %s", float32Exact, err, wantFloat32)
	}
}

func TestFromBigRat_ConvertsTerminatingValuesAndReportsUnsupportedInputs(t *testing.T) {
	oneEighth, err := FromBigRat(big.NewRat(1, 8))
	if err != nil || oneEighth.String() != "0.125" {
		t.Fatalf("FromBigRat(1/8) = %s, %v", oneEighth, err)
	}
	if _, err := FromBigRat(big.NewRat(1, 3)); !errors.Is(err, ErrInexact) {
		t.Fatalf("FromBigRat(1/3) error = %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Error("FromBigRat did not panic for a nil argument")
		}
	}()
	_, _ = FromBigRat(nil)
}

func TestBigInt_PreservesExactValuesAndOwnership(t *testing.T) {
	integerSource := MustParse("12.00")
	integer, err := integerSource.BigInt()
	if err != nil || integer.Cmp(big.NewInt(12)) != 0 {
		t.Fatalf("BigInt(12.00) = %s, %v", integer, err)
	}
	integer.SetInt64(99)
	if integerSource.String() != "12.00" {
		t.Fatal("BigInt result retained decimal storage")
	}
	if integer, err := New(-12, -2).BigInt(); err != nil || integer.Cmp(big.NewInt(-1_200)) != 0 {
		t.Fatalf("BigInt(-12e2) = %s, %v", integer, err)
	}
	if integer, err := MustParse("1.5").BigInt(); !errors.Is(err, ErrInexact) || integer != nil {
		t.Fatalf("fractional BigInt = %v, %v; want nil and ErrInexact", integer, err)
	}
	if integer, err := (Decimal{}).BigInt(); err != nil || integer.Sign() != 0 {
		t.Fatalf("zero BigInt = %s, %v", integer, err)
	}
}

func TestInt_PreservesExactValuesAndReportsLoss(t *testing.T) {
	type Cents int32
	type Units uint16
	if got, err := MustParse("123.00").Int[Cents](); err != nil || got != 123 {
		t.Fatalf("Int[Cents] = %d, %v", got, err)
	}
	if got, err := FromInt(math.MaxUint16).Int[Units](); err != nil || got != math.MaxUint16 {
		t.Fatalf("Int[Units] = %d, %v", got, err)
	}
	if _, err := MustParse("1.5").Int[int](); !errors.Is(err, ErrInexact) {
		t.Fatalf("fractional integer error = %v", err)
	}
	if _, err := FromInt(128).Int[int8](); !errors.Is(err, ErrRange) {
		t.Fatalf("int8 overflow error = %v", err)
	}
	if _, err := FromInt(-1).Int[uint64](); !errors.Is(err, ErrRange) {
		t.Fatalf("uint underflow error = %v", err)
	}
	if got, err := FromInt(-128).Int[int8](); err != nil || got != -128 {
		t.Fatalf("minimum int8 = %d, %v", got, err)
	}
	if got, err := FromInt(255).Int[uint8](); err != nil || got != 255 {
		t.Fatalf("maximum uint8 = %d, %v", got, err)
	}
	if _, err := FromInt(-129).Int[int8](); !errors.Is(err, ErrRange) {
		t.Fatalf("int8 underflow error = %v", err)
	}
	if _, err := FromInt(256).Int[uint8](); !errors.Is(err, ErrRange) {
		t.Fatalf("uint8 overflow error = %v", err)
	}
}

func TestInt64_ConvertsExactValues(t *testing.T) {
	if got, err := MustParse("-12.00").Int64(); err != nil || got != -12 {
		t.Fatalf("Int64 = %d, %v", got, err)
	}
	if got, err := FromInt(int64(math.MinInt64)).Int64(); err != nil || got != math.MinInt64 {
		t.Fatalf("minimum int64 = %d, %v", got, err)
	}
}

func TestUint64_ConvertsExactValues(t *testing.T) {
	if got, err := MustParse("12.00").Uint64(); err != nil || got != 12 {
		t.Fatalf("Uint64 = %d, %v", got, err)
	}
	if got, err := FromInt(uint64(math.MaxUint64)).Uint64(); err != nil || got != math.MaxUint64 {
		t.Fatalf("maximum uint64 = %d, %v", got, err)
	}
}

func TestFloat_HandlesNamedAndExtremeValues(t *testing.T) {
	type Float32 float32
	if got, exact := MustParse("0.5").Float[Float32](); got != 0.5 || !exact {
		t.Fatalf("Float[Float32] = %g, %v", got, exact)
	}
	if got, exact := MustParse("9e-46").Float[Float32](); got != Float32(math.SmallestNonzeroFloat32) || exact {
		t.Fatalf("float32 subnormal boundary = %g, %v", got, exact)
	}
	if got, exact := MustParse("1e-1000000000").Float[Float32](); got != 0 || exact {
		t.Fatalf("extreme float32 underflow = %g, %v", got, exact)
	}
	if got, exact := MustParse("1e1000000000").Float[Float32](); !math.IsInf(float64(got), 1) || exact {
		t.Fatalf("extreme float32 overflow = %g, %v", got, exact)
	}
}

func TestFloat64_HandlesExactAndExtremeValues(t *testing.T) {
	exactInput, err := FromFloatExact(0.1)
	if err != nil {
		t.Fatal(err)
	}
	if got, exact := exactInput.Float64(); got != 0.1 || !exact {
		t.Fatalf("Float64 = %g, %v; want 0.1, true", got, exact)
	}
	if got, exact := MustParse("1e-1000000000").Float64(); got != 0 || exact {
		t.Fatalf("extreme float64 underflow = %g, %v", got, exact)
	}
	if got, exact := MustParse("-1e-1000000000").Float64(); got != 0 || !math.Signbit(got) || exact {
		t.Fatalf("extreme negative float64 underflow = %g, %v", got, exact)
	}
	if got, exact := MustParse("1e1000000000").Float64(); !math.IsInf(got, 1) || exact {
		t.Fatalf("extreme float64 overflow = %g, %v", got, exact)
	}
	if got, exact := MustParse("-1e1000000000").Float64(); !math.IsInf(got, -1) || exact {
		t.Fatalf("extreme negative float64 overflow = %g, %v", got, exact)
	}
}

func ExampleFromFloatExact() {
	human, err := FromFloat(0.1)
	if err != nil {
		fmt.Println("short conversion:", err)
		return
	}
	exact, err := FromFloatExact(0.1)
	if err != nil {
		fmt.Println("exact conversion:", err)
		return
	}

	fmt.Println(human)
	fmt.Println(exact.Equal(human))
	// Output:
	// 0.1
	// false
}

var (
	convertBenchmarkFloat64 float64
	convertBenchmarkExact   bool
)

func BenchmarkFloat64(b *testing.B) {
	x := MustParse("-12345678901234567890.123456789")
	b.ReportAllocs()
	for b.Loop() {
		convertBenchmarkFloat64, convertBenchmarkExact = x.Float64()
	}
}

func BenchmarkFloatExtremeExponent(b *testing.B) {
	x := MustParse("1e-1000000000")
	b.ReportAllocs()
	for b.Loop() {
		convertBenchmarkFloat64, convertBenchmarkExact = x.Float64()
	}
}
