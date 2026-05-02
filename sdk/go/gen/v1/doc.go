// Package proto preserves a limited subset of the historical generated
// protobuf import path.
//
// New provider code should prefer the authored gestalt SDK APIs where they are
// available. These names alias the SDK's internal transport types only for
// existing providers and test fixtures that still need a low-level protocol
// surface during migration.
package proto
