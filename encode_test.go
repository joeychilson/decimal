package decimal

import (
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestTextEncoding_RoundTripsRepresentations(t *testing.T) {
	values := []Decimal{{}, MustParse("0.00"), MustParse("-123.4500"), MustParse("12e2"), New(1, Scale(math.MaxInt64))}
	for _, value := range values {
		text, err := value.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var fromText Decimal
		if err := fromText.UnmarshalText(text); err != nil || !fromText.SameRepresentation(value) {
			t.Fatalf("text round trip %s: %s, %v", value, fromText, err)
		}
	}

	appended, err := MustParse("1.20").AppendText([]byte("$"))
	if err != nil || string(appended) != "$1.20" {
		t.Fatalf("AppendText = %q, %v", appended, err)
	}
	original := MustParse("9.9")
	got := original
	if err := got.UnmarshalText([]byte("x")); err == nil || !got.SameRepresentation(original) {
		t.Fatalf("failed UnmarshalText changed receiver to %s: %v", got, err)
	}
}

func TestBinaryEncoding_RoundTripsRepresentations(t *testing.T) {
	values := []Decimal{{}, MustParse("0.00"), MustParse("-123.4500"), MustParse("12e2"), New(1, Scale(math.MaxInt64))}
	for _, value := range values {
		encoded, err := value.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		var fromBinary Decimal
		if err := fromBinary.UnmarshalBinary(encoded); err != nil || !fromBinary.SameRepresentation(value) {
			t.Fatalf("binary round trip %s: %s, %v", value, fromBinary, err)
		}
	}
	for _, test := range []struct {
		value Decimal
		want  string
	}{
		{Decimal{}, "\x01\x00\x00\x00"},
		{New(0, 2), "\x01\x00\x04\x00"},
		{MustParse("-1.20"), "\x01\x01\x04\x01\x78"},
		{New(12, -2), "\x01\x00\x03\x01\x0c"},
	} {
		encoded, err := test.value.AppendBinary([]byte("$"))
		if err != nil || string(encoded) != "$"+test.want {
			t.Fatalf("AppendBinary(%s) = %x, %v; want %x", test.value, encoded, err, "$"+test.want)
		}
	}

	original := MustParse("9.9")
	bad := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"truncated header", []byte{binaryEncodingVersion}},
		{"unknown version", []byte{99, 0}},
		{"invalid sign", []byte{binaryEncodingVersion, 2}},
		{"truncated scale", []byte{binaryEncodingVersion, 0, 0x80}},
		{"non-canonical scale", []byte{binaryEncodingVersion, 0, 0x80, 0, 0}},
		{"truncated size", []byte{binaryEncodingVersion, 0, 0, 0x80}},
		{"non-canonical size", []byte{binaryEncodingVersion, 0, 0, 0x80, 0}},
		{"short coefficient", []byte{binaryEncodingVersion, 0, 0, 2, 1}},
		{"trailing data", []byte{binaryEncodingVersion, 0, 0, 1, 1, 2}},
		{"leading coefficient zero", []byte{binaryEncodingVersion, 0, 0, 2, 0, 1}},
		{"negative zero", []byte{binaryEncodingVersion, 1, 0, 0}},
		{"overflowing scale", []byte{binaryEncodingVersion, 0, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}},
		{"overflowing size", []byte{binaryEncodingVersion, 0, 0, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}},
	}
	for _, test := range bad {
		t.Run("rejects "+test.name, func(t *testing.T) {
			got := original
			if err := got.UnmarshalBinary(test.data); !errors.Is(err, ErrSyntax) || !got.SameRepresentation(original) {
				t.Fatalf("UnmarshalBinary(%x) = %s, %v; want unchanged receiver and ErrSyntax", test.data, got, err)
			}
		})
	}
}

