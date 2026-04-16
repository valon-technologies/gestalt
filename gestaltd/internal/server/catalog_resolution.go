package server

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/invocation"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type sessionCatalogTarget struct {
	connection string
	instance   string
}

func (s *Server) sessionCatalogTargets(providerName string, p *principal.Principal, requestedConnection, requestedInstance string) []sessionCatalogTarget {
	connections := s.sessionCatalogConnections(providerName, p, requestedConnection)
	targets := make([]sessionCatalogTarget, 0, len(connections))
	for _, connection := range connections {
		connection, instance := s.workloadBindingSelectors(p, providerName, connection, requestedInstance)
		targets = append(targets, sessionCatalogTarget{
			connection: connection,
			instance:   instance,
		})
	}
	return targets
}

func (s *Server) resolveOperationsCatalog(
	ctx context.Context,
	prov core.Provider,
	providerName string,
	resolver invocation.TokenResolver,
	p *principal.Principal,
	requestedConnection, requestedInstance string,
) (*catalog.Catalog, bool, error) {
	resolveCatalog := invocation.ResolveCatalogWithMetadata
	strictCatalog := false
	if requestedConnection != "" || requestedInstance != "" {
		resolveCatalog = invocation.ResolveCatalogStrictWithMetadata
		strictCatalog = true
	} else if core.SupportsSessionCatalog(prov) {
		resolveCatalog = invocation.ResolveCatalogStrictWithMetadata
		strictCatalog = true
	}

	targets := s.sessionCatalogTargets(providerName, p, requestedConnection, requestedInstance)
	if strictCatalog && requestedConnection == "" && requestedInstance == "" &&
		(s.authorizer == nil || !s.authorizer.IsWorkload(p)) {
		var firstErr error
		for _, target := range targets {
			cat, metadata, err := resolveCatalog(ctx, prov, providerName, resolver, p, target.connection, target.instance)
			if err == nil {
				return cat, metadata.SessionFailed, nil
			}
			if firstErr == nil {
				firstErr = err
			}
		}
		if firstErr != nil {
			return nil, true, firstErr
		}
	}

	target := targets[0]
	cat, metadata, err := resolveCatalog(ctx, prov, providerName, resolver, p, target.connection, target.instance)
	return cat, metadata.SessionFailed || err != nil, err
}

func (s *Server) resolveOperationMetadata(
	ctx context.Context,
	prov core.Provider,
	providerName string,
	resolver invocation.TokenResolver,
	p *principal.Principal,
	connection, instance, operationName string,
) (catalog.CatalogOperation, string, error) {
	opMeta, ok := invocation.CatalogOperation(prov.Catalog(), operationName)
	if !core.SupportsSessionCatalog(prov) {
		if !ok {
			return catalog.CatalogOperation{}, connection, fmt.Errorf("%w: %q on provider %q", invocation.ErrOperationNotFound, operationName, providerName)
		}
		return opMeta, connection, nil
	}

	var firstSessionErr error
	sessionCatalogResolved := false
	for _, target := range s.sessionCatalogTargets(providerName, p, connection, instance) {
		sessionCat, err := invocation.ResolveSessionCatalog(ctx, prov, providerName, resolver, p, target.connection, target.instance)
		if err != nil {
			if firstSessionErr == nil {
				firstSessionErr = err
			}
			if connection != "" {
				return catalog.CatalogOperation{}, connection, err
			}
			continue
		}
		sessionCatalogResolved = true
		if sessionOp, sessionOK := invocation.CatalogOperation(sessionCat, operationName); sessionOK {
			if connection == "" {
				connection = target.connection
			}
			return sessionOp, connection, nil
		}
	}

	if firstSessionErr != nil && !sessionCatalogResolved {
		return catalog.CatalogOperation{}, connection, firstSessionErr
	}
	if instance != "" {
		return catalog.CatalogOperation{}, connection, fmt.Errorf("%w: %q on provider %q", invocation.ErrOperationNotFound, operationName, providerName)
	}
	if !ok {
		return catalog.CatalogOperation{}, connection, fmt.Errorf("%w: %q on provider %q", invocation.ErrOperationNotFound, operationName, providerName)
	}
	return opMeta, connection, nil
}
