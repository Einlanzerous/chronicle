package api

import (
	"crypto/subtle"
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
// That was true and the limiter did not deliver it until CHRN-75: keying
// trusted CHRONICLE_TRUSTED_PROXIES, whose deployed value 172.16.0.0/12
// CONTAINS construct_net, so every neighbour was a trusted proxy and could
// choose its own bucket. See clientIP.
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

// ProxySecretHeader is what Traefik stamps on every request it proxies here.
//
// It is NOT authentication and nothing outside clientIP may consult it. It
// answers one question -- did this request come through the estate's edge --
// and a secret that starts as a keying hint and ends up gating a route is how
// this kind of header becomes a credential nobody rotated.
const ProxySecretHeader = "X-Chronicle-Proxy-Secret"

// mismatchWarnEvery bounds how often a mismatched secret is reported. The
// lateral path lets a neighbour trip that warning deliberately, so it is rate
// limited -- but never silenced, because silence is the failure this exists to
// prevent.
const mismatchWarnEvery = time.Minute

// mismatchWarner emits at most one line per window.
type mismatchWarner struct {
	mu   sync.Mutex
	last time.Time
	now  func() time.Time
}

// proxySeen records whether any request has ever carried the right secret.
//
// It exists because an ABSENT header has two causes that look identical at the
// request level: a neighbour talking to the container directly (ordinary, and
// not worth a line), and a WAN request that came through Traefik with the
// middleware NOT ATTACHED. The second is a silent degraded state -- the secret
// is set, so boot does not warn; the header is absent, so the mismatch path
// does not warn; and every sign-in shares one bucket, which is exactly the
// owner-lockout this ticket exists to close.
//
// SERV-148's own text flags the likeliest way in: it attaches the middleware to
// three routers, and warns "all three, not only the /auth/ one". Miss
// chronicle-public-auth and it is the sign-in path that degrades.
//
// So: warn on an absent header until one request proves the middleware is
// attached, then go permanently quiet. A neighbour cannot produce a match, so
// it cannot silence the warning; and the warning stops the moment the deploy is
// actually correct.
type proxySeen struct {
	mu   sync.Mutex
	seen bool
}

func (p *proxySeen) mark() {
	p.mu.Lock()
	p.seen = true
	p.mu.Unlock()
}

func (p *proxySeen) yes() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seen
}

func (w *mismatchWarner) allow() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	if !w.last.IsZero() && now.Sub(w.last) < mismatchWarnEvery {
		return false
	}
	w.last = now
	return true
}

// clientIP is the address this request should be rate-limited against.
//
// The naive answer -- always RemoteAddr, never X-Forwarded-For, because the
// header is client-settable -- is right about the header and wrong about the
// outcome. Every request that arrives through Traefik carries Traefik's address
// as its peer, so keying on it collapses all legitimate browser and app traffic
// into ONE bucket: twenty sign-ins a minute across everybody, and an attacker
// hammering the direct host locks out every real user. A limiter that a
// stranger can use to deny the owner their own service is worse than none.
//
// So the header is trusted exactly when its source is. The question that has to
// be answered is "did this request come through the edge", and on this network
// THE PEER ADDRESS CANNOT ANSWER IT: construct_net is `external:` with default
// IPAM, Traefik holds no reserved address there (172.19.0.16 today, by
// allocation order), and nothing distinguishes it from the other seventeen
// containers. CHRN-75's first draft of this file believed a CIDR could, and the
// deployed value -- 172.16.0.0/12 -- contained the whole shared network, so
// every neighbour was trusted and could pick its own bucket by writing a header.
//
// A shared secret answers that question and nothing else. Traefik sets
// ProxySecretHeader with `customRequestHeaders`, which OVERWRITES whatever the
// caller sent -- the same mechanism strip-identity-headers already relies on --
// so through Traefik the value is honest no matter what the client sends, and
// around Traefik the client keeps its own forgery.
//
// Which is why this is a COMPARISON AND NEVER A PRESENCE TEST. A neighbour
// going direct arrives with the header present and wrong; treating "set" as
// "trusted" would reintroduce the whole defect in a new spelling.
//
// With no secret configured nothing is believed and the coarse behaviour above
// returns; setup logs a warning saying so.
func (a *api) clientIP(r *http.Request) string {
	peer := remoteIP(r)
	if a.proxySecret == "" {
		return peer.String()
	}

	presented := r.Header.Get(ProxySecretHeader)
	if presented == "" {
		// No header at all. Two causes, indistinguishable here: a neighbour
		// talking to the container directly (ordinary), or Traefik proxying
		// without the middleware attached (a silent shared bucket). See
		// proxySeen -- until one request proves the middleware is on, this is
		// reported; afterwards it is the lateral case and stays quiet.
		if a.proxySeen != nil && !a.proxySeen.yes() &&
			a.absent != nil && a.logger != nil && a.absent.allow() {
			a.logger.Warn(ProxySecretHeader+" is configured but no request has ever carried it; "+
				"X-Forwarded-For is being ignored and sign-ins share one bucket",
				"peer", peer.String(),
				"hint", "is the Traefik middleware attached to every Chronicle router, including chronicle-public-auth? (SERV-148)")
		}
		return peer.String()
	}
	if subtle.ConstantTimeCompare([]byte(presented), []byte(a.proxySecret)) != 1 {
		// Present and wrong. Either Traefik and Chronicle disagree about the
		// secret -- a typo, a half-applied rotation, or an image deployed ahead
		// of the compose change -- or a neighbour is forging it. Both put this
		// request on the coarse path, and the difference is visible in the peer:
		// Traefik's address means the deploy is broken, anything else means
		// somebody is probing.
		//
		// NEITHER VALUE IS LOGGED. requestLogger emits no headers by
		// construction and this must not become the exception.
		if a.mismatch != nil && a.logger != nil && a.mismatch.allow() {
			a.logger.Warn(ProxySecretHeader+" did not match; X-Forwarded-For is being ignored",
				"peer", peer.String(),
				"hint", "Traefik and Chronicle disagree about the secret, or a neighbour is forging it")
		}
		return peer.String()
	}

	// A match proves the middleware is attached, which is what silences the
	// absent-header warning above for the life of the process.
	if a.proxySeen != nil {
		a.proxySeen.mark()
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
