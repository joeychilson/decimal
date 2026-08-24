package decimal

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"strings"
	"testing"
)

func TestContextValidate_AcceptsZeroValue(t *testing.T) {
	if err := (Context{}).Validate(); err != nil {
		t.Fatalf("zero Context validation = %v", err)
	}
}

func TestContextValidate_RejectsInvalidRoundingMode(t *testing.T) {
	if err := (Context{Rounding: RoundingMode(255)}).Validate(); !errors.Is(err, ErrInvalidRoundingMode) {
		t.Fatalf("invalid context error = %v", err)
	}
}

func TestContextRound_AppliesPrecision(t *testing.T) {
	if got, err := (Context{Precision: 2}).Round(MustParse("1.25")); err != nil || got.String() != "1.2" {
		t.Fatalf("Context.Round = %s, %v", got, err)
	}
	value := MustParse("1.2500")
	if got, err := (Context{}).Round(value); err != nil || !got.SameRepresentation(value) {
		t.Fatalf("unlimited Context.Round = %s, %v; want %s", got, err, value)
	}
}

func TestContextAdd_RoundsFinalResult(t *testing.T) {
	ctx := Context{Precision: 3, Rounding: HalfEven}
	if got, err := ctx.Add(MustParse("1.234"), Decimal{}); err != nil || got.String() != "1.23" {
		t.Fatalf("Context.Add = %s, %v; want 1.23", got, err)
	}
}

