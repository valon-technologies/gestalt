package sandbox

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Pool struct {
	mu          sync.Mutex
	processes   map[string]*sandboxProcess
	pythonCmd   string
	script      string
	toolAddr    string
	maxSize     int
	idleTimeout time.Duration
}

func NewPool(pythonCmd, script, toolAddr string, maxSize int, idleTimeout time.Duration) *Pool {
	return &Pool{
		processes:   make(map[string]*sandboxProcess),
		pythonCmd:   pythonCmd,
		script:      script,
		toolAddr:    toolAddr,
		maxSize:     maxSize,
		idleTimeout: idleTimeout,
	}
}

func (p *Pool) Acquire(ctx context.Context) (*sandboxProcess, error) {
	p.mu.Lock()

	for _, proc := range p.processes {
		if !proc.busy.Load() {
			proc.busy.Store(true)
			p.mu.Unlock()
			return proc, nil
		}
	}

	if len(p.processes) >= p.maxSize {
		p.mu.Unlock()
		return nil, fmt.Errorf("sandbox pool exhausted (max %d)", p.maxSize)
	}
	p.mu.Unlock()

	proc, err := spawnProcess(ctx, p.pythonCmd, p.script, p.toolAddr)
	if err != nil {
		return nil, err
	}
	proc.busy.Store(true)

	p.mu.Lock()
	p.processes[proc.id] = proc
	p.mu.Unlock()

	return proc, nil
}

func (p *Pool) Release(proc *sandboxProcess) {
	proc.busy.Store(false)
}

func (p *Pool) Shutdown(ctx context.Context) {
	p.mu.Lock()
	procs := make([]*sandboxProcess, 0, len(p.processes))
	for _, proc := range p.processes {
		procs = append(procs, proc)
	}
	p.processes = make(map[string]*sandboxProcess)
	p.mu.Unlock()

	var wg sync.WaitGroup
	for _, proc := range procs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = proc.shutdown(ctx)
		}()
	}
	wg.Wait()
}
