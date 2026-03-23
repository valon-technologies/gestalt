package sandbox

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Pool struct {
	mu          sync.Mutex
	processes   map[string]*Process
	count       int
	maxSize     int
	pythonCmd   string
	script      string
	toolAddr    string
	idleTimeout time.Duration
}

func NewPool(pythonCmd, script, toolAddr string, maxSize int, idleTimeout time.Duration) *Pool {
	if maxSize <= 0 {
		maxSize = 5
	}
	if idleTimeout <= 0 {
		idleTimeout = 5 * time.Minute
	}
	return &Pool{
		processes:   make(map[string]*Process),
		maxSize:     maxSize,
		pythonCmd:   pythonCmd,
		script:      script,
		toolAddr:    toolAddr,
		idleTimeout: idleTimeout,
	}
}

func (p *Pool) Acquire() (*Process, error) {
	p.mu.Lock()

	for _, proc := range p.processes {
		if !proc.IsBusy() {
			proc.SetBusy()
			p.mu.Unlock()
			return proc, nil
		}
	}

	if p.count >= p.maxSize {
		p.mu.Unlock()
		return nil, fmt.Errorf("sandbox pool exhausted (max %d)", p.maxSize)
	}

	p.count++
	p.mu.Unlock()

	id := uuid.NewString()[:8]
	proc, err := SpawnProcess(id, p.pythonCmd, p.script, p.toolAddr)
	if err != nil {
		p.mu.Lock()
		p.count--
		p.mu.Unlock()
		return nil, fmt.Errorf("spawning sandbox: %w", err)
	}

	proc.SetBusy()

	p.mu.Lock()
	p.processes[proc.ID()] = proc
	p.mu.Unlock()

	return proc, nil
}

func (p *Pool) Release(proc *Process) {
	proc.SetIdle()
}

func (p *Pool) Shutdown(ctx context.Context) {
	p.mu.Lock()
	procs := make([]*Process, 0, len(p.processes))
	for _, proc := range p.processes {
		procs = append(procs, proc)
	}
	p.processes = make(map[string]*Process)
	p.count = 0
	p.mu.Unlock()

	var wg sync.WaitGroup
	for _, proc := range procs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("shutting down sandbox %s", proc.ID())
			proc.Shutdown(ctx)
		}()
	}
	wg.Wait()
}