func TestContextAdd_HandlesExtremeScales(t *testing.T) {
	const exponent = 1_000_000_000
	large := New(1, Scale(-exponent))
	one := FromInt(1)
	ctx := Context{Precision: 3, Rounding: HalfEven}
	wantRounded := New(100, Scale(-exponent+2))
	if got, err := ctx.Add(large, one); err != nil || !got.SameRepresentation(wantRounded) {
		t.Fatalf("extreme Add = coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), err, wantRounded.Coefficient(), wantRounded.Scale())
	}
	if got, err := (Context{Precision: 1, Rounding: Floor}).Add(New(1, -100), FromInt(-1)); err != nil || !got.SameRepresentation(New(9, -99)) {
		t.Fatalf("one-digit floor addition = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}

	tie := MustParse("1.25")
	positiveTiny := New(1, exponent)
	negativeTiny := New(-1, exponent)
	tieContext := Context{Precision: 2, Rounding: HalfEven}
	if got, err := tieContext.Add(tie, positiveTiny); err != nil || got.String() != "1.3" {
		t.Fatalf("tie plus tiny = %s, %v", got, err)
	}
	if got, err := tieContext.Add(tie, negativeTiny); err != nil || got.String() != "1.2" {
		t.Fatalf("tie minus tiny = %s, %v", got, err)
	}
	if _, err := (Context{Precision: 3, Rounding: Exact}).Add(large, one); !errors.Is(err, ErrInexact) {
		t.Fatalf("exact extreme addition error = %v", err)
	}
	fineZero := New(0, exponent)
	if got, err := ctx.Add(one, fineZero); err != nil || !got.SameRepresentation(New(100, 2)) {
		t.Fatalf("fine-zero addition = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	if got, err := (Context{Precision: 3, Rounding: Exact}).Add(one, fineZero); err != nil || !got.SameRepresentation(New(100, 2)) {
		t.Fatalf("exact fine-zero addition = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
}

func TestContextAddition_HandlesScaleRangeBoundaries(t *testing.T) {
	minimumScale := Scale(math.MinInt64)
	maximumScale := Scale(math.MaxInt64)
	large := New(1, minimumScale)
	tiny := New(1, maximumScale)
	want := New(100, minimumScale+2)
	ctx := Context{Precision: 3, Rounding: HalfEven}

	for _, test := range []struct {
		name string
		x, y Decimal
	}{
		{"large operand first", large, tiny},
		{"large operand second", tiny, large},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ctx.Add(test.x, test.y)
			if err != nil || !got.SameRepresentation(want) {
				t.Fatalf("Add(%s, %s) = coefficient %s, scale %d, %v; want coefficient %s, scale %d",
					test.x, test.y, got.Coefficient(), got.Scale(), err, want.Coefficient(), want.Scale())
			}
		})
	}

	if _, err := (Context{Precision: 1}).Add(New(10, minimumScale), tiny); !errors.Is(err, ErrRange) {
		t.Fatalf("addition below minimum target scale error = %v, want ErrRange", err)
	}
}

func TestContextAddition_CorrectsOnlyOneDigitAtPowerOfTenBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		context    Context
		x, y, want Decimal
	}{
		{
			"positive carry",
			Context{Precision: 2, Rounding: HalfEven},
			New(999, 0),
			New(1, 1_000),
			New(10, -2),
		},
		{
			"negative carry",
			Context{Precision: 2, Rounding: HalfEven},
			New(-999, 0),
			New(-1, 1_000),
			New(-10, -2),
		},
		{
			"positive cancellation",
			Context{Precision: 1, Rounding: Floor},
			New(1, -1_000),
			FromInt(-1),
			New(9, -999),
		},
		{
			"negative cancellation",
			Context{Precision: 1, Rounding: Ceiling},
			New(-1, -1_000),
			FromInt(1),
			New(-9, -999),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.context.Add(test.x, test.y)
			if err != nil || !got.SameRepresentation(test.want) {
				t.Fatalf("Add(%s, %s) = coefficient %s, scale %d, %v; want coefficient %s, scale %d",
					test.x, test.y, got.Coefficient(), got.Scale(), err, test.want.Coefficient(), test.want.Scale())
			}
		})
	}
}

func TestContextAdd_MatchesBigRatForRandomInputs(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	modes := []RoundingMode{HalfEven, HalfUp, HalfDown, TowardZero, AwayFromZero, Floor, Ceiling, ZeroFiveUp, Exact}
	for i := range 1_000 {
		xCoefficient := int64(rng.Uint64()%1_000_000 + 1)
		yCoefficient := int64(rng.Uint64()%1_000_000 + 1)
		if rng.Uint64()&1 != 0 {
			xCoefficient = -xCoefficient
		}
		if rng.Uint64()&1 != 0 {
			yCoefficient = -yCoefficient
		}
		x := New(xCoefficient, -20)
		y := New(yCoefficient, 20)
		if rng.Uint64()&1 != 0 {
			x, y = y, x
		}
		precision := uint(rng.Uint64()%8 + 1)
		mode := modes[rng.Uint64()%uint64(len(modes))]
		ctx := Context{Precision: precision, Rounding: mode}
		xRat, yRat := x.BigRat(), y.BigRat()
		got, gotErr := ctx.Add(x, y)
		want, exact := roundRatToPrecision(new(big.Rat).Add(xRat, yRat), max(x.Scale(), y.Scale()), precision, mode)
		if mode == Exact && !exact {
			if !errors.Is(gotErr, ErrInexact) {
				t.Fatalf("case %d Add error = %v, want ErrInexact", i, gotErr)
			}
			continue
		}
		if gotErr != nil || !got.SameRepresentation(want) {
			t.Fatalf("case %d Add = %s [scale %d], %v; want %s [scale %d]", i, got, got.Scale(), gotErr, want, want.Scale())
		}
	}
}

func TestContextSub_HandlesExtremeScales(t *testing.T) {
	const exponent = 1_000_000_000
	large := New(1, Scale(-exponent))
	one := FromInt(1)
	wantRounded := New(100, Scale(-exponent+2))
	if got, err := (Context{Precision: 3, Rounding: HalfEven}).Sub(large, one); err != nil || !got.SameRepresentation(wantRounded) {
		t.Fatalf("extreme Sub = coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), err, wantRounded.Coefficient(), wantRounded.Scale())
	}
	got, err := (Context{Precision: 3, Rounding: Floor}).Sub(large, one)
	wantFloor := New(999, Scale(-exponent+3))
	if err != nil || !got.SameRepresentation(wantFloor) {
		t.Fatalf("floor subtraction = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
}

func TestContextSub_MatchesBigRatForRandomInputs(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	modes := []RoundingMode{HalfEven, HalfUp, HalfDown, TowardZero, AwayFromZero, Floor, Ceiling, ZeroFiveUp, Exact}
	for i := range 1_000 {
		xCoefficient := int64(rng.Uint64()%1_000_000 + 1)
		yCoefficient := int64(rng.Uint64()%1_000_000 + 1)
		if rng.Uint64()&1 != 0 {
			xCoefficient = -xCoefficient
		}
		if rng.Uint64()&1 != 0 {
			yCoefficient = -yCoefficient
		}
		x := New(xCoefficient, -20)
		y := New(yCoefficient, 20)
		if rng.Uint64()&1 != 0 {
			x, y = y, x
		}
		precision := uint(rng.Uint64()%8 + 1)
		mode := modes[rng.Uint64()%uint64(len(modes))]
		ctx := Context{Precision: precision, Rounding: mode}
		xRat, yRat := x.BigRat(), y.BigRat()
		got, gotErr := ctx.Sub(x, y)
		want, exact := roundRatToPrecision(new(big.Rat).Sub(xRat, yRat), max(x.Scale(), y.Scale()), precision, mode)
		if mode == Exact && !exact {
			if !errors.Is(gotErr, ErrInexact) {
				t.Fatalf("case %d Sub error = %v, want ErrInexact", i, gotErr)
			}
			continue
		}
		if gotErr != nil || !got.SameRepresentation(want) {
			t.Fatalf("case %d Sub = %s [scale %d], %v; want %s [scale %d]", i, got, got.Scale(), gotErr, want, want.Scale())
		}
	}
}

func TestContextMul_RoundsFinalResult(t *testing.T) {
	ctx := Context{Precision: 3, Rounding: HalfEven}
	if got, err := ctx.Mul(MustParse("9.99"), FromInt(10)); err != nil || got.String() != "99.9" {
		t.Fatalf("Context.Mul = %s, %v; want 99.9", got, err)
	}
}

func TestContextMul_ReportsInvalidModeAndScaleRange(t *testing.T) {
	if _, err := (Context{Rounding: 99}).Mul(FromInt(1), FromInt(1)); !errors.Is(err, ErrInvalidRoundingMode) {
		t.Fatalf("invalid Context.Mul error = %v", err)
	}
	if _, err := (Context{}).Mul(New(1, Scale(math.MaxInt64)), New(1, 1)); !errors.Is(err, ErrRange) {
		t.Fatalf("Context.Mul scale error = %v", err)
	}
}

func TestContextMul_HandlesExtremeScales(t *testing.T) {
	maximumScale := Scale(math.MaxInt64)
	minimumScale := Scale(math.MinInt64)
	for _, test := range []struct {
		name string
		fn   func() (Decimal, error)
		want Decimal
	}{
		{"multiply above maximum scale", func() (Decimal, error) { return (Context{}).Mul(New(10, maximumScale), New(1, 1)) }, New(1, maximumScale)},
		{"multiply negative above maximum scale", func() (Decimal, error) { return (Context{}).Mul(New(-10, maximumScale), New(1, 1)) }, New(-1, maximumScale)},
		{"multiply below minimum scale", func() (Decimal, error) { return (Context{}).Mul(New(1, minimumScale), New(1, -1)) }, New(10, minimumScale)},
		{"zero above maximum scale", func() (Decimal, error) { return (Context{}).Mul(New(0, maximumScale), New(1, 1)) }, New(0, maximumScale)},
		{"zero below minimum scale", func() (Decimal, error) { return (Context{}).Mul(New(0, minimumScale), New(1, -1)) }, New(0, minimumScale)},
	} {
		t.Run("handles "+test.name, func(t *testing.T) {
			got, err := test.fn()
			if err != nil || !got.SameRepresentation(test.want) {
				t.Fatalf("got coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), err, test.want.Coefficient(), test.want.Scale())
			}
		})
	}
}

func TestContextMul_MatchesBigRatForRandomInputs(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	modes := []RoundingMode{HalfEven, HalfUp, HalfDown, TowardZero, AwayFromZero, Floor, Ceiling, ZeroFiveUp, Exact}
	for i := range 1_000 {
		xCoefficient := int64(rng.Uint64()%1_000_000 + 1)
		yCoefficient := int64(rng.Uint64()%1_000_000 + 1)
		if rng.Uint64()&1 != 0 {
			xCoefficient = -xCoefficient
		}
		if rng.Uint64()&1 != 0 {
			yCoefficient = -yCoefficient
		}
		x := New(xCoefficient, -20)
		y := New(yCoefficient, 20)
		if rng.Uint64()&1 != 0 {
			x, y = y, x
		}
		precision := uint(rng.Uint64()%8 + 1)
		mode := modes[rng.Uint64()%uint64(len(modes))]
		ctx := Context{Precision: precision, Rounding: mode}
		xRat, yRat := x.BigRat(), y.BigRat()
		got, gotErr := ctx.Mul(x, y)
		want, exact := roundRatToPrecision(new(big.Rat).Mul(xRat, yRat), x.Scale()+y.Scale(), precision, mode)
		if mode == Exact && !exact {
			if !errors.Is(gotErr, ErrInexact) {
				t.Fatalf("case %d Mul error = %v, want ErrInexact", i, gotErr)
			}
			continue
		}
		if gotErr != nil || !got.SameRepresentation(want) {
			t.Fatalf("case %d Mul = %s [scale %d], %v; want %s [scale %d]", i, got, got.Scale(), gotErr, want, want.Scale())
		}
	}
}

func TestContextDiv_RoundsFinalResult(t *testing.T) {
	ctx := Context{Precision: 3, Rounding: HalfEven}
	if got, err := ctx.Div(FromInt(1), FromInt(8)); err != nil || got.String() != "0.125" {
		t.Fatalf("exact Context.Div = %s, %v; want 0.125", got, err)
	}
	if got, err := ctx.Div(FromInt(2), FromInt(3)); err != nil || got.String() != "0.667" {
		t.Fatalf("repeating Context.Div = %s, %v; want 0.667", got, err)
	}
	if _, err := (Context{Precision: 3, Rounding: Exact}).Div(FromInt(1), FromInt(3)); !errors.Is(err, ErrInexact) {
		t.Fatalf("exact context error = %v", err)
	}
}

func TestContextDiv_RejectsInvalidRoundingMode(t *testing.T) {
	if _, err := (Context{Rounding: RoundingMode(255)}).Div(FromInt(1), FromInt(2)); !errors.Is(err, ErrInvalidRoundingMode) {
		t.Fatalf("Context.Div error = %v, want ErrInvalidRoundingMode", err)
	}
}

func TestContextDiv_HandlesExtremeScales(t *testing.T) {
	ctx := Context{Precision: 3, Rounding: HalfEven}
	one := FromInt(1)
	maximumScale := Scale(math.MaxInt64)
	minimumScale := Scale(math.MinInt64)
	for _, test := range []struct {
		name string
		x, y Decimal
		want Decimal
	}{
		{"at maximum scale", New(1, maximumScale), one, New(1, maximumScale)},
		{"at minimum scale", New(1, minimumScale), one, New(1, minimumScale)},
		{"equal maximum scales", New(1, maximumScale), New(1, maximumScale), one},
		{"equal minimum scales", New(1, minimumScale), New(1, minimumScale), one},
		{"zero above maximum scale", New(0, maximumScale), New(1, minimumScale), New(0, maximumScale)},
		{"zero below minimum scale", New(0, minimumScale), New(1, maximumScale), New(0, minimumScale)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ctx.Div(test.x, test.y)
			if err != nil || !got.SameRepresentation(test.want) {
				t.Fatalf("Context.Div = coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), err, test.want.Coefficient(), test.want.Scale())
			}
		})
	}
	if _, err := ctx.Div(New(1, maximumScale), FromInt(3)); !errors.Is(err, ErrRange) {
		t.Fatalf("unrepresentable rounded division error = %v", err)
	}
	if _, err := (Context{Precision: 3, Rounding: Exact}).Div(New(1, maximumScale), FromInt(3)); !errors.Is(err, ErrInexact) {
		t.Fatalf("unrepresentable exact division error = %v", err)
	}
}

func TestContextDiv_WorkingScaleAllowsIntermediateOverflow(t *testing.T) {
	ctx := Context{Precision: 1, Rounding: HalfEven}
	maximumScale := Scale(math.MaxInt64)
	// The first scale subtraction overflows, but the ratio exponent brings the
	// completed target back into range. Three keeps exact division from hiding
	// an incorrect early ErrRange.
	got, err := ctx.Div(New(100, maximumScale), New(3, -1))
	want := New(3, maximumScale)
	if err != nil || !got.SameRepresentation(want) {
		t.Fatalf("intermediate overflow Div = coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), err, want.Coefficient(), want.Scale())
	}

	if _, err := ctx.Div(New(1, maximumScale), New(1, -1)); !errors.Is(err, ErrRange) {
		t.Fatalf("completed working-scale overflow error = %v, want ErrRange", err)
	}
}

func TestContextDiv_UnlimitedRequiresExactResult(t *testing.T) {
	if got, err := (Context{}).Div(FromInt(1), FromInt(8)); err != nil || got.String() != "0.125" {
		t.Fatalf("unlimited Context.Div = %s, %v; want 0.125", got, err)
	}
	if _, err := (Context{}).Div(FromInt(1), FromInt(3)); !errors.Is(err, ErrInexact) {
		t.Fatalf("unlimited Context.Div error = %v, want ErrInexact", err)
	}
}

func TestContextDivision_MatchesBigRatAcrossScales(t *testing.T) {
	modes := [...]RoundingMode{HalfEven, HalfUp, HalfDown, TowardZero, AwayFromZero, Floor, Ceiling, ZeroFiveUp, Exact}
	// Quotient value and preferred scale depend on the difference between the
	// operand scales, so one pair per difference covers every scale pair in
	// [-3, 3] without repeating equivalent cases.
	for scaleDifference := Scale(-6); scaleDifference <= 6; scaleDifference++ {
		xScale, yScale := Scale(3), Scale(3)-scaleDifference
		if scaleDifference < 0 {
			xScale, yScale = -3, Scale(-3)-scaleDifference
		}
		for xCoefficient := int64(-40); xCoefficient <= 40; xCoefficient++ {
			x := New(xCoefficient, xScale)
			for yCoefficient := int64(-40); yCoefficient <= 40; yCoefficient++ {
				if yCoefficient == 0 {
					continue
				}
				y := New(yCoefficient, yScale)
				quotient := new(big.Rat).Quo(x.BigRat(), y.BigRat())
				for precision := uint(1); precision <= 6; precision++ {
					for _, mode := range modes {
						got, gotErr := (Context{Precision: precision, Rounding: mode}).Div(x, y)
						want, exact := roundRatToPrecision(quotient, xScale-yScale, precision, mode)
						if mode == Exact && !exact {
							if !errors.Is(gotErr, ErrInexact) {
								t.Fatalf("Context{%d, %s}.Div(%s, %s) error = %v, want ErrInexact", precision, mode, x, y, gotErr)
							}
							continue
						}
						if gotErr != nil || !got.SameRepresentation(want) {
							t.Fatalf("Context{%d, %s}.Div(%s, %s) = %s [scale %d], %v; want %s [scale %d]", precision, mode, x, y, got, got.Scale(), gotErr, want, want.Scale())
						}
					}
				}
			}
		}
	}
}

func TestContextDivision_TerminatingWordDivisorMatchesBigRat(t *testing.T) {
	twoTo63 := new(big.Int).Lsh(big.NewInt(1), 63)
	fiveTo27 := new(big.Int).Exp(big.NewInt(5), big.NewInt(27), nil)
	tenTo20 := new(big.Int).Exp(big.NewInt(10), big.NewInt(20), nil)
	divisors := []*big.Int{
		big.NewInt(8),
		new(big.Int).Neg(twoTo63),
		fiveTo27,
		tenTo20, // Larger than a machine word exercises the general fallback.
	}
	modes := [...]RoundingMode{HalfEven, HalfUp, HalfDown, TowardZero, AwayFromZero, Floor, Ceiling, ZeroFiveUp, Exact}
	precisions := [...]uint{1, 34, 100}
	x := MustParse("12345678901234567890.123456788")
	for _, divisor := range divisors {
		y := NewBig(divisor, 0)
		wantRat := new(big.Rat).Quo(x.BigRat(), y.BigRat())
		for _, precision := range precisions {
			for _, mode := range modes {
				got, gotErr := (Context{Precision: precision, Rounding: mode}).Div(x, y)
				want, exact := roundRatToPrecision(wantRat, x.Scale()-y.Scale(), precision, mode)
				if mode == Exact && !exact {
					if !errors.Is(gotErr, ErrInexact) {
						t.Fatalf("Context{%d, %s}.Div(%s, %s) error = %v, want ErrInexact", precision, mode, x, y, gotErr)
					}
					continue
				}
				if gotErr != nil || !got.SameRepresentation(want) {
					t.Fatalf("Context{%d, %s}.Div(%s, %s) = %s [scale %d], %v; want %s [scale %d]", precision, mode, x, y, got, got.Scale(), gotErr, want, want.Scale())
				}
			}
		}
	}
}

func TestContextFMA_RoundsFinalResult(t *testing.T) {
	ctx := Context{Precision: 3, Rounding: HalfEven}
	if got, err := ctx.FMA(MustParse("9.99"), MustParse("9.99"), MustParse("0.199")); err != nil || got.String() != "100" {
		t.Fatalf("Context.FMA = %s, %v; want 100", got, err)
	}
	preferred := New(12_000, 5)
	if got, err := (Context{}).FMA(New(0, 5), FromInt(1), New(12, 2)); err != nil || !got.SameRepresentation(preferred) {
		t.Fatalf("zero-product FMA = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	if got, err := (Context{}).FMA(New(12, 2), FromInt(1), New(0, 5)); err != nil || !got.SameRepresentation(preferred) {
		t.Fatalf("zero-addend FMA = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
	}
	assertContextPanics(t, func() {
		_, _ = roundWithPreferredScale(New(1, 2), 1, 3, HalfEven)
	})
}

func TestContextFMA_ReportsScaleRange(t *testing.T) {
	if _, err := (Context{}).FMA(New(1, Scale(math.MaxInt64)), New(1, 1), Decimal{}); !errors.Is(err, ErrRange) {
		t.Fatalf("Context.FMA scale error = %v", err)
	}
}

func TestContextFMA_HandlesExtremeScales(t *testing.T) {
	const exponent = 1_000_000_000
	ctx := Context{Precision: 3, Rounding: HalfEven}
	one := FromInt(1)
	wantRounded := New(100, Scale(-exponent+2))
	if got, err := ctx.FMA(New(1, -exponent/2), New(1, -exponent/2), one); err != nil || !got.SameRepresentation(wantRounded) {
		t.Fatalf("extreme Context.FMA = coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), err, wantRounded.Coefficient(), wantRounded.Scale())
	}
	maximumScale := Scale(math.MaxInt64)
	minimumScale := Scale(math.MinInt64)
	for _, test := range []struct {
		name    string
		x, y, z Decimal
		want    Decimal
	}{
		{"above maximum scale", New(10, maximumScale), New(1, 1), New(0, maximumScale), New(1, maximumScale)},
		{"below minimum scale", New(-1, minimumScale), New(1, -1), New(0, minimumScale), New(-10, minimumScale)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := (Context{}).FMA(test.x, test.y, test.z)
			if err != nil || !got.SameRepresentation(test.want) {
				t.Fatalf("Context.FMA = coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), err, test.want.Coefficient(), test.want.Scale())
			}
		})
	}
}

func TestContextFMA_PreservesZeroFiveUpCancellationAcrossWideScales(t *testing.T) {
	ctx := Context{Precision: 1, Rounding: ZeroFiveUp}
	for _, test := range []struct {
		name    string
		x, y, z Decimal
		want    Decimal
	}{
		{
			name: "negative result",
			x:    New(-121, 77),
			y:    New(-9, 100),
			z:    New(-10, -87),
			want: New(-9, -87),
		},
		{
			name: "positive result",
			x:    New(121, 77),
			y:    New(-9, 100),
			z:    New(10, -87),
			want: New(9, -87),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ctx.FMA(test.x, test.y, test.z)
			if err != nil || !got.SameRepresentation(test.want) {
				t.Fatalf("FMA(%s, %s, %s) = coefficient %s, scale %d, %v; want coefficient %s, scale %d",
					test.x, test.y, test.z, got.Coefficient(), got.Scale(), err, test.want.Coefficient(), test.want.Scale())
			}
		})
	}
}

func TestContextFMA_MatchesBigRatForRandomInputs(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	modes := []RoundingMode{HalfEven, HalfUp, HalfDown, TowardZero, AwayFromZero, Floor, Ceiling, ZeroFiveUp, Exact}
	seven := FromInt(7)
	sevenRat := big.NewRat(7, 1)
	for i := range 1_000 {
		xCoefficient := int64(rng.Uint64()%1_000_000 + 1)
		yCoefficient := int64(rng.Uint64()%1_000_000 + 1)
		if rng.Uint64()&1 != 0 {
			xCoefficient = -xCoefficient
		}
		if rng.Uint64()&1 != 0 {
			yCoefficient = -yCoefficient
		}
		x := New(xCoefficient, -20)
		y := New(yCoefficient, 20)
		if rng.Uint64()&1 != 0 {
			x, y = y, x
		}
		precision := uint(rng.Uint64()%8 + 1)
		mode := modes[rng.Uint64()%uint64(len(modes))]
		ctx := Context{Precision: precision, Rounding: mode}
		xRat, yRat := x.BigRat(), y.BigRat()
		got, gotErr := ctx.FMA(x, seven, y)
		wantRat := new(big.Rat).Add(new(big.Rat).Mul(xRat, sevenRat), yRat)
		want, exact := roundRatToPrecision(wantRat, max(x.Scale(), y.Scale()), precision, mode)
		if mode == Exact && !exact {
			if !errors.Is(gotErr, ErrInexact) {
				t.Fatalf("case %d FMA error = %v, want ErrInexact", i, gotErr)
			}
			continue
		}
		if gotErr != nil || !got.SameRepresentation(want) {
			t.Fatalf("case %d FMA = %s [scale %d], %v; want %s [scale %d]", i, got, got.Scale(), gotErr, want, want.Scale())
		}
	}
}

func TestContextSqrt_RejectsInvalidRoundingMode(t *testing.T) {
	if _, err := (Context{Rounding: RoundingMode(255)}).Sqrt(FromInt(1)); !errors.Is(err, ErrInvalidRoundingMode) {
		t.Fatalf("Context.Sqrt error = %v, want ErrInvalidRoundingMode", err)
	}
}

func TestContextSqrt_HandlesExtremeScales(t *testing.T) {
	ctx := Context{Precision: 3, Rounding: HalfEven}
	maximumScale := Scale(math.MaxInt64)
	minimumScale := Scale(math.MinInt64)
	for _, test := range []struct {
		name  string
		value Decimal
		want  Decimal
	}{
		{"at maximum scale", New(10, maximumScale), New(1, maximumScale/2)},
		{"at minimum scale", New(1, minimumScale), New(1, minimumScale/2)},
		{"zero at maximum scale", New(0, maximumScale), New(0, maximumScale/2+1)},
		{"rounded at maximum scale", New(1, maximumScale), New(316, maximumScale/2+3)},
		{"rounded at minimum scale", New(2, minimumScale), New(141, minimumScale/2+2)},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ctx.Sqrt(test.value)
			if err != nil || !got.SameRepresentation(test.want) {
				t.Fatalf("Context.Sqrt = coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), err, test.want.Coefficient(), test.want.Scale())
			}
		})
	}
	if _, err := (Context{Precision: 3, Rounding: Exact}).Sqrt(New(1, maximumScale)); !errors.Is(err, ErrInexact) {
		t.Fatalf("extreme exact square-root error = %v", err)
	}
	if ^uint(0) > uint(math.MaxUint32) {
		maximumInt := ^uint(0) >> 1
		precision := maximumInt + maximumInt/2 + 2
		x := New(1, minimumScale)
		if got, err := (Context{Precision: precision}).Sqrt(x); err != nil || !got.SameRepresentation(New(1, minimumScale/2)) {
			t.Fatalf("wide-precision exact square root = coefficient %s, scale %d, %v", got.Coefficient(), got.Scale(), err)
		}
	}
}

func TestContextSqrt_UnlimitedRequiresExactResult(t *testing.T) {
	if got, err := (Context{}).Sqrt(MustParse("2.25")); err != nil || got.String() != "1.5" {
		t.Fatalf("unlimited Context.Sqrt = %s, %v; want 1.5", got, err)
	}
	if _, err := (Context{}).Sqrt(FromInt(2)); !errors.Is(err, ErrInexact) {
		t.Fatalf("unlimited Context.Sqrt error = %v, want ErrInexact", err)
	}
}

func TestContextSquareRoot_ReturnsRoundedAndExactResults(t *testing.T) {
	ctx := Context{Precision: 10, Rounding: HalfEven}
	if got, err := ctx.Sqrt(FromInt(2)); err != nil || got.String() != "1.414213562" {
		t.Fatalf("rounded Sqrt(2) = %s, %v", got, err)
	}
	if got, err := ctx.Sqrt(MustParse("10000000000000000000e-1")); err != nil || got.String() != "1000000000" {
		t.Fatalf("exact uint64-boundary square root = %s, %v", got, err)
	}
	if got, err := (Context{Precision: 1, Rounding: Ceiling}).Sqrt(FromInt(2)); err != nil || got.String() != "2" {
		t.Fatalf("ceiling Sqrt(2) = %s, %v", got, err)
	}

	tests := []struct {
		input string
		mode  RoundingMode
		want  string
	}{
		{"2.25", HalfEven, "2"},
		{"2.25", HalfUp, "2"},
		{"2.25", HalfDown, "1"},
		{"2", TowardZero, "1"},
		{"2", AwayFromZero, "2"},
		{"2", Floor, "1"},
	}
	for _, test := range tests {
		got, err := (Context{Precision: 1, Rounding: test.mode}).Sqrt(MustParse(test.input))
		if err != nil || got.String() != test.want {
			t.Errorf("Sqrt(%s), %s = %s, %v; want %s", test.input, test.mode, got, err, test.want)
		}
	}

	largeRoot := MustParse("1234567890123456789012345678901234")
	largeSquare, err := largeRoot.Mul(largeRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, precision := range [...]uint{34, 1_000} {
		if got, err := (Context{Precision: precision}).Sqrt(largeSquare); err != nil || !got.SameRepresentation(largeRoot) {
			t.Fatalf("large exact context root at precision %d = %s [scale %d], %v; want %s [scale %d]", precision, got, got.Scale(), err, largeRoot, largeRoot.Scale())
		}
	}
}

func TestContextSquareRoot_RoundsCorrectlyAtBoundaries(t *testing.T) {
	modes := [...]RoundingMode{HalfEven, HalfUp, HalfDown}
	for lower := int64(1); lower <= 99; lower++ {
		precision := uint(1)
		if lower >= 10 {
			precision = 2
		}
		odd := 2*lower + 1
		midpoint := odd * odd * 250_000
		for _, test := range []struct {
			name  string
			delta int64
		}{
			{"below midpoint", -1},
			{"at midpoint", 0},
			{"above midpoint", 1},
		} {
			x := New(midpoint+test.delta, 6)
			for _, mode := range modes {
				want := lower
				if test.delta > 0 || test.delta == 0 && (mode == HalfUp || mode == HalfEven && lower&1 != 0) {
					want++
				}
				got, err := (Context{Precision: precision, Rounding: mode}).Sqrt(x)
				if err != nil || !got.Equal(FromInt(want)) {
					t.Fatalf("%s: Context{%d, %s}.Sqrt(%s) = %s, %v; want %d", test.name, precision, mode, x, got, err, want)
				}
			}
		}
	}
}

func TestContextSquareRoot_MatchesBigRatOracle(t *testing.T) {
	modes := [...]RoundingMode{HalfEven, HalfUp, HalfDown, TowardZero, AwayFromZero, Floor, Ceiling, ZeroFiveUp, Exact}
	for coefficient := range int64(401) {
		for scale := Scale(-5); scale <= 5; scale++ {
			x := New(coefficient, scale)
			for precision := uint(1); precision <= 6; precision++ {
				for _, mode := range modes {
					got, gotErr := (Context{Precision: precision, Rounding: mode}).Sqrt(x)
					want, exact := roundSquareRootToPrecision(x, precision, mode)
					if mode == Exact && !exact {
						if !errors.Is(gotErr, ErrInexact) {
							t.Fatalf("Context{%d, %s}.Sqrt(%s) error = %v, want ErrInexact", precision, mode, x, gotErr)
						}
						continue
					}
					if gotErr != nil || !got.SameRepresentation(want) {
						t.Fatalf("Context{%d, %s}.Sqrt(%s) = %s [scale %d], %v; want %s [scale %d]", precision, mode, x, got, got.Scale(), gotErr, want, want.Scale())
					}
				}
			}
		}
	}
}

func TestContextPow_RejectsInvalidRoundingMode(t *testing.T) {
	if _, err := (Context{Rounding: RoundingMode(255)}).Pow(FromInt(1), -1); !errors.Is(err, ErrInvalidRoundingMode) {
		t.Fatalf("Context.Pow error = %v, want ErrInvalidRoundingMode", err)
	}
}

func TestContextPow_UnlimitedRequiresExactResult(t *testing.T) {
	if got, err := (Context{}).Pow(FromInt(2), -3); err != nil || got.String() != "0.125" {
		t.Fatalf("unlimited Context.Pow = %s, %v; want 0.125", got, err)
	}
	if _, err := (Context{}).Pow(FromInt(3), -1); !errors.Is(err, ErrInexact) {
		t.Fatalf("unlimited Context.Pow error = %v, want ErrInexact", err)
	}
}

func TestContextPower_ReturnsRoundedAndExactResults(t *testing.T) {
	if got, err := (Context{Precision: 3}).Pow(FromInt(3), -1); err != nil || got.String() != "0.333" {
		t.Fatalf("context 3^-1 = %s, %v", got, err)
	}
	for _, test := range []struct {
		name     string
		base     Decimal
		exponent int64
		want     Decimal
	}{
		{"above maximum scale", New(10, Scale(math.MaxInt64/2+1)), 2, New(10, Scale(math.MaxInt64))},
		{"negative above maximum scale", New(-10, Scale(math.MaxInt64/3+1)), 3, New(-10, Scale(math.MaxInt64))},
		{"below minimum scale", New(1, Scale(math.MinInt64/2-1)), 2, New(100, Scale(math.MinInt64))},
		{"zero above maximum scale", New(0, Scale(math.MaxInt64/2+1)), 2, New(0, Scale(math.MaxInt64))},
		{"zero below minimum scale", New(0, Scale(math.MinInt64/2-1)), 2, New(0, Scale(math.MinInt64))},
	} {
		t.Run("matches the oracle for "+test.name, func(t *testing.T) {
			got, err := (Context{}).Pow(test.base, test.exponent)
			if err != nil || !got.SameRepresentation(test.want) {
				t.Fatalf("Context.Pow = coefficient %s, scale %d, %v; want coefficient %s, scale %d", got.Coefficient(), got.Scale(), err, test.want.Coefficient(), test.want.Scale())
			}
		})
	}

	unrepresentablePower := New(1, Scale(math.MaxInt64/2+1))
	if _, err := (Context{}).Pow(unrepresentablePower, 2); !errors.Is(err, ErrRange) {
		t.Fatalf("unrepresentable context power error = %v", err)
	}
	extremeZero := New(0, Scale(math.MaxInt64))
	if _, err := (Context{Precision: 3}).Pow(extremeZero, -2); !errors.Is(err, ErrDivisionByZero) {
		t.Fatalf("context extreme zero negative power error = %v", err)
	}
}

func TestContextPower_RoundsExactIntegerPowersCorrectly(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		exponent  int64
		precision uint
		mode      RoundingMode
		want      string
	}{
		{
			"half-even above midpoint",
			"-63302298391728957e-9",
			3,
			40,
			HalfEven,
			"-2536637662116832651320077625622066548691e-16",
		},
		{
			"away from zero with leading discarded zeros",
			"2113000282280642673440e23",
			3,
			48,
			AwayFromZero,
			"943406067795409323186642398620748832508631496220e85",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := (Context{Precision: test.precision, Rounding: test.mode}).Pow(MustParse(test.input), test.exponent)
			want := MustParse(test.want)
			if err != nil || !got.SameRepresentation(want) {
				t.Fatalf("Context{%d, %s}.Pow(%s, %d) = %s [scale %d], %v; want %s [scale %d]",
					test.precision, test.mode, test.input, test.exponent, got, got.Scale(), err, want, want.Scale())
			}
		})
	}
}

func TestContextFromBigRat_RejectsInvalidRoundingMode(t *testing.T) {
	if _, err := (Context{Rounding: RoundingMode(255)}).FromBigRat(big.NewRat(1, 2)); !errors.Is(err, ErrInvalidRoundingMode) {
		t.Fatalf("Context.FromBigRat error = %v, want ErrInvalidRoundingMode", err)
	}
}

func TestContextFromBigRat_UnlimitedRequiresExactResult(t *testing.T) {
	if got, err := (Context{}).FromBigRat(big.NewRat(1, 8)); err != nil || got.String() != "0.125" {
		t.Fatalf("unlimited Context.FromBigRat = %s, %v; want 0.125", got, err)
	}
	if _, err := (Context{}).FromBigRat(big.NewRat(1, 3)); !errors.Is(err, ErrInexact) {
		t.Fatalf("unlimited Context.FromBigRat error = %v, want ErrInexact", err)
	}
}

func TestContextFromBigRat_RoundsRepeatingValues(t *testing.T) {
	if got, err := (Context{Precision: 3}).FromBigRat(big.NewRat(1, 3)); err != nil || got.String() != "0.333" {
		t.Fatalf("Context.FromBigRat = %s, %v", got, err)
	}
	assertContextPanics(t, func() { _, _ = (Context{}).FromBigRat(nil) })
}

func ExampleContext_Div() {
	ctx := Context{Precision: 4, Rounding: HalfEven}
	quotient, err := ctx.Div(FromInt(2), FromInt(3))
	if err != nil {
		fmt.Println("divide:", err)
		return
	}

	fmt.Println(quotient)
	// Output: 0.6667
}

func FuzzContextArithmeticAgainstBigRat(f *testing.F) {
	f.Add(int64(125), int64(1), int64(-25), int16(2), int16(100), int16(1), uint8(2), uint8(HalfEven), uint8(0))
	f.Add(int64(125), int64(1), int64(-25), int16(2), int16(100), int16(1), uint8(2), uint8(HalfEven), uint8(1))
	f.Add(int64(125), int64(1), int64(-25), int16(2), int16(100), int16(1), uint8(2), uint8(HalfEven), uint8(2))
	f.Add(int64(1), int64(-1), int64(7), int16(-100), int16(0), int16(20), uint8(3), uint8(Floor), uint8(3))
	f.Add(int64(125), int64(1), int64(-25), int16(2), int16(100), int16(1), uint8(2), uint8(HalfEven), uint8(4))
	f.Add(int64(-121), int64(-9), int64(-10), int16(77), int16(100), int16(-87), uint8(0), uint8(ZeroFiveUp), uint8(4))
	f.Fuzz(func(t *testing.T, xCoefficient, yCoefficient, zCoefficient int64, xScale, yScale, zScale int16, rawPrecision, rawMode, rawOperation uint8) {
		precision := uint(rawPrecision%20 + 1)
		mode := RoundingMode(rawMode % uint8(Exact+1))
		x := New(xCoefficient, Scale(xScale%101))
		if yCoefficient == 0 {
			yCoefficient = 1
		}
		y := New(yCoefficient, Scale(yScale%101))
		z := New(zCoefficient, Scale(zScale%101))
		xBefore := NewBig(x.Coefficient(), x.Scale())
		yBefore := NewBig(y.Coefficient(), y.Scale())
		zBefore := NewBig(z.Coefficient(), z.Scale())
		ctx := Context{Precision: precision, Rounding: mode}

		var got Decimal
		var gotErr error
		var exact *big.Rat
		var preferredScale Scale
		operation := "add"
		switch rawOperation % 5 {
		case 0:
			got, gotErr = ctx.Add(x, y)
			exact = new(big.Rat).Add(x.BigRat(), y.BigRat())
			preferredScale = max(x.Scale(), y.Scale())
		case 1:
			operation = "subtract"
			got, gotErr = ctx.Sub(x, y)
			exact = new(big.Rat).Sub(x.BigRat(), y.BigRat())
			preferredScale = max(x.Scale(), y.Scale())
		case 2:
			operation = "multiply"
			got, gotErr = ctx.Mul(x, y)
			exact = new(big.Rat).Mul(x.BigRat(), y.BigRat())
			preferredScale = x.Scale() + y.Scale()
		case 3:
			operation = "divide"
			got, gotErr = ctx.Div(x, y)
			exact = new(big.Rat).Quo(x.BigRat(), y.BigRat())
			preferredScale = x.Scale() - y.Scale()
		case 4:
			operation = "fma"
			got, gotErr = ctx.FMA(x, y, z)
			exact = new(big.Rat).Add(new(big.Rat).Mul(x.BigRat(), y.BigRat()), z.BigRat())
			preferredScale = max(x.Scale()+y.Scale(), z.Scale())
		}
		if !x.SameRepresentation(xBefore) || !y.SameRepresentation(yBefore) || !z.SameRepresentation(zBefore) {
			t.Fatalf("%s mutated an operand: x %s, y %s, z %s", operation, x, y, z)
		}
		want, resultExact := roundRatToPrecision(exact, preferredScale, precision, mode)
		if mode == Exact && !resultExact {
			if !errors.Is(gotErr, ErrInexact) {
				t.Fatalf("%s error = %v, want ErrInexact", operation, gotErr)
			}
			return
		}
		if gotErr != nil || !got.SameRepresentation(want) {
			t.Fatalf("%s = %s [scale %d], %v; want %s [scale %d]", operation, got, got.Scale(), gotErr, want, want.Scale())
		}
	})
}

var (
	contextBenchmarkDecimal Decimal
	errContextBenchmark     error
)

func assertContextPanics(t *testing.T, operation func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("context operation did not panic")
		}
	}()
	operation()
}

func BenchmarkContextRoundingMatrix(b *testing.B) {
	tests := []struct {
		name  string
		value Decimal
		ctx   Context
	}{
		{"exact", MustParse("1.234"), Context{Precision: 4, Rounding: Exact}},
		{"below_tie", MustParse("1.2344"), Context{Precision: 4, Rounding: HalfEven}},
		{"tie", MustParse("1.2345"), Context{Precision: 4, Rounding: HalfEven}},
		{"above_tie", MustParse("1.2346"), Context{Precision: 4, Rounding: HalfEven}},
		{"carry", MustParse("9.9995"), Context{Precision: 4, Rounding: HalfUp}},
		{"toward_zero", MustParse("-1.2345"), Context{Precision: 4, Rounding: TowardZero}},
		{"away_from_zero", MustParse("-1.2345"), Context{Precision: 4, Rounding: AwayFromZero}},
		{"floor", MustParse("-1.2345"), Context{Precision: 4, Rounding: Floor}},
		{"ceiling", MustParse("-1.2345"), Context{Precision: 4, Rounding: Ceiling}},
		{"zero_five_up", MustParse("1.2501"), Context{Precision: 3, Rounding: ZeroFiveUp}},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				contextBenchmarkDecimal, errContextBenchmark = test.ctx.Round(test.value)
			}
		})
	}
}

func BenchmarkContextAddOperandSizes(b *testing.B) {
	ctx := Context{Precision: 34, Rounding: HalfEven}
	thousandDigits := strings.Repeat("1234567890", 100)
	tests := []struct {
		name string
		x, y Decimal
	}{
		{"compact", New(12_345, 2), New(67_890, 3)},
		{"34_digits", MustParse("12345678901234567.89012345678901234"), MustParse("98765432109876543.21098765432109876")},
		{"1000_digits", MustParse(thousandDigits), MustParse("9" + thousandDigits[1:])},
		{"unbalanced", MustParse(thousandDigits), New(7, 3)},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				contextBenchmarkDecimal, errContextBenchmark = ctx.Add(test.x, test.y)
			}
		})
	}
}

