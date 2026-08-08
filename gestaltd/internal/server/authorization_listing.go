package server

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/services/invocation"
)

// listingDecisionKey identifies exactly one authorization question. Two
// questions share a key only when they would produce the same evaluator
// request, so a cache hit can never substitute one question's answer for
// another's.
type listingDecisionKey struct {
	subjectID    string
	action       string
	resourceType string
	resourceID   string
	allowedRoles string
}

func newListingDecisionKey(req invocation.ResourceAccessRequest) listingDecisionKey {
	return listingDecisionKey{
		subjectID:    strings.TrimSpace(req.SubjectID),
		action:       strings.TrimSpace(req.Action),
		resourceType: strings.TrimSpace(req.Resource.GetType()),
		resourceID:   strings.TrimSpace(req.Resource.GetId()),
		allowedRoles: strings.Join(req.AllowedRoles, "\x00"),
	}
}

// listingDecisionCache memoizes evaluator answers and active-model reads for
// the lifetime of one listing request.
//
// It is a cache, never a decision maker. Every entry is produced by the same
// helpers the single-decision path uses, and only successful answers are
// stored, so reading from the cache cannot change an answer - it can only
// remove a repeat of the same provider call. That is what makes it safe to
// batch a listing: the per-app code path is untouched and still asks
// checkResourceAccess, which now finds the answer already present.
type listingDecisionCache struct {
	mu        sync.Mutex
	decisions map[listingDecisionKey]invocation.ResourceAccessDecision
	models    map[string]mountedUIModelSnapshot
}

type listingDecisionCacheContextKey struct{}

// withListingDecisionCache installs a per-request decision cache. Only listing
// surfaces install one; every other surface keeps making its own calls.
func withListingDecisionCache(ctx context.Context) (context.Context, *listingDecisionCache) {
	cache := &listingDecisionCache{
		decisions: make(map[listingDecisionKey]invocation.ResourceAccessDecision),
		models:    make(map[string]mountedUIModelSnapshot),
	}
	return context.WithValue(ctx, listingDecisionCacheContextKey{}, cache), cache
}

func listingDecisionCacheFromContext(ctx context.Context) *listingDecisionCache {
	if ctx == nil {
		return nil
	}
	cache, _ := ctx.Value(listingDecisionCacheContextKey{}).(*listingDecisionCache)
	return cache
}

func (c *listingDecisionCache) decision(key listingDecisionKey) (invocation.ResourceAccessDecision, bool) {
	if c == nil {
		return invocation.ResourceAccessDecision{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	decision, ok := c.decisions[key]
	return decision, ok
}

func (c *listingDecisionCache) putDecision(key listingDecisionKey, decision invocation.ResourceAccessDecision) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.decisions[key] = decision
}

func (c *listingDecisionCache) model(typeName string) (mountedUIModelSnapshot, bool) {
	if c == nil {
		return mountedUIModelSnapshot{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot, ok := c.models[typeName]
	return snapshot, ok
}

func (c *listingDecisionCache) putModel(typeName string, snapshot mountedUIModelSnapshot) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.models[typeName] = snapshot
}

// prefetchListingDecisions answers every question a listing is about to ask
// with ONE batched provider call and stores the answers in the request's
// decision cache.
//
// A batch the provider cannot serve is deliberately not an error here. The
// cache simply stays empty and each entry falls back to the single-decision
// path that already shipped, so listing degrades to more provider calls and
// never to fewer visible apps. Whatever the evaluator does answer is enforced
// identically either way, and invoke-time enforcement is unchanged.
func (s *Server) prefetchListingDecisions(ctx context.Context, reqs []invocation.ResourceAccessRequest) {
	cache := listingDecisionCacheFromContext(ctx)
	if s == nil || s.authorization == nil || cache == nil || len(reqs) == 0 {
		return
	}

	keys := make([]listingDecisionKey, 0, len(reqs))
	unique := make([]invocation.ResourceAccessRequest, 0, len(reqs))
	seen := make(map[listingDecisionKey]struct{}, len(reqs))
	for _, req := range reqs {
		key := newListingDecisionKey(req)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
		unique = append(unique, req)
	}

	decisions, err := invocation.CheckResourceAccessMany(ctx, s.authorization, unique)
	if err != nil {
		slog.WarnContext(ctx, "auth: batched listing decision unavailable; falling back to per-item decisions",
			"error", err, "requests", len(unique))
		return
	}
	for i, decision := range decisions {
		cache.putDecision(keys[i], decision)
	}
}

// operationAccessChecker exposes the broker's batched operation-access
// decisions to listing surfaces. Only the broker owns invocation authorization,
// so anything else means listing cannot be filtered and must stay unfiltered
// rather than guess.
func operationAccessChecker(invoker invocation.Invoker) invocation.OperationAccessChecker {
	broker, ok := invoker.(*invocation.Broker)
	if !ok {
		return nil
	}
	return broker
}
