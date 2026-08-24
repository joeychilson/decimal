package decimal

import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"strings"
	"testing"
)

func TestParseError_ExposesDocumentedFailures(t *testing.T) {
	parseError := &ParseError{Input: "x", Offset: 0, Err: ErrSyntax}
	if !errors.Is(parseError, ErrSyntax) || parseError.Error() == "" || !errors.Is(parseError.Unwrap(), ErrSyntax) {
		t.Fatalf("ParseError behavior: %v", parseError)
	}
	if (*ParseError)(nil).Error() == "" {
		t.Fatal("nil error formatting failed")
	}
	if (&ParseError{Input: "x"}).Error() == "" {
		t.Fatal("zero error formatting failed")
	}
	if (*ParseError)(nil).Unwrap() != nil {
		t.Fatal("nil ParseError unwrap did not return nil")
	}
	if got, want := parseError.Error(), `decimal: parsing "x" at byte 0: invalid syntax`; got != want {
		t.Fatalf("ParseError.Error() = %q, want %q", got, want)
	}
}

func TestParse_PreservesValuesAndScales(t *testing.T) {
	tests := []struct {
		input       string
		coefficient string
		scale       Scale
		text        string
	}{
		{"0", "0", 0, "0"},
		{"-0.00", "0", 2, "0.00"},
		{"+123", "123", 0, "123"},
		{"001.2300", "12300", 4, "1.2300"},
		{".5", "5", 1, "0.5"},
		{"1.", "1", 0, "1"},
		{"12e2", "12", -2, "12e2"},
		{"12.30e-2", "1230", 4, "0.1230"},
		{"1e-9223372036854775807", "1", Scale(math.MaxInt64), "1e-9223372036854775807"},
		{"1e9223372036854775808", "1", Scale(math.MinInt64), "1e9223372036854775808"},
		{"0.1e9223372036854775809", "1", Scale(math.MinInt64), "1e9223372036854775808"},
	}
	for _, test := range tests {
		t.Run("parses "+test.input, func(t *testing.T) {
			d, err := Parse(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got := d.Coefficient().String(); got != test.coefficient {
				t.Fatalf("coefficient = %s, want %s", got, test.coefficient)
			}
			if got := d.Scale(); got != test.scale {
				t.Fatalf("scale = %d, want %d", got, test.scale)
			}
			if test.text != "" {
				got := d.String()
				if got != test.text {
					t.Fatalf("String = %q, want %q", got, test.text)
				}
				roundTrip, err := Parse(got)
				if err != nil || !roundTrip.SameRepresentation(d) {
					t.Fatalf("Parse(String()) = %v, %v", roundTrip, err)
				}
			}
		})
	}

	invalid := []string{"", "+", "-", ".", "e1", "1e", "1e+", " 1", "1 ", "1_0", "NaN", "Inf", "1..0", "1x"}
	for _, input := range invalid {
		if _, err := Parse(input); !errors.Is(err, ErrSyntax) {
			t.Errorf("Parse(%q) error = %v, want ErrSyntax", input, err)
		}
	}
	if _, err := Parse("1e9223372036854775809"); !errors.Is(err, ErrRange) {
		t.Fatalf("large exponent error = %v, want ErrRange", err)
	}
	if _, err := Parse("1e18446744073709551616"); !errors.Is(err, ErrRange) {
		t.Fatalf("overflowing exponent error = %v, want ErrRange", err)
	}
}

func TestParse_AcceptsLargeCoefficients(t *testing.T) {
	text := strings.Repeat("1234567890", 100)
	parsed, err := Parse("-" + text + "e-42")
	if err != nil {
		t.Fatal(err)
	}
	want, ok := new(big.Int).SetString("-"+text, 10)
	if !ok || parsed.Coefficient().Cmp(want) != 0 || parsed.Scale() != 42 {
		t.Fatalf("large coefficient parsed incorrectly")
	}
}

func TestMustParse_PanicsForInvalidInput(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustParse did not panic")
		}
	}()
	MustParse("not a decimal")
}

func FuzzParse(f *testing.F) {
	grammar := regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?$`)
	for _, seed := range []string{"0", "-0.00", "1.2300", ".5", "12e2", "-999999999999999999.000001e-20", "1e-9223372036854775807"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		d, err := Parse(input)
		validSyntax := grammar.MatchString(input)
		if !validSyntax {
			if !errors.Is(err, ErrSyntax) {
				t.Fatalf("Parse(%q) error = %v, want ErrSyntax", input, err)
			}
			return
		}
		if errors.Is(err, ErrSyntax) {
			t.Fatalf("Parse(%q) rejected valid decimal grammar: %v", input, err)
		}
		if err != nil {
			if !errors.Is(err, ErrRange) {
				t.Fatalf("Parse(%q) error = %v, want ErrRange", input, err)
			}
			return
		}
		roundTrip, err := Parse(d.String())
		if err != nil {
			t.Fatal(err)
		}
		if !roundTrip.SameRepresentation(d) {
			t.Fatalf("Parse(%q) = %s; Parse(String()) = %s", input, d, roundTrip)
		}
		if len(input) > 256 || d.Scale() < -256 || d.Scale() > 256 {
			return
		}
		want, ok := new(big.Rat).SetString(input)
		if !ok {
			t.Fatalf("math/big rejected valid decimal %q", input)
		}
		if got := d.BigRat(); got.Cmp(want) != 0 {
			t.Fatalf("Parse(%q) = %s, want numeric value %s", input, got, want)
		}
	})
}

var (
	parseBenchmarkDecimal Decimal
	errParseBenchmark     error
	parseBenchmarkBigInt  *big.Int
	parseBenchmarkOK      bool
)

func BenchmarkParseCompact(b *testing.B) {
	for b.Loop() {
		parseBenchmarkDecimal, errParseBenchmark = Parse("-123.45")
	}
}

func BenchmarkParse(b *testing.B) {
	for b.Loop() {
		parseBenchmarkDecimal, errParseBenchmark = Parse("-12345678901234567890.123456789e-42")
	}
}

func BenchmarkParse1000Digits(b *testing.B) {
	text := strings.Repeat("1234567890", 100) + "e-42"
	for b.Loop() {
		parseBenchmarkDecimal, errParseBenchmark = Parse(text)
	}
}

func BenchmarkSetDecimalDigits(b *testing.B) {
	for _, test := range []struct {
		name   string
		digits []byte
	}{
		{"19_digits", []byte("1234567890123456789")},
		{"20_digits", []byte("12345678901234567890")},
		{"1000_digits", []byte(strings.Repeat("1234567890", 100))},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.Run("chunked", func(b *testing.B) {
				b.ReportAllocs()
				var result big.Int
				for b.Loop() {
					setDecimalDigits(&result, test.digits)
					parseBenchmarkBigInt = &result
				}
			})
			b.Run("set_string", func(b *testing.B) {
				b.ReportAllocs()
				var result big.Int
				for b.Loop() {
					parseBenchmarkBigInt, parseBenchmarkOK = result.SetString(string(test.digits), 10)
				}
			})
		})
	}
}