func BenchmarkContextAdd(b *testing.B) {
	ctx := Context{Precision: 34, Rounding: HalfEven}
	x := MustParse("12345678901234567890.123456789")
	y := MustParse("98765432109876543210.987654321")
	b.ReportAllocs()
	for b.Loop() {
		contextBenchmarkDecimal, errContextBenchmark = ctx.Add(x, y)
	}
}

func BenchmarkContextAddWideScale(b *testing.B) {
	ctx := Context{Precision: 34, Rounding: HalfEven}
	x := New(1, -1_000_000_000)
	y := FromInt(1)
	b.ReportAllocs()
	for b.Loop() {
		contextBenchmarkDecimal, errContextBenchmark = ctx.Add(x, y)
	}
}

func BenchmarkContextMulOperandSizes(b *testing.B) {
	ctx := Context{Precision: 34, Rounding: HalfEven}
	thousandDigits := strings.Repeat("1234567890", 100)
	tests := []struct {
		name string
		x, y Decimal
	}{
		{"compact", New(12_345, 2), New(67_890, 3)},
		{"34_digits", MustParse("12345678901234567.89012345678901234"), MustParse("98765432109876543.21098765432109876")},
		{"1000_digits", MustParse(thousandDigits), MustParse("9" + thousandDigits[1:])},
		{"unbalanced", MustParse(thousandDigits), New(7, 3)},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				contextBenchmarkDecimal, errContextBenchmark = ctx.Mul(test.x, test.y)
			}
		})
	}
}