func TestJSONEncoding_RoundTripsRepresentations(t *testing.T) {
	values := []Decimal{{}, MustParse("0.00"), MustParse("-123.4500"), MustParse("12e2"), New(1, Scale(math.MaxInt64))}
	for _, value := range values {
		encoded, err := jsonv1.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Decimal
		if err := jsonv1.Unmarshal(encoded, &decoded); err != nil || !decoded.SameRepresentation(value) {
			t.Fatalf("JSON round trip %s: %s, %v (%s)", value, decoded, err, encoded)
		}
	}

	value := MustParse("123.4500")
	quoted, err := jsonv2.Marshal(value, jsonv2.StringifyNumbers(true))
	if err != nil || string(quoted) != `"123.4500"` {
		t.Fatalf("JSON v2 stringified = %s, %v", quoted, err)
	}
	var decoded Decimal
	if decodeErr := jsonv2.Unmarshal(quoted, &decoded, jsonv2.StringifyNumbers(true)); decodeErr != nil || !decoded.SameRepresentation(value) {
		t.Fatalf("JSON v2 stringified decode = %s, %v", decoded, decodeErr)
	}
	jsonText, err := MustParse("1.20").MarshalJSON()
	if err != nil || string(jsonText) != "1.20" {
		t.Fatalf("MarshalJSON = %q, %v", jsonText, err)
	}
	original := MustParse("9.9")
	for _, decode := range []func(*Decimal) error{
		func(d *Decimal) error { return jsonv1.Unmarshal([]byte(`"1.2"`), d) },
		func(d *Decimal) error { return jsonv2.Unmarshal([]byte(`"1.2"`), d) },
	} {
		got := original
		if err := decode(&got); err == nil || !got.SameRepresentation(original) {
			t.Fatalf("failed JSON decode changed receiver to %s: %v", got, err)
		}
	}
	for _, input := range []string{"null", `"1"`, "true", "01", "+1", "1 2"} {
		got := original
		if err := got.UnmarshalJSON([]byte(input)); !errors.Is(err, ErrSyntax) || !got.SameRepresentation(original) {
			t.Errorf("UnmarshalJSON(%q) = %s, %v; want unchanged receiver and ErrSyntax", input, got, err)
		}
	}
	got := original
	if err := got.UnmarshalJSON([]byte("1e9223372036854775809")); !errors.Is(err, ErrRange) || !got.SameRepresentation(original) {
		t.Fatalf("range JSON decode = %s, %v; want unchanged receiver and ErrRange", got, err)
	}
	got = original
	if err := jsonv2.Unmarshal([]byte("1.20"), &got, jsonv2.StringifyNumbers(true)); err == nil || !got.SameRepresentation(original) {
		t.Fatalf("unquoted stringified JSON decode = %s, %v; want unchanged receiver and an error", got, err)
	}
	got = original
	if err := jsonv2.Unmarshal([]byte(`"not a decimal"`), &got, jsonv2.StringifyNumbers(true)); !errors.Is(err, ErrSyntax) || !got.SameRepresentation(original) {
		t.Fatalf("invalid stringified JSON decode = %s, %v; want unchanged receiver and ErrSyntax", got, err)
	}
}

func TestPointerOperations_PanicOnNilRequiredPointers(t *testing.T) {
	var destination *Decimal
	for _, test := range []struct {
		name string
		fn   func()
	}{
		{"text", func() { _ = destination.UnmarshalText(nil) }},
		{"binary", func() { _ = destination.UnmarshalBinary(nil) }},
		{"JSON", func() { _ = destination.UnmarshalJSON(nil) }},
		{"JSON stream destination", func() { _ = destination.UnmarshalJSONFrom(nil) }},
		{"JSON encoder", func() { _ = (Decimal{}).MarshalJSONTo(nil) }},
		{"JSON decoder", func() { _ = new(Decimal).UnmarshalJSONFrom(nil) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("operation did not panic")
				}
			}()
			test.fn()
		})
	}
}

func ExampleDecimal_MarshalJSONTo() {
	encoded, err := jsonv2.Marshal(MustParse("1.20"), jsonv2.StringifyNumbers(true))
	if err != nil {
		fmt.Println("marshal:", err)
		return
	}

	fmt.Println(string(encoded))
	// Output: "1.20"
}

func FuzzTextRoundTrip(f *testing.F) {
	for _, text := range []string{"0", "-1.2300", "12e2", "invalid"} {
		f.Add([]byte(text))
	}
	f.Fuzz(func(t *testing.T, text []byte) {
		original := MustParse("9.9")
		decoded := original
		if err := decoded.UnmarshalText(text); err != nil {
			if !decoded.SameRepresentation(original) {
				t.Fatalf("failed text decode changed receiver to %s", decoded)
			}
			return
		}
		encoded, err := decoded.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var roundTrip Decimal
		if err := roundTrip.UnmarshalText(encoded); err != nil || !roundTrip.SameRepresentation(decoded) {
			t.Fatalf("text round trip = %s, %v; want %s", roundTrip, err, decoded)
		}
	})
}

