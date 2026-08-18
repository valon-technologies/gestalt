package server

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func (s *Server) integrationHiddenFromCatalog(provider string) bool {
	entry, ok := s.pluginDefs[strings.TrimSpace(provider)]
	return ok && entry != nil && entry.Static != nil && entry.Static.CatalogHidden
}

func (s *Server) integrationSettingsAccessibleContext(ctx context.Context, p *principal.Principal, provider string) (bool, error) {
	if s == nil || s.authorization == nil {
		return true, nil
	}
	if p == nil || principal.IsNonUserPrincipal(p) {
		return false, nil
	}
	subjectID, err := principal.ResolveAuthorizationSubjectID(ctx, s.credentialUserResolver(), p)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(subjectID) == "" {
		return false, nil
	}
	decision, err := s.checkResourceAccess(ctx, invocation.ResourceAccessRequest{
		SubjectID: subjectID,
		Action:    provider,
		Resource:  s.authorizationResource(provider),
	})
	if err != nil {
		return false, err
	}
	return decision.Allowed, nil
}

// prefetchIntegrationListingDecisions answers every authorization question the
// Apps list is about to ask — may they use this app, open its web UI, or
// admin it — with a single batched evaluator call. The per-app handlers
// below still ask checkResourceAccess; they simply find the answer already
// cached.
func (s *Server) prefetchIntegrationListingDecisions(ctx context.Context, p *principal.Principal, appNames []string) {
	if s == nil || s.authorization == nil || p == nil || principal.IsNonUserPrincipal(p) {
		return
	}
	subjectID, err := principal.ResolveAuthorizationSubjectID(ctx, s.credentialUserResolver(), p)
	if err != nil {
		return
	}
	if subjectID = strings.TrimSpace(subjectID); subjectID == "" {
		return
	}

	reqs := make([]invocation.ResourceAccessRequest, 0, 3*len(appNames))
	for _, name := range appNames {
		reqs = append(reqs, invocation.ResourceAccessRequest{
			SubjectID: subjectID,
			Action:    name,
			Resource:  s.authorizationResource(name),
		})
		if req, ok := s.mountedUIListingAccessRequest(name, subjectID); ok {
			reqs = append(reqs, req)
		}
		if _, ok := s.registryApp(name); ok {
			reqs = append(reqs, invocation.ResourceAccessRequest{
				SubjectID:    subjectID,
				Action:       name,
				Resource:     s.authorizationResource(name),
				AllowedRoles: []string{appAdminRole},
			})
		}
	}
	s.prefetchListingDecisions(ctx, reqs)
}

// mountedUIListingAccessRequest is the exact question
// mountedUIRootAccessibleContext will ask for this app's mounted UI, or ok
// false when the mount asks none. It builds the request through the same
// mountedResourceAccess type the mounted-UI boundary uses so the prefetched
// question cannot drift from the question actually asked.
func (s *Server) mountedUIListingAccessRequest(appName, subjectID string) (invocation.ResourceAccessRequest, bool) {
	mountedPath := s.configuredMountedPath(appName)
	if mountedPath == "" {
		return invocation.ResourceAccessRequest{}, false
	}
	mounted, ok := s.mountedUIForProvider(appName, mountedPath)
	if !ok || !mountedUIRequiresAuthorization(mounted) {
		return invocation.ResourceAccessRequest{}, false
	}
	resourceName := mountedUIAuthorizationResourceName(mounted)
	if resourceName == "" {
		return invocation.ResourceAccessRequest{}, false
	}
	access := mountedResourceAccess{
		appKey:       strings.TrimSpace(mounted.AppName),
		resourceName: resourceName,
		subjectID:    subjectID,
		allowedRoles: mounted.AllowedRoles,
	}
	return invocation.ResourceAccessRequest{
		SubjectID:    access.subjectID,
		Action:       access.action(),
		Resource:     s.authorizationResource(access.resourceName),
		AllowedRoles: access.allowedRoles,
	}, true
}

// configuredMountedPath returns the app's declared static mount path.
func (s *Server) configuredMountedPath(appName string) string {
	entry, ok := s.pluginDefs[appName]
	if !ok || entry == nil || entry.Static == nil {
		return ""
	}
	return strings.TrimSpace(entry.Static.Mount)
}

func (s *Server) integrationMountedPathForPrincipalContext(ctx context.Context, p *principal.Principal, provider, mountedPath string) string {
	mountedPath = strings.TrimSpace(mountedPath)
	if mountedPath == "" {
		return ""
	}
	mounted, ok := s.mountedUIForProvider(provider, mountedPath)
	if !ok || !s.mountedUIRootAccessibleContext(ctx, p, mounted) {
		return ""
	}
	return mountedPath
}

func (s *Server) mountedUIForProvider(provider, mountedPath string) (MountedUI, bool) {
	for i := range s.mountedUIs {
		mounted := &s.mountedUIs[i]
		if mounted.Handler == nil || mounted.Path != mountedPath {
			continue
		}
		if mounted.AppName == provider {
			return *mounted, true
		}
	}
	for i := range s.mountedUIs {
		mounted := &s.mountedUIs[i]
		if mounted.Handler == nil || mounted.Path != mountedPath {
			continue
		}
		return *mounted, true
	}
	return MountedUI{}, false
}

func (s *Server) mountedUIRootAccessibleContext(ctx context.Context, p *principal.Principal, mounted MountedUI) bool {
	if !mountedUIRequiresAuthorization(mounted) {
		return true
	}
	if p == nil || principal.IsNonUserPrincipal(p) {
		return false
	}

	_, allowed, err := s.authorizeMountedAppAccess(ctx, p, mounted)
	return err == nil && allowed
}