func BenchmarkContextMul(b *testing.B) {
	ctx := Context{Precision: 34, Rounding: HalfEven}
	x := MustParse("12345678901234567890.123456789")
	y := MustParse("98765432109876543210.987654321")
	b.ReportAllocs()
	for b.Loop() {
		contextBenchmarkDecimal, errContextBenchmark = ctx.Mul(x, y)
	}
}

func BenchmarkContextDiv(b *testing.B) {
	ctx := Context{Precision: 34, Rounding: HalfEven}
	x := MustParse("12345678901234567890.123456788")
	for _, test := range []struct {
		name    string
		divisor Decimal
	}{
		{"terminating", FromInt(8)},
		{"repeating", FromInt(3)},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				contextBenchmarkDecimal, errContextBenchmark = ctx.Div(x, test.divisor)
			}
		})
	}
}

func BenchmarkContextFMA(b *testing.B) {
	ctx := Context{Precision: 34, Rounding: HalfEven}
	x := MustParse("9.999999999999999999")
	y := MustParse("9.999999999999999999")
	z := MustParse("-99.99999999999999997")
	b.ReportAllocs()
	for b.Loop() {
		contextBenchmarkDecimal, errContextBenchmark = ctx.FMA(x, y, z)
	}
}

func BenchmarkContextFMAZeroOperands(b *testing.B) {
	ctx := Context{Precision: 34, Rounding: HalfEven}
	for _, test := range []struct {
		name    string
		x, y, z Decimal
	}{
		{"zero product", New(0, 5), FromInt(7), New(12, 2)},
		{"zero addend", New(12, 2), New(3, 1), New(0, 5)},
	} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				contextBenchmarkDecimal, errContextBenchmark = ctx.FMA(test.x, test.y, test.z)
			}
		})
	}
}

