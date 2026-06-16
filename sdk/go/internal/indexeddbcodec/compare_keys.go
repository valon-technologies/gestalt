package indexeddbcodec

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"time"
	"unicode/utf16"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type keyKind int

const (
	keyKindNumber keyKind = iota
	keyKindDate
	keyKindString
	keyKindBinary
	keyKindArray
)

// CompareKeys compares two native IndexedDB keys using W3C ordering:
// number < date < string < binary < array, with element-wise array compare
// and shorter prefixes sorting first.
func CompareKeys(a, b any) int {
	ka, okA := keyKindOf(a)
	kb, okB := keyKindOf(b)
	if !okA || !okB {
		return compareFallback(a, b)
	}
	if ka != kb {
		if ka < kb {
			return -1
		}
		return 1
	}
	switch ka {
	case keyKindNumber:
		return compareNumbers(a, b)
	case keyKindDate:
		return compareDates(a.(time.Time), b.(time.Time))
	case keyKindString:
		return compareUTF16Strings(a.(string), b.(string))
	case keyKindBinary:
		return bytes.Compare(a.([]byte), b.([]byte))
	case keyKindArray:
		return compareArrayKeys(keyArrayParts(a), keyArrayParts(b))
	default:
		return compareFallback(a, b)
	}
}

// KeyInRange reports whether key satisfies kr. A nil range includes all keys.
func KeyInRange(key any, kr *proto.KeyRange) (bool, error) {
	if kr == nil {
		return true, nil
	}
	if kr.GetLower() != nil {
		lower, err := KeyValueToAny(kr.GetLower())
		if err != nil {
			return false, err
		}
		cmp := CompareKeys(key, lower)
		if kr.GetLowerOpen() {
			if cmp <= 0 {
				return false, nil
			}
		} else if cmp < 0 {
			return false, nil
		}
	}
	if kr.GetUpper() != nil {
		upper, err := KeyValueToAny(kr.GetUpper())
		if err != nil {
			return false, err
		}
		cmp := CompareKeys(key, upper)
		if kr.GetUpperOpen() {
			if cmp >= 0 {
				return false, nil
			}
		} else if cmp > 0 {
			return false, nil
		}
	}
	return true, nil
}

func keyKindOf(v any) (keyKind, bool) {
	if v == nil {
		return 0, false
	}
	if parts, ok := KeyValueArrayParts(v); ok {
		_ = parts
		return keyKindArray, true
	}
	switch v.(type) {
	case []byte:
		return keyKindBinary, true
	case time.Time:
		return keyKindDate, true
	case string:
		return keyKindString, true
	case bool:
		return keyKindNumber, true
	}
	if _, ok := numberRat(v); ok {
		return keyKindNumber, true
	}
	return 0, false
}

func keyArrayParts(v any) []any {
	if parts, ok := KeyValueArrayParts(v); ok {
		return parts
	}
	return nil
}

func compareArrayKeys(a, b []any) int {
	for i := range a {
		if i >= len(b) {
			return 1
		}
		if cmp := CompareKeys(a[i], b[i]); cmp != 0 {
			return cmp
		}
	}
	if len(a) < len(b) {
		return -1
	}
	return 0
}

func compareDates(a, b time.Time) int {
	av := a.UnixNano()
	bv := b.UnixNano()
	switch {
	case av < bv:
		return -1
	case av > bv:
		return 1
	default:
		return 0
	}
}

func compareUTF16Strings(a, b string) int {
	au := utf16.Encode([]rune(a))
	bu := utf16.Encode([]rune(b))
	limit := len(au)
	if len(bu) < limit {
		limit = len(bu)
	}
	for i := 0; i < limit; i++ {
		if au[i] < bu[i] {
			return -1
		}
		if au[i] > bu[i] {
			return 1
		}
	}
	switch {
	case len(au) < len(bu):
		return -1
	case len(au) > len(bu):
		return 1
	default:
		return 0
	}
}

func compareNumbers(a, b any) int {
	ar, okA := numberRat(a)
	br, okB := numberRat(b)
	if !okA || !okB {
		return compareFallback(a, b)
	}
	return ar.Cmp(br)
}

func numberRat(v any) (*big.Rat, bool) {
	switch n := v.(type) {
	case bool:
		if n {
			return big.NewRat(1, 1), true
		}
		return big.NewRat(0, 1), true
	case int:
		return big.NewRat(int64(n), 1), true
	case int8:
		return big.NewRat(int64(n), 1), true
	case int16:
		return big.NewRat(int64(n), 1), true
	case int32:
		return big.NewRat(int64(n), 1), true
	case int64:
		return big.NewRat(n, 1), true
	case uint:
		return new(big.Rat).SetInt(new(big.Int).SetUint64(uint64(n))), true
	case uint8:
		return new(big.Rat).SetInt(new(big.Int).SetUint64(uint64(n))), true
	case uint16:
		return new(big.Rat).SetInt(new(big.Int).SetUint64(uint64(n))), true
	case uint32:
		return new(big.Rat).SetInt(new(big.Int).SetUint64(uint64(n))), true
	case uint64:
		return new(big.Rat).SetInt(new(big.Int).SetUint64(n)), true
	case float32:
		return floatRat(float64(n))
	case float64:
		return floatRat(n)
	default:
		return nil, false
	}
}

func floatRat(v float64) (*big.Rat, bool) {
	if math.IsNaN(v) {
		return nil, false
	}
	r := new(big.Rat).SetFloat64(v)
	if r == nil {
		return nil, false
	}
	return r, true
}

func compareFallback(a, b any) int {
	as := fmtString(a)
	bs := fmtString(b)
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}

func fmtString(v any) string {
	return fmt.Sprint(v)
}
