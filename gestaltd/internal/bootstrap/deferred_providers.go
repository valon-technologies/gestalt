package bootstrap

import "sync"

type deferredProviders struct {
	mu             sync.Mutex
	triggered      bool
	connAuth       func() map[string]map[string]OAuthHandler
	manualConnAuth func() map[string]map[string]ManualTokenExchanger

	done       chan struct{}
	finishOnce sync.Once
}

func newDeferredProviders() *deferredProviders {
	return &deferredProviders{done: make(chan struct{})}
}

func (d *deferredProviders) ready() <-chan struct{} {
	return d.done
}

func (d *deferredProviders) markActivating() {
	d.mu.Lock()
	d.triggered = true
	d.mu.Unlock()
}

func (d *deferredProviders) set(
	connAuth func() map[string]map[string]OAuthHandler,
	manualConnAuth func() map[string]map[string]ManualTokenExchanger,
) {
	d.mu.Lock()
	d.triggered = true
	d.connAuth = connAuth
	d.manualConnAuth = manualConnAuth
	d.mu.Unlock()
}

func (d *deferredProviders) finish() {
	d.finishOnce.Do(func() { close(d.done) })
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
	d.mu.Unlock()
	if triggered {
		<-d.done
	}
}
