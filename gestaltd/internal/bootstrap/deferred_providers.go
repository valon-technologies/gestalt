package bootstrap

import "sync"

type deferredProviders struct {
	mu             sync.Mutex
	triggered      bool
	ready          <-chan struct{}
	connAuth       func() map[string]map[string]OAuthHandler
	manualConnAuth func() map[string]map[string]ManualTokenExchanger
}

func (d *deferredProviders) set(
	ready <-chan struct{},
	connAuth func() map[string]map[string]OAuthHandler,
	manualConnAuth func() map[string]map[string]ManualTokenExchanger,
) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.triggered = true
	d.ready = ready
	d.connAuth = connAuth
	d.manualConnAuth = manualConnAuth
}

func (d *deferredProviders) connectionAuth() map[string]map[string]OAuthHandler {
	d.mu.Lock()
	fn := d.connAuth
	d.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn()
}

func (d *deferredProviders) manualConnectionAuth() map[string]map[string]ManualTokenExchanger {
	d.mu.Lock()
	fn := d.manualConnAuth
	d.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn()
}

func (d *deferredProviders) waitReady() {
	if d == nil {
		return
	}
	d.mu.Lock()
	triggered := d.triggered
	ready := d.ready
	d.mu.Unlock()
	if triggered && ready != nil {
		<-ready
	}
}
