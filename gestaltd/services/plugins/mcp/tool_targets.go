package mcp

import "sync"

type toolTargetRef struct {
	provider  string
	operation string
}

type toolTargetIndex struct {
	mu   sync.RWMutex
	refs map[string]toolTargetRef
}

func newToolTargetIndex() *toolTargetIndex {
	return &toolTargetIndex{refs: map[string]toolTargetRef{}}
}

func (i *toolTargetIndex) Store(toolName, provider, operation string) {
	if i == nil || toolName == "" || provider == "" || operation == "" {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.refs[toolName] = toolTargetRef{provider: provider, operation: operation}
}

func (i *toolTargetIndex) Lookup(toolName string) (toolTargetRef, bool) {
	if i == nil || toolName == "" {
		return toolTargetRef{}, false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	ref, ok := i.refs[toolName]
	return ref, ok
}
