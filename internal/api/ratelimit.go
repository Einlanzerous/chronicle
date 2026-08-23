package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Sign-in is rate-limited per client IP (CHRN-71).
//
// Lyceum deliberately does not limit its token sign-in path, and that reasoning
// is sound on its face: a 256-bit secret needs no help against brute force.
// Chronicle limits it anyway, for a reason that is lateral rather than
// external. The container is reachable on the shared Docker network by every
// other service on the box without passing Traefik at all, so the edge limiter
// CHRN-16 attaches to PathPrefix(/auth/) does nothing for that path. This is
// the backstop for a caller already inside the network.
//
// The burst is generous on purpose: this exists to bound an automated attempt,
// not to inconvenience someone mistyping a device label.
const (
	signInRateWindow = time.Minute
	signInRateBurst  = 20
)

type rateWindow struct {
	count   int
	resetAt time.Time
}

// ipRateLimiter is a fixed-window per-key limiter: a lock, a map and lazy
// pruning. Deliberately minimal — at Chronicle's scale the map holds a handful
// of entries, and a fixed window is plenty for a brute-force backstop.
type ipRateLimiter struct {
	mu     sync.Mutex
	window time.Duration
	burst  int
	hits   map[string]*rateWindow
	now    func() time.Time // injectable so tests do not sleep
}

func newIPRateLimiter(window time.Duration, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		window: window,
		burst:  burst,
		hits:   make(map[string]*rateWindow),
		now:    time.Now,
	}
}

// allow records one attempt for key and reports whether it is within the limit.
func (l *ipRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	w := l.hits[key]
	if w == nil || now.After(w.resetAt) {
		// Prune expired windows opportunistically so a spray of distinct source
		// IPs cannot grow the map without bound.
		if len(l.hits) > 1024 {
			for k, v := range l.hits {
				if now.After(v.resetAt) {
					delete(l.hits, k)
				}
			}
		}
		l.hits[key] = &rateWindow{count: 1, resetAt: now.Add(l.window)}
		return true
	}
	if w.count >= l.burst {
		return false
	}
	w.count++
	return true
}

// clientIP is the address a request actually came from, for rate-limit keying.
// It reads RemoteAddr and never X-Forwarded-For: that header is client-settable
// and would let a caller rotate the key at will. Behind Traefik every request
// shares the proxy's address, which degrades this to a global limit on the
// sign-in path — a fail-closed coarsening, not a gap, and the case this limiter
// is for is the one that does not come through Traefik at all.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
