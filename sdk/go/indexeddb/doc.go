// Package indexeddb carries the IndexedDB provider contract types shared by
// store implementations and the daemon.
//
// Query parameters use any for keys because Go has no recursive union type.
// Valid keys are number, time.Time, string, []byte, or arrays of keys; invalid
// keys panic when building ranges or queries.
package indexeddb
