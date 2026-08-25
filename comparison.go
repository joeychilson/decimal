package decimal

import (
	"cmp"
	"hash/maphash"
)

// Cmp compares d and x numerically and returns -1, 0, or +1. It returns zero
// for numerically equal values even when their scales differ.
func (d Decimal) Cmp(x Decimal) int {
	dc, ds := decimalParts(d)
	xc, xs := decimalParts(x)
	dSign := dc.Sign()
	xSign := xc.Sign()
	if dSign != xSign {
		return cmp.Compare(dSign, xSign)
	}
	if dSign == 0 {
		return 0
	}
	if ds == xs {
		return dc.Cmp(xc)
	}
	var comparison int
	if ds > xs {
		comparison = compareScaledByPowerOfTen(dc, xc, scaleDistance(ds, xs))
	} else {
		comparison = -compareScaledByPowerOfTen(xc, dc, scaleDistance(xs, ds))
	}
	if dSign < 0 {
		return -comparison
	}
	return comparison
}

// Equal reports whether d and x have the same numeric value. It is equivalent
// to d.Cmp(x) == 0.
func (d Decimal) Equal(x Decimal) bool {
	return d.Cmp(x) == 0
}

// SameRepresentation reports whether d and x have the same numeric value and
// the same scale. It distinguishes 1.20 from 1.2.
func (d Decimal) SameRepresentation(x Decimal) bool {
	if d.value == x.value {
		return true
	}
	dc, ds := decimalParts(d)
	xc, xs := decimalParts(x)
	return ds == xs && dc.Cmp(xc) == 0
}

// CompareTotal defines a deterministic total order. It first compares numeric
// values, then orders numerically equal values by scale, with the smaller scale
// first. CompareTotal is useful for stable serialization and tests; ordinary
// numeric sorting should use Cmp.
func (d Decimal) CompareTotal(x Decimal) int {
	if comparison := d.Cmp(x); comparison != 0 {
		return comparison
	}
	return cmp.Compare(d.Scale(), x.Scale())
}

// Min returns the numerically smaller of x and y. If they are numerically equal,
// it returns x and therefore preserves x's representation.
func Min(x, y Decimal) Decimal {
	if x.Cmp(y) <= 0 {
		return x
	}
	return y
}

// Max returns the numerically larger of x and y. If they are numerically equal,
// it returns x and therefore preserves x's representation.
func Max(x, y Decimal) Decimal {
	if x.Cmp(y) >= 0 {
		return x
	}
	return y
}

// Clamp returns min if x < min, max if x > max, and x otherwise. It panics if
// min > max. Numerically equal boundary values do not replace x's representation.
func Clamp(x, min, max Decimal) Decimal {
	if min.Cmp(max) > 0 {
		panic("decimal: invalid Clamp range")
	}
	if x.Cmp(min) < 0 {
		return min
	}
	if x.Cmp(max) > 0 {
		return max
	}
	return x
}

// NumericHasher implements [maphash.Hasher] for Decimal using numeric equality.
// It treats representations such as 1.20 and 1.2 as the same key. The type is
// stateless; its zero value is ready to use.
//
// Built-in Go maps still require comparable keys. NumericHasher is intended for
// hash containers that accept a maphash.Hasher[Decimal].
type NumericHasher struct{}

// Hash writes a canonical numeric encoding of d to h. It does not finalize or
// reset h. Hash panics if h is nil.
func (NumericHasher) Hash(h *maphash.Hash, d Decimal) {
	if h == nil {
		panic("decimal: NumericHasher.Hash on nil *maphash.Hash")
	}
	canonical := d.Canonical()
	coefficient, scale := decimalParts(canonical)
	maphash.WriteComparable(h, int64(scale))
	// Hash.WriteByte and Hash.Write never return errors.
	if coefficient.Sign() < 0 {
		_ = h.WriteByte(1)
	} else {
		_ = h.WriteByte(0)
	}
	_, _ = h.Write(coefficient.Bytes())
}

// Equal reports numeric equality, with the same semantics as [Decimal.Equal].
func (NumericHasher) Equal(x, y Decimal) bool {
	return x.Equal(y)
}
