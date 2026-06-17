package gestalt

import (
	"sort"

	"github.com/valon-technologies/gestalt/sdk/go/client"
	"github.com/valon-technologies/gestalt/sdk/go/indexeddb"
)

// IndexedDBCursorSnapshotEntry is one provider-side cursor row.
type IndexedDBCursorSnapshotEntry struct {
	// Key is the cursor key. For index cursors, this is the index key.
	Key any
	// PrimaryKey is the canonical object-store primary key for the row.
	PrimaryKey string
	// PrimaryKeyValue is the primary key value used as a stable tie-breaker
	// when multiple index rows share the same index key.
	PrimaryKeyValue any
	// Record is the row value. It is omitted when a key-only cursor entry is
	// returned to clients.
	Record Record
}

// IndexedDBCursorSnapshot provides IndexedDB cursor ordering, range filtering,
// and movement semantics for native providers.
type IndexedDBCursorSnapshot struct {
	// IndexCursor indicates whether Key contains secondary-index values.
	IndexCursor bool
	// KeysOnly indicates whether callers should omit Record from returned
	// cursor entries.
	KeysOnly bool
	// Reverse orders entries from greatest to least key.
	Reverse bool
	// Unique collapses duplicate index keys while iterating.
	Unique bool
	// Entries is the sorted and range-filtered snapshot used by cursor movement.
	Entries []IndexedDBCursorSnapshotEntry
	// Pos is the current cursor position. A value of -1 means unpositioned.
	Pos int
}

// NewIndexedDBCursorSnapshot creates an empty provider-side cursor snapshot
// configured from a native open-cursor request.
func NewIndexedDBCursorSnapshot(req IndexedDBOpenCursorRequest) IndexedDBCursorSnapshot {
	return IndexedDBCursorSnapshot{
		IndexCursor: req.Index != "",
		KeysOnly:    req.KeysOnly,
		Reverse:     req.Direction == CursorPrev || req.Direction == CursorPrevUnique,
		Unique:      req.Direction == CursorNextUnique || req.Direction == CursorPrevUnique,
		Pos:         -1,
	}
}

// Load sorts entries, applies the supplied query, and stores the resulting
// cursor snapshot.
func (s *IndexedDBCursorSnapshot) Load(entries []IndexedDBCursorSnapshotEntry, query *client.IndexedDBQuery) error {
	sort.Slice(entries, func(i, j int) bool {
		cmp := indexeddb.CompareKeys(entries[i].Key, entries[j].Key)
		if cmp == 0 {
			cmp = indexeddb.CompareKeys(entries[i].PrimaryKeyValue, entries[j].PrimaryKeyValue)
		}
		if s.Reverse {
			return cmp > 0
		}
		return cmp < 0
	})

	filtered, err := ApplyIndexedDBQuery(entries, query)
	if err != nil {
		return err
	}
	s.Entries = filtered
	s.Pos = -1
	return nil
}

// ApplyIndexedDBQuery returns entries that satisfy query without mutating the snapshot.
func ApplyIndexedDBQuery(entries []IndexedDBCursorSnapshotEntry, query *client.IndexedDBQuery) ([]IndexedDBCursorSnapshotEntry, error) {
	if query == nil {
		return entries, nil
	}
	filtered := make([]IndexedDBCursorSnapshotEntry, 0, len(entries))
	for _, entry := range entries {
		ok, err := indexeddb.MatchQuery(entry.Key, query)
		if err != nil {
			return nil, err
		}
		if ok {
			filtered = append(filtered, entry)
		}
	}
	return filtered, nil
}

// Next advances the snapshot to the next entry. It returns nil when the
// snapshot is exhausted.
func (s *IndexedDBCursorSnapshot) Next() (*IndexedDBCursorSnapshotEntry, error) {
	if s.Unique && s.IndexCursor && s.Pos >= 0 && s.Pos < len(s.Entries) {
		prev := s.Entries[s.Pos].Key
		for s.Pos++; s.Pos < len(s.Entries); s.Pos++ {
			if indexeddb.CompareKeys(s.Entries[s.Pos].Key, prev) != 0 {
				return s.Current()
			}
		}
		return nil, nil
	}

	s.Pos++
	if s.Pos >= len(s.Entries) {
		return nil, nil
	}
	return s.Current()
}

// ContinueToKey advances to target or the next entry past target according to
// the snapshot direction. It returns nil when the snapshot is exhausted.
func (s *IndexedDBCursorSnapshot) ContinueToKey(target any) (*IndexedDBCursorSnapshotEntry, error) {
	var prev any
	if s.Unique && s.IndexCursor && s.Pos >= 0 && s.Pos < len(s.Entries) {
		prev = s.Entries[s.Pos].Key
	}
	for s.Pos++; s.Pos < len(s.Entries); s.Pos++ {
		cur := s.Entries[s.Pos].Key
		if prev != nil && s.Unique && s.IndexCursor && indexeddb.CompareKeys(cur, prev) == 0 {
			continue
		}
		cmp := indexeddb.CompareKeys(cur, target)
		if s.Reverse {
			if cmp <= 0 {
				return s.Current()
			}
			continue
		}
		if cmp >= 0 {
			return s.Current()
		}
	}
	return nil, nil
}

// Advance skips count entries from the current position and returns the new
// current entry. It returns nil when the snapshot is exhausted.
func (s *IndexedDBCursorSnapshot) Advance(count int) (*IndexedDBCursorSnapshotEntry, error) {
	if count <= 0 {
		return nil, InvalidArgument("advance count must be positive")
	}
	var entry *IndexedDBCursorSnapshotEntry
	for i := 0; i < count; i++ {
		var err error
		entry, err = s.Next()
		if entry == nil || err != nil {
			return entry, err
		}
	}
	return entry, nil
}

// Current returns the currently positioned entry.
func (s *IndexedDBCursorSnapshot) Current() (*IndexedDBCursorSnapshotEntry, error) {
	if s.Pos < 0 || s.Pos >= len(s.Entries) {
		return nil, ErrNotFound
	}
	return &s.Entries[s.Pos], nil
}

// CompareIndexedDBValues compares native IndexedDB key values using W3C ordering.
func CompareIndexedDBValues(a, b any) int {
	return indexeddb.CompareKeys(a, b)
}
