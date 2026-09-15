package server

import (
	"context"
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

type listingDecisionResult struct {
	decision invocation.ResourceAccessDecision
	err      error
}

// listingDecisionCache reuses answers and failures for one listing request.
// Per-entry projection decides whether an unavailable decision prevents access.
type listingDecisionCache struct {
	mu        sync.Mutex
	decisions map[listingDecisionKey]listingDecisionResult
	models    map[string]mountedUIModelSnapshot
}

type listingDecisionCacheContextKey struct{}

// withListingDecisionCache installs a per-request decision cache. Only listing
// surfaces install one; every other surface keeps making its own calls.
func withListingDecisionCache(ctx context.Context) (context.Context, *listingDecisionCache) {
	cache := &listingDecisionCache{
		decisions: make(map[listingDecisionKey]listingDecisionResult),
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

func (c *listingDecisionCache) decision(key listingDecisionKey) (listingDecisionResult, bool) {
	if c == nil {
		return listingDecisionResult{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	decision, ok := c.decisions[key]
	return decision, ok
}

func (c *listingDecisionCache) putDecision(key listingDecisionKey, decision invocation.ResourceAccessDecision, err error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.decisions[key] = listingDecisionResult{decision: decision, err: err}
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

// prefetchListingDecisions fills the request cache using bounded batches.
// Failed and unattempted questions retain the batch error so projection never
// retries them individually against the same unavailable dependency.
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

	for start := 0; start < len(unique); start += invocation.MaxBatchedAccessChecks {
		end := min(start+invocation.MaxBatchedAccessChecks, len(unique))
		decisions, err := invocation.CheckResourceAccessMany(ctx, s.authorization, unique[start:end])
		if err != nil {
			for _, key := range keys[start:] {
				cache.putDecision(key, invocation.ResourceAccessDecision{}, err)
			}
			return
		}
		for i, decision := range decisions {
			cache.putDecision(keys[start+i], decision, nil)
		}
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
