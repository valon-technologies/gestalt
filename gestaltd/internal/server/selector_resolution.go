package server

import (
	"fmt"
	"net/http"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type resolvedSelectors struct {
	Connection string
	Instance   string
}

type sessionCatalogPlan struct {
	ExplicitConnection bool
	ExplicitInstance   bool
	Targets            []resolvedSelectors
}

func (s *Server) resolveRequestedSelectors(w http.ResponseWriter, provider, requestedConnection, requestedInstance string) (resolvedSelectors, bool) {
	if requestedConnection != "" {
		var ok bool
		requestedConnection, ok = s.resolveRequestedConnection(w, provider, requestedConnection)
		if !ok {
			return resolvedSelectors{}, false
		}
	}
	if requestedInstance != "" {
		var ok bool
		requestedInstance, ok = resolveRequestedInstance(w, requestedInstance)
		if !ok {
			return resolvedSelectors{}, false
		}
	}
	return resolvedSelectors{
		Connection: requestedConnection,
		Instance:   requestedInstance,
	}, true
}

func mergeResolvedSelectors(w http.ResponseWriter, querySelectors, bodySelectors resolvedSelectors) (resolvedSelectors, bool) {
	if querySelectors.Instance != "" && bodySelectors.Instance != "" && querySelectors.Instance != bodySelectors.Instance {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("conflicting instance parameter %q in query string and JSON body", httpInstanceParam))
		return resolvedSelectors{}, false
	}
	if querySelectors.Connection != "" && bodySelectors.Connection != "" && querySelectors.Connection != bodySelectors.Connection {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("conflicting connection parameter %q in query string and JSON body", httpConnectionParam))
		return resolvedSelectors{}, false
	}

	selectors := bodySelectors
	if selectors.Instance == "" {
		selectors.Instance = querySelectors.Instance
	}
	if selectors.Connection == "" {
		selectors.Connection = querySelectors.Connection
	}
	return selectors, true
}

func (s *Server) planSessionCatalogResolution(p *principal.Principal, provider string, selectors resolvedSelectors) sessionCatalogPlan {
	plan := sessionCatalogPlan{
		ExplicitConnection: selectors.Connection != "",
		ExplicitInstance:   selectors.Instance != "",
	}
	candidates := s.sessionCatalogConnections(provider, p, selectors.Connection)
	plan.Targets = make([]resolvedSelectors, 0, len(candidates))
	for _, candidateConnection := range candidates {
		connection, instance := s.workloadBindingSelectors(p, provider, candidateConnection, selectors.Instance)
		plan.Targets = append(plan.Targets, resolvedSelectors{
			Connection: connection,
			Instance:   instance,
		})
	}
	if len(plan.Targets) == 0 {
		plan.Targets = []resolvedSelectors{{Connection: selectors.Connection, Instance: selectors.Instance}}
	}
	return plan
}

func (plan sessionCatalogPlan) requiresStrictCatalog(prov core.Provider) bool {
	return plan.ExplicitConnection || plan.ExplicitInstance || core.SupportsSessionCatalog(prov)
}

func (plan sessionCatalogPlan) shouldTryAllTargets(p *principal.Principal) bool {
	return !plan.ExplicitConnection && !plan.ExplicitInstance && (p == nil || p.Kind != principal.KindWorkload)
}
