package decimal

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestStringAndAppend_PreserveRepresentedScale(t *testing.T) {
	for _, test := range []struct {
		value Decimal
		want  string
	}{
		{Decimal{}, "0"},
		{New(0, 2), "0.00"},
		{New(-12345, 3), "-12.345"},
		{New(12, -2), "12e2"},
		{New(1, Scale(math.MaxInt64)), "1e-9223372036854775807"},
	} {
		if got := test.value.String(); got != test.want {
			t.Errorf("String() = %q, want %q", got, test.want)
		}
		if got := string(test.value.Append([]byte("$"))); got != "$"+test.want {
			t.Errorf("Append() = %q, want %q", got, "$"+test.want)
		}
	}
}

func TestFormatting_RespectsVerbsFlagsAndPrecision(t *testing.T) {
	d := MustParse("1.225")
	tests := []struct{ format, want string }{
		{"%s", "1.225"},
		{"%q", `"1.225"`},
		{"%#q", "`1.225`"},
		{"%f", "1.225000"},
		{"%.2f", "1.22"},
		{"%.2e", "1.22e+00"},
		{"%.3g", "1.22"},
		{"%#g", "1.22500"},
		{"%+08.2f", "+0001.22"},
		{"%-8.2f", "1.22    "},
		{"%#.0f", "1."},
	}
	for _, test := range tests {
		if got := fmt.Sprintf(test.format, d); got != test.want {
			t.Errorf("Sprintf(%q) = %q, want %q", test.format, got, test.want)
		}
	}
	if got := fmt.Sprintf("%.3g", MustParse("12345")); got != "1.23e+04" {
		t.Errorf("large %%g = %q", got)
	}
	if got := fmt.Sprintf("%.3g", MustParse("0.000012345")); got != "1.23e-05" {
		t.Errorf("small %%g = %q", got)
	}

	negativeHalf := MustParse("-0.5")
	for _, format := range []string{"%.0f", "%#.0f", "%05.0f"} {
		if got, want := fmt.Sprintf(format, negativeHalf), fmt.Sprintf(format, -0.5); got != want {
			t.Errorf("Sprintf(%q, -0.5) = %q, want %q", format, got, want)
		}
	}

	for _, test := range []struct {
		text  string
		value float64
	}{
		{"0", 0},
		{"-1.5", -1.5},
		{"1.234375", 1.234375},
		{"0.0001220703125", 0.0001220703125},
		{"0.000030517578125", 0.000030517578125},
		{"100000", 100000},
		{"1e5", 100000},
		{"1000000", 1000000},
	} {
		for _, verb := range []byte{'g', 'G'} {
			format := "%" + string(verb)
			got := fmt.Sprintf(format, MustParse(test.text))
			want := fmt.Sprintf(format, test.value)
			if got != want {
				t.Errorf("Sprintf(%q, %s) = %q, want %q", format, test.text, got, want)
			}
		}
	}

	for _, test := range []struct{ format, input, want string }{
		{"%g", "1.2345678901234567890123456789", "1.2345678901234567890123456789"},
		{"%g", "12345678901234567890", "1.234567890123456789e+19"},
		{"%G", "12345678901234567890", "1.234567890123456789E+19"},
		{"%g", "1.2345678900", "1.23456789"},
	} {
		if got := fmt.Sprintf(test.format, MustParse(test.input)); got != test.want {
			t.Errorf("Sprintf(%q, %s) = %q, want %q", test.format, test.input, got, test.want)
		}
	}
}

func TestFormatting_RejectsUnsafeRequestsWithoutPanicking(t *testing.T) {
	value := MustParse("1.2")
	for _, state := range []*formatState{
		{width: maximumFormatParameter + 1, hasWidth: true},
		{precision: maximumFormatParameter + 1, hasPrecision: true},
	} {
		value.Format(state, 'f')
		if got, want := state.String(), "%!f(decimal.Decimal=1.2)"; got != want {
			t.Fatalf("Format wrote %q, want %q", got, want)
		}
	}

	state := &formatState{precision: 6, hasPrecision: true}
	New(1, Scale(math.MinInt64)).Format(state, 'f')
	if got, want := state.String(), "%!f(decimal.Decimal=1e9223372036854775808)"; got != want {
		t.Fatalf("extreme fixed Format wrote %q, want %q", got, want)
	}

	defer func() {
		if recover() == nil {
			t.Error("Format did not panic")
		}
	}()
	value.Format(nil, 'v')
}

type formatState struct {
	bytes.Buffer
	width, precision       int
	hasWidth, hasPrecision bool
}

func (s *formatState) Width() (int, bool)     { return s.width, s.hasWidth }
func (s *formatState) Precision() (int, bool) { return s.precision, s.hasPrecision }
func (*formatState) Flag(int) bool            { return false }

var benchmarkFormattedString string

func BenchmarkString(b *testing.B) {
	tests := []struct {
		name  string
		value Decimal
	}{
		{"compact", MustParse("-123.45")},
		{"34_digits", MustParse("-12345678901234567.89012345678901234")},
		{"1000_digits", MustParse("-" + strings.Repeat("1234567890", 100))},
		{"large_exponent", New(12345, -1_000_000_000)},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkFormattedString = test.value.String()
			}
		})
	}
}
