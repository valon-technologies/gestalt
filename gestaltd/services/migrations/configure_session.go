package migrations

import (
	"strings"
	"sync"
)

// ConfigureSessionRegistry tracks provider processes currently executing configure
// or migration. Workflow migrations are only accepted while the caller app is
// registered here.
type ConfigureSessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]int
}

func NewConfigureSessionRegistry() *ConfigureSessionRegistry {
	return &ConfigureSessionRegistry{sessions: map[string]int{}}
}

func (r *ConfigureSessionRegistry) Begin(appName string) {
	if r == nil {
		return
	}
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[appName]++
}

func (r *ConfigureSessionRegistry) End(appName string) {
	if r == nil {
		return
	}
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	count := r.sessions[appName]
	if count <= 1 {
		delete(r.sessions, appName)
		return
	}
	r.sessions[appName] = count - 1
}

func (r *ConfigureSessionRegistry) Active(appName string) bool {
	if r == nil {
		return false
	}
	appName = strings.TrimSpace(appName)
	if appName == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessions[appName] > 0
}
