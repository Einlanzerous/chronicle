package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Sign-in is rate-limited per client IP (CHRN-71).
//
// Lyceum deliberately does not limit its token sign-in path, and that reasoning
// is sound on its face: a 256-bit secret needs no help against brute force.
// Chronicle limits it anyway, for a reason that is lateral rather than
// external. The container is reachable on the shared Docker network by every
// other service on the box without passing Traefik, so the edge limiter
// CHRN-16 attaches to PathPrefix(/auth/) does nothing for that path.
//
// The burst is generous on purpose: this exists to bound an automated attempt,
// not to inconvenience someone mistyping a device label.
const (
	signInRateWindow = time.Minute
	signInRateBurst  = 20
)

// maxRateKeys bounds the limiter's map. Reached, it evicts the window closest
// to expiry rather than growing — the alternative is that a spray of source
// addresses is itself the memory exhaustion the limiter is supposed to prevent.
const maxRateKeys = 4096

type rateWindow struct {
	count   int
	resetAt time.Time
}

// ipRateLimiter is a fixed-window per-key limiter: a lock, a map and bounded
// eviction. Deliberately minimal — at Chronicle's scale the map holds a handful
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
	if w := l.hits[key]; w != nil && !now.After(w.resetAt) {
		if w.count >= l.burst {
			return false
		}
		w.count++
		return true
	}

	if len(l.hits) >= maxRateKeys {
		l.evict(now)
	}
	l.hits[key] = &rateWindow{count: 1, resetAt: now.Add(l.window)}
	return true
}

// evict makes room. It drops every expired window first, and only if that
// frees nothing does it drop the single entry closest to expiry.
//
// Dropping expired entries alone is not a bound: they are all live during
// exactly the spray this guards against, so the earlier version scanned the
// whole map on every new key and deleted nothing. Evicting a live window lets
// one attacker's key survive slightly longer than it should, which is the
// cheaper of the two failures by a wide margin.
func (l *ipRateLimiter) evict(now time.Time) {
	freed := false
	for k, w := range l.hits {
		if now.After(w.resetAt) {
			delete(l.hits, k)
			freed = true
		}
	}
	if freed {
		return
	}
	var oldestKey string
	var oldest time.Time
	for k, w := range l.hits {
		if oldestKey == "" || w.resetAt.Before(oldest) {
			oldestKey, oldest = k, w.resetAt
		}
	}
	delete(l.hits, oldestKey)
}

// limitSignIn wraps a credential-minting handler in the per-client limiter.
func (a *api) limitSignIn(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.signInLimiter.allow(a.clientIP(r)) {
			http.Error(w, "too many attempts, slow down", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// clientIP is the address this request should be rate-limited against.
//
// The naive answer — always RemoteAddr, never X-Forwarded-For, because the
// header is client-settable — is right about the header and wrong about the
// outcome. Every request that arrives through Traefik carries Traefik's address
// as its peer, so keying on it collapses all legitimate browser and app traffic
// into ONE bucket: twenty sign-ins a minute across everybody, and an attacker
// hammering the direct host locks out every real user. A limiter that a
// stranger can use to deny the owner their own service is worse than none.
//
// So the header is trusted exactly when its source is: only when the immediate
// peer is a configured trusted proxy. That is sound here because Traefik's
// entrypoints set `forwardedHeaders.trustedIPs: []`, which makes Traefik
// overwrite whatever the caller sent with the address it actually saw. A
// neighbour container connecting directly is not a trusted peer, so its own
// X-Forwarded-For is ignored and it is keyed on its real address — which is the
// lateral case this limiter exists for in the first place.
//
// With no trusted proxies configured, nothing is believed and the coarse
// behaviour is the one above; setup logs a warning saying so.
func (a *api) clientIP(r *http.Request) string {
	peer := remoteIP(r)
	if !a.isTrustedProxy(peer) {
		return peer.String()
	}
	// Rightmost entry: with trustedIPs empty Traefik writes a single address,
	// and where a chain exists the last hop is the only one our trusted peer
	// actually observed. Reading the leftmost would take the caller's word.
	fwd := r.Header.Get("X-Forwarded-For")
	if fwd == "" {
		return peer.String()
	}
	parts := strings.Split(fwd, ",")
	last := strings.TrimSpace(parts[len(parts)-1])
	if addr, err := netip.ParseAddr(last); err == nil {
		return addr.String()
	}
	return peer.String()
}

func (a *api) isTrustedProxy(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	for _, p := range a.trustedProxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// remoteIP is the transport peer, or the invalid zero Addr when RemoteAddr is
// not parseable (an httptest request with no address, for instance).
func remoteIP(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}