func FuzzBinaryRoundTrip(f *testing.F) {
	for _, value := range []Decimal{{}, MustParse("0.00"), MustParse("-1.2300"), MustParse("12e2")} {
		encoded, err := value.MarshalBinary()
		if err != nil {
			f.Fatal(err)
		}
		f.Add(encoded)
	}
	f.Fuzz(func(t *testing.T, encoded []byte) {
		original := MustParse("9.9")
		d := original
		if err := d.UnmarshalBinary(encoded); err != nil {
			if !d.SameRepresentation(original) {
				t.Fatalf("failed binary decode changed receiver to %s", d)
			}
			return
		}
		roundTrip, err := d.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if string(roundTrip) != string(encoded) {
			t.Fatalf("non-canonical accepted encoding: %x became %x", encoded, roundTrip)
		}
	})
}

func FuzzJSONRoundTrip(f *testing.F) {
	for _, data := range []string{"0", "-1.2300", "12e2", "null", `"1.2"`} {
		f.Add([]byte(data))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		original := MustParse("9.9")
		decoded := original
		if err := decoded.UnmarshalJSON(data); err != nil {
			if !decoded.SameRepresentation(original) {
				t.Fatalf("failed JSON decode changed receiver to %s", decoded)
			}
			return
		}
		encoded, err := decoded.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		var roundTrip Decimal
		if err := roundTrip.UnmarshalJSON(encoded); err != nil || !roundTrip.SameRepresentation(decoded) {
			t.Fatalf("JSON round trip = %s, %v; want %s", roundTrip, err, decoded)
		}
	})
}

var (
	benchmarkEncodedDecimal Decimal
	benchmarkEncodedBytes   []byte
	errEncodingBenchmark    error
)

func BenchmarkUnmarshalBinary(b *testing.B) {
	d := MustParse("-12345678901234567890.123456789")
	encoded, err := d.MarshalBinary()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		var decoded Decimal
		if err := decoded.UnmarshalBinary(encoded); err != nil {
			b.Fatal(err)
		}
		benchmarkEncodedDecimal = decoded
	}
}

func BenchmarkBinarySizes(b *testing.B) {
	values := []struct {
		name  string
		value Decimal
	}{
		{"compact", MustParse("-123.45")},
		{"34_digits", MustParse("-12345678901234567.89012345678901234")},
		{"1000_digits", MustParse("-" + strings.Repeat("1234567890", 100))},
	}
	for _, test := range values {
		encoded, err := test.value.MarshalBinary()
		if err != nil {
			b.Fatal(err)
		}
		b.Run(test.name+"/marshal", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkEncodedBytes, errEncodingBenchmark = test.value.MarshalBinary()
			}
		})
		b.Run(test.name+"/unmarshal", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var decoded Decimal
				errEncodingBenchmark = decoded.UnmarshalBinary(encoded)
				benchmarkEncodedDecimal = decoded
			}
		})
	}
}

func BenchmarkJSON(b *testing.B) {
	value := MustParse("-12345678901234567890.123456789")
	encoded := []byte("-12345678901234567890.123456789")

	b.Run("v1_marshal", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkEncodedBytes, errEncodingBenchmark = jsonv1.Marshal(value)
			if errEncodingBenchmark != nil {
				b.Fatal(errEncodingBenchmark)
			}
		}
	})
	b.Run("v1_unmarshal", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var decoded Decimal
			errEncodingBenchmark = jsonv1.Unmarshal(encoded, &decoded)
			if errEncodingBenchmark != nil {
				b.Fatal(errEncodingBenchmark)
			}
			benchmarkEncodedDecimal = decoded
		}
	})
	b.Run("v2_marshal", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkEncodedBytes, errEncodingBenchmark = jsonv2.Marshal(value)
			if errEncodingBenchmark != nil {
				b.Fatal(errEncodingBenchmark)
			}
		}
	})
	b.Run("v2_unmarshal", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var decoded Decimal
			errEncodingBenchmark = jsonv2.Unmarshal(encoded, &decoded)
			if errEncodingBenchmark != nil {
				b.Fatal(errEncodingBenchmark)
			}
			benchmarkEncodedDecimal = decoded
		}
	})
}
