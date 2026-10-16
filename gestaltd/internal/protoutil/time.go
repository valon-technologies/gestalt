package protoutil

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// TimeToProto converts an optional time, preserving nil.
func TimeToProto(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

// TimeFromProto converts an optional protobuf timestamp, preserving nil.
func TimeFromProto(t *timestamppb.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	value := t.AsTime()
	return &value
}
