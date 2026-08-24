package decimal

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestSQLConversions_UseLosslessDecimalSemantics(t *testing.T) {
	type databaseInt int16
	type databaseUint uint32

	for _, test := range []struct {
		name   string
		source any
		want   Decimal
	}{
		{"string", "1.20", MustParse("1.20")},
		{"bytes", []byte("1.20"), MustParse("1.20")},
		{"int64", int64(-12), FromInt(-12)},
		{"uint32", uint32(12), FromInt(12)},
		{"named int", databaseInt(-12), FromInt(-12)},
		{"named uint", databaseUint(12), FromInt(12)},
		{"float64", 1.5, MustParse("1.5")},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got Decimal
			if err := got.Scan(test.source); err != nil || !got.SameRepresentation(test.want) {
				t.Fatalf("Scan(%T) = %s, %v; want %s", test.source, got, err, test.want)
			}
		})
	}

	var d Decimal
	if err := d.Scan(nil); !errors.Is(err, ErrInvalidOperation) {
		t.Fatalf("Scan(nil) error = %v, want ErrInvalidOperation", err)
	}
	original := MustParse("9.9")
	unchanged := original
	if err := unchanged.Scan(true); !errors.Is(err, ErrInvalidOperation) || !unchanged.SameRepresentation(original) {
		t.Fatalf("failed Scan changed receiver to %s: %v", unchanged, err)
	}
	unchanged = original
	if err := unchanged.Scan(math.NaN()); !errors.Is(err, ErrInvalidOperation) || !unchanged.SameRepresentation(original) {
		t.Fatalf("non-finite Scan changed receiver to %s: %v", unchanged, err)
	}
	if value, err := MustParse("1.20").Value(); err != nil || value != driver.Value("1.20") {
		t.Fatalf("Value = %v, %v", value, err)
	}
	if value, err := New(1, Scale(math.MaxInt64)).Value(); err != nil || value != driver.Value("1e-9223372036854775807") {
		t.Fatalf("extreme Value = %v, %v", value, err)
	}
	var nullable sql.Null[Decimal]
	if err := nullable.Scan("1.20"); err != nil || !nullable.Valid || nullable.V.String() != "1.20" {
		t.Fatalf("sql.Null[Decimal].Scan = %+v, %v", nullable, err)
	}
	if err := nullable.Scan(nil); err != nil || nullable.Valid {
		t.Fatalf("sql.Null[Decimal].Scan(nil) = %+v, %v", nullable, err)
	}
}

func TestScan_PanicsOnNilReceiver(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Scan did not panic")
		}
	}()
	var destination *Decimal
	_ = destination.Scan(nil)
}

func ExampleDecimal_Scan() {
	var amount sql.Null[Decimal]
	if err := amount.Scan(nil); err != nil {
		fmt.Println("scan null:", err)
		return
	}
	fmt.Println(amount.Valid)

	if err := amount.Scan("1.20"); err != nil {
		fmt.Println("scan decimal:", err)
		return
	}
	fmt.Println(amount.Valid, amount.V)
	// Output:
	// false
	// true 1.20
}

var (
	sqlBenchmarkDecimal Decimal
	sqlBenchmarkValue   driver.Value
	errSQLBenchmark     error
)

func BenchmarkSQLSizes(b *testing.B) {
	values := []struct {
		name string
		text string
	}{
		{"compact", "-123.45"},
		{"34_digits", "-12345678901234567.89012345678901234"},
		{"1000_digits", "-" + strings.Repeat("1234567890", 100)},
	}
	for _, test := range values {
		value := MustParse(test.text)
		b.Run(test.name+"/value", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sqlBenchmarkValue, errSQLBenchmark = value.Value()
			}
		})
		b.Run(test.name+"/scan", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var decoded Decimal
				errSQLBenchmark = decoded.Scan(test.text)
				sqlBenchmarkDecimal = decoded
			}
		})
	}
}
