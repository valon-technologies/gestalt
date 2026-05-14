package bootstrap

import (
	"context"
	"fmt"
	"sync"

	coremodel "github.com/valon-technologies/gestalt/server/core/model"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/models/modelmanager"
)

type lazyModelManager struct {
	mu     sync.RWMutex
	target modelmanager.Service
}

func newLazyModelManager() *lazyModelManager {
	return &lazyModelManager{}
}

func (l *lazyModelManager) SetTarget(target modelmanager.Service) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.target = target
}

func (l *lazyModelManager) Generate(ctx context.Context, p *principal.Principal, req coremodel.ManagerGenerateRequest) (*coremodel.GenerateResponse, error) {
	target, err := l.current()
	if err != nil {
		return nil, err
	}
	return target.Generate(ctx, p, req)
}

func (l *lazyModelManager) current() (modelmanager.Service, error) {
	l.mu.RLock()
	target := l.target
	l.mu.RUnlock()
	if target == nil {
		return nil, fmt.Errorf("model manager is not available")
	}
	return target, nil
}

var _ modelmanager.Service = (*lazyModelManager)(nil)
