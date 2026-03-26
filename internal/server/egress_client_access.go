package server

import (
	"context"
	"errors"
	"net/http"
	"sort"

	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/internal/principal"
)

var errEgressClientScopeForbidden = errors.New("egress client scope forbidden")

type egressClientActor struct {
	userID string
	admin  bool
}

func (s *Server) currentEgressClientActor(w http.ResponseWriter, r *http.Request) (egressClientActor, bool) {
	userID, ok := s.resolveUserID(w, r)
	if !ok {
		return egressClientActor{}, false
	}
	return egressClientActor{
		userID: userID,
		admin:  s.isAdmin(principal.FromContext(r.Context())),
	}, true
}

func normalizeEgressClientScope(scope string) (string, error) {
	if scope == "" {
		return core.EgressClientScopePersonal, nil
	}
	if scope != core.EgressClientScopePersonal && scope != core.EgressClientScopeGlobal {
		return "", errors.New("invalid scope")
	}
	return scope, nil
}

func validateOptionalEgressClientScope(scope string) error {
	if scope == "" {
		return nil
	}
	_, err := normalizeEgressClientScope(scope)
	return err
}

func (a egressClientActor) canManage(client *core.EgressClient) bool {
	switch client.Scope {
	case core.EgressClientScopeGlobal:
		return a.admin
	default:
		return client.CreatedByID == a.userID
	}
}

func (a egressClientActor) canCreate(scope string) bool {
	return scope != core.EgressClientScopeGlobal || a.admin
}

func (a egressClientActor) listFilters(scope string) ([]core.EgressClientFilter, error) {
	switch {
	case scope == core.EgressClientScopeGlobal && !a.admin:
		return nil, errEgressClientScopeForbidden
	case scope == core.EgressClientScopeGlobal:
		return []core.EgressClientFilter{{Scope: core.EgressClientScopeGlobal}}, nil
	case a.admin && scope == "":
		return []core.EgressClientFilter{
			{CreatedByID: a.userID, Scope: core.EgressClientScopePersonal},
			{Scope: core.EgressClientScopeGlobal},
		}, nil
	default:
		return []core.EgressClientFilter{{
			CreatedByID: a.userID,
			Scope:       core.EgressClientScopePersonal,
		}}, nil
	}
}

func listEgressClientsForActor(ctx context.Context, store core.EgressClientStore, actor egressClientActor, scope string) ([]*core.EgressClient, error) {
	filters, err := actor.listFilters(scope)
	if err != nil {
		return nil, err
	}

	clients := make([]*core.EgressClient, 0)
	for _, filter := range filters {
		items, err := store.ListEgressClients(ctx, filter)
		if err != nil {
			return nil, err
		}
		clients = append(clients, items...)
	}
	if len(filters) > 1 {
		sort.Slice(clients, func(i, j int) bool {
			return clients[i].CreatedAt.Before(clients[j].CreatedAt)
		})
	}
	return clients, nil
}
