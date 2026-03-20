package broker

import (
	"sync"

	"golang.org/x/time/rate"
)

type rateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	rps      rate.Limit
	burst    int
}

func newRateLimiter(rps, burst int) *rateLimiter {
	return &rateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
}

func (r *rateLimiter) Allow(provider string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	lim, ok := r.limiters[provider]
	if !ok {
		lim = rate.NewLimiter(r.rps, r.burst)
		r.limiters[provider] = lim
	}
	return lim.Allow()
}