func BenchmarkContextFMAWideScaleZeroFiveUp(b *testing.B) {
	ctx := Context{Precision: 1, Rounding: ZeroFiveUp}
	x := New(-121, 77)
	y := New(-9, 100)
	z := New(-10, -87)
	b.ReportAllocs()
	for b.Loop() {
		contextBenchmarkDecimal, errContextBenchmark = ctx.FMA(x, y, z)
	}
}

func BenchmarkContextSqrtInexact(b *testing.B) {
	ctx := Context{Precision: 34, Rounding: HalfEven}
	x := MustParse("2.000000000000000000000000000000000")
	b.ReportAllocs()
	for b.Loop() {
		contextBenchmarkDecimal, errContextBenchmark = ctx.Sqrt(x)
	}
}

func BenchmarkContextSqrtExact(b *testing.B) {
	ctx := Context{Precision: 34, Rounding: HalfEven}
	root := MustParse("1234567890123456789012345678901234")
	x, err := root.Mul(root)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		contextBenchmarkDecimal, errContextBenchmark = ctx.Sqrt(x)
	}
}

func BenchmarkContextSqrtExactSmall(b *testing.B) {
	ctx := Context{Precision: 34, Rounding: HalfEven}
	x := MustParse("2.25")
	b.ReportAllocs()
	for b.Loop() {
		contextBenchmarkDecimal, errContextBenchmark = ctx.Sqrt(x)
	}
}

func BenchmarkContextPow(b *testing.B) {
	ctx := Context{Precision: 34, Rounding: HalfEven}
	x := MustParse("1.0000000000000000001")
	b.ReportAllocs()
	for b.Loop() {
		contextBenchmarkDecimal, errContextBenchmark = ctx.Pow(x, -12)
	}
}
