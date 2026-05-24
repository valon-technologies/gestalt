package indexeddb

// Only matches a single key value.
func Only(value any) *KeyRange {
	return &KeyRange{Lower: value, Upper: value}
}

// LowerBound sets a lower bound; open=true means exclusive (>).
func LowerBound(value any, open bool) *KeyRange {
	return &KeyRange{Lower: value, LowerOpen: open}
}

// UpperBound sets an upper bound; open=true means exclusive (<).
func UpperBound(value any, open bool) *KeyRange {
	return &KeyRange{Upper: value, UpperOpen: open}
}

// Bound sets both bounds.
func Bound(lower, upper any, lowerOpen, upperOpen bool) *KeyRange {
	return &KeyRange{Lower: lower, Upper: upper, LowerOpen: lowerOpen, UpperOpen: upperOpen}
}

// Includes reports whether key is within the range. Nil range includes all keys.
func (r *KeyRange) Includes(key any) bool {
	if r == nil {
		return true
	}
	if r.Lower != nil {
		if cmp, ok := compareKeys(key, r.Lower); ok {
			if r.LowerOpen {
				if cmp <= 0 {
					return false
				}
			} else if cmp < 0 {
				return false
			}
		}
	}
	if r.Upper != nil {
		if cmp, ok := compareKeys(key, r.Upper); ok {
			if r.UpperOpen {
				if cmp >= 0 {
					return false
				}
			} else if cmp > 0 {
				return false
			}
		}
	}
	return true
}

func compareKeys(a, b any) (int, bool) {
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		if !ok {
			return 0, false
		}
		switch {
		case av < bv:
			return -1, true
		case av > bv:
			return 1, true
		default:
			return 0, true
		}
	case int:
		return compareIntKey(av, b)
	case int64:
		return compareInt64Key(av, b)
	case float64:
		return compareFloatKey(av, b)
	default:
		return 0, false
	}
}

func compareIntKey(av int, b any) (int, bool) {
	switch bv := b.(type) {
	case int:
		switch {
		case av < bv:
			return -1, true
		case av > bv:
			return 1, true
		default:
			return 0, true
		}
	case int64:
		return compareInt64Key(int64(av), bv)
	case float64:
		return compareFloatKey(float64(av), bv)
	default:
		return 0, false
	}
}

func compareInt64Key(av int64, b any) (int, bool) {
	bv, ok := b.(int64)
	if !ok {
		if bi, ok := b.(int); ok {
			bv = int64(bi)
		} else {
			return 0, false
		}
	}
	switch {
	case av < bv:
		return -1, true
	case av > bv:
		return 1, true
	default:
		return 0, true
	}
}

func compareFloatKey(av float64, b any) (int, bool) {
	bv, ok := b.(float64)
	if !ok {
		return 0, false
	}
	switch {
	case av < bv:
		return -1, true
	case av > bv:
		return 1, true
	default:
		return 0, true
	}
}
