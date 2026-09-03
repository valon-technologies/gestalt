package observability

import (
	"strings"
	"sync"
	"time"
)

// DefaultInvocationRecordCapacity bounds each provider's process-local history.
const DefaultInvocationRecordCapacity = 256

type InvocationOutcome string

const (
	InvocationPassed InvocationOutcome = "passed"
	InvocationFailed InvocationOutcome = "failed"
)

// InvocationRecord is the privacy-safe summary of one completed operation.
// It intentionally excludes invocation parameters, response bodies, caller
// identity, and error payloads.
type InvocationRecord struct {
	ID        uint64
	Provider  string
	Operation string
	Outcome   InvocationOutcome
	Status    int
	Duration  time.Duration
	Timestamp time.Time
}

// InvocationRecordRecorder accepts completed invocation records.
type InvocationRecordRecorder interface {
	RecordInvocation(InvocationRecord)
}

// InvocationRecordReader reads recent records for one provider.
type InvocationRecordReader interface {
	RecentInvocations(provider string, limit int) []InvocationRecord
}

// InvocationRecordStore retains recent records in memory for the lifetime of
// one gestaltd process. Capacity is applied independently to each provider so
// traffic from one app cannot evict another app's recent history.
type InvocationRecordStore struct {
	mu       sync.RWMutex
	capacity int
	nextID   uint64
	records  map[string][]InvocationRecord
}

func NewInvocationRecordStore(capacity int) *InvocationRecordStore {
	if capacity <= 0 {
		capacity = DefaultInvocationRecordCapacity
	}
	return &InvocationRecordStore{
		capacity: capacity,
		records:  make(map[string][]InvocationRecord),
	}
}

func (s *InvocationRecordStore) RecordInvocation(record InvocationRecord) {
	if s == nil {
		return
	}
	record.Provider = strings.TrimSpace(record.Provider)
	record.Operation = strings.TrimSpace(record.Operation)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	record.ID = s.nextID
	records := s.records[record.Provider]
	if len(records) == s.capacity {
		copy(records, records[1:])
		records = records[:len(records)-1]
	}
	s.records[record.Provider] = append(records, record)
}

func (s *InvocationRecordStore) RecentInvocations(provider string, limit int) []InvocationRecord {
	if s == nil || limit <= 0 {
		return nil
	}
	provider = strings.TrimSpace(provider)

	s.mu.RLock()
	defer s.mu.RUnlock()
	records := s.records[provider]
	result := make([]InvocationRecord, 0, min(limit, len(records)))
	for i := len(records) - 1; i >= 0 && len(result) < limit; i-- {
		result = append(result, records[i])
	}
	return result
}

var _ InvocationRecordRecorder = (*InvocationRecordStore)(nil)
var _ InvocationRecordReader = (*InvocationRecordStore)(nil)
