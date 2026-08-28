// Package config loads Chronicle's configuration from the environment. No
// config files — house style is env-only, CHRONICLE_-prefixed, with a
// DATABASE_URL fallback so the shared-Postgres convention keeps working.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Einlanzerous/chronicle/internal/invite"
)

// Default listen port. 4009 is the lowest free slot in the estate's 40xx block
// (4001-4008, 4010, 4012-4013 are taken).
const DefaultPort = 4009

// Config is the process-wide configuration.
type Config struct {
	DatabaseURL   string        // pgx DSN for Chronicle's own database
	Addr          string        // listen address, e.g. ":4009"
	LogLevel      slog.Level    // debug | info | warn | error
	LogFormat     string        // json | text
	ShutdownGrace time.Duration // how long in-flight work has to finish on SIGTERM

	// CHRN-71 — accounts. There is no CHRONICLE_AUTH: auth is unconditional,
	// so there is no flag to leave off by accident.
	OwnerEmail string // reconciled onto the owner row at boot
	OwnerName  string // display name; defaults to the email

	// Cloudflare Access SSO. Both or neither — exactly one is a boot error,
	// because a half-configured verifier fails every browser sign-in with a
	// message that says the token was invalid rather than that the server was.
	//
	// CFAccessAUD is a list: Access AUD tags are per-application, so when
	// CHRN-65 puts MCP behind its own Access application its tokens carry that
	// application's tag and not the web app's.
	CFAccessTeamDomain string
	CFAccessAUD        []string

	// MobileBaseURL is the origin baked into an invite's sign-in link, and the
	// only origin a phone is told about. Empty omits the link.
	MobileBaseURL string

	// SecureCookies sets Secure on the session cookie. Defaults to true and is
	// NOT derived from the request: TLS terminates at Traefik, so r.TLS is nil
	// for every request this service sees in the deployment it ships. It exists
	// only so a plain-HTTP LAN install can turn it off on purpose.
	SecureCookies bool

	// ProxySecret is the value Traefik stamps on X-Chronicle-Proxy-Secret, and
	// the whole of the trust decision for X-Forwarded-For (CHRN-75). Empty
	// means the header is never believed and every request through the proxy
	// shares one rate-limit bucket; boot warns when that is the case.
	//
	// It replaced CHRONICLE_TRUSTED_PROXIES, which could not express the thing
	// it needed to: on construct_net Traefik takes a DHCP address
	// indistinguishable from every other container's, so no prefix separates
	// "came through the edge" from "is a neighbour".
	ProxySecret string

	// RetiredTrustedProxies reports that CHRONICLE_TRUSTED_PROXIES was set.
	// Load does not error on it -- compose pins :latest and construct-server
	// still sets the variable, so refusing to boot would turn a retired knob
	// into a crash loop the moment the image lands ahead of the SERV change.
	// cmd/chronicle warns and ignores it; this becomes an error one release
	// later, once no deployed compose file still carries it.
	RetiredTrustedProxies bool

	// AudioDir is the root of the on-disk store of recordings (CHRN-23).
	// Absolute, and empty when unset -- the storage report then answers 503
	// naming this variable. CHRN-19 and CHRN-20 are what make it required:
	// nothing writes audio yet, and a service that refuses to boot over a
	// directory it has no use for would be a worse default than a warning.
	AudioDir string

	// InboxDir is the Copyparty-fed directory the watcher reads (CHRN-19),
	// with one subdirectory per account. Absolute, and empty means no watcher
	// runs at all — the Copyparty seam is one of two ingest paths and the
	// service is useful without it.
	InboxDir string

	// WatchInterval is how often the inbox is rescanned; WatchSettle is how
	// long a file must be untouched before it is read. Both have defaults in
	// internal/watch and are here so a slow sync client can be accommodated
	// without a rebuild.
	WatchInterval time.Duration
	WatchSettle   time.Duration

	// CHRN-27 — the estate ASR service Chronicle submits memos to. Empty
	// disables transcription entirely, and boot says so: an ingest path that
	// files memos nobody ever transcribes looks exactly like a working system
	// until somebody goes looking for a transcript.
	ASRBaseURL string

	// ASRToken is the bearer credential the ASR service issues per client.
	// Required whenever ASRBaseURL is set -- there is no anonymous mode on the
	// other end, and half-configuring it would fail every submission with a
	// 401 that reads like the token was wrong rather than absent.
	ASRToken string

	// ASRModel is the model to ask for. Empty takes the service's own default,
	// which is the right behaviour: the deployment knows what it has on disk
	// and Chronicle does not.
	ASRModel string

	// TranscribeInterval is how often the pump sweeps. Default in
	// internal/transcribe.
	TranscribeInterval time.Duration
}

// TranscriptionEnabled reports whether Chronicle will submit memos for
// transcription.
func (c Config) TranscriptionEnabled() bool { return c.ASRBaseURL != "" }

// SSOEnabled reports whether Cloudflare Access sign-in is configured.
func (c Config) SSOEnabled() bool {
	return c.CFAccessTeamDomain != "" && len(c.CFAccessAUD) > 0
}

// ValidateForServe checks what only a running server needs. It is separate
// from Load because `migrate` and `mint-invite` genuinely do not need an owner
// identity — migrate applies SQL, and the owner row is seeded with a
// placeholder by migration 0002 — so requiring it there would be a papercut on
// every schema operation.
//
// Serving does need it. Left unset the owner keeps that placeholder, which can
// never match a Cloudflare-verified email, so browser sign-in would look
// configured and silently never work. A named boot error beats that.
func (c Config) ValidateForServe() error {
	if c.OwnerEmail == "" {
		return fmt.Errorf("config: CHRONICLE_OWNER_EMAIL is required to serve (the account the first invite is minted for)")
	}
	return nil
}

// Load reads configuration from the environment, naming the offending variable
// rather than failing later at connect time.
func Load() (Config, error) {
	var c Config
	var err error

	c.DatabaseURL = firstNonEmpty(os.Getenv("CHRONICLE_DATABASE_URL"), os.Getenv("DATABASE_URL"))
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("config: CHRONICLE_DATABASE_URL (or DATABASE_URL) is required")
	}

	port := DefaultPort
	if v := strings.TrimSpace(os.Getenv("CHRONICLE_PORT")); v != "" {
		port, err = strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return c, fmt.Errorf("config: CHRONICLE_PORT %q is not a valid port", v)
		}
	}
	c.Addr = fmt.Sprintf(":%d", port)

	c.LogLevel, err = parseLevel(firstNonEmpty(os.Getenv("CHRONICLE_LOG_LEVEL"), "info"))
	if err != nil {
		return c, err
	}

	// JSON by default: Datadog parses it into attributes with no pipeline
	// config, and Dozzle renders it fine. text is for a human at a terminal.
	c.LogFormat = strings.ToLower(firstNonEmpty(os.Getenv("CHRONICLE_LOG_FORMAT"), "json"))
	if c.LogFormat != "json" && c.LogFormat != "text" {
		return c, fmt.Errorf("config: CHRONICLE_LOG_FORMAT %q is not json or text", c.LogFormat)
	}

	c.ShutdownGrace = 20 * time.Second
	if v := strings.TrimSpace(os.Getenv("CHRONICLE_SHUTDOWN_GRACE")); v != "" {
		c.ShutdownGrace, err = time.ParseDuration(v)
		if err != nil || c.ShutdownGrace <= 0 {
			return c, fmt.Errorf("config: CHRONICLE_SHUTDOWN_GRACE %q is not a positive duration", v)
		}
	}

	// Absolute or nothing. A relative path resolves against the daemon's
	// working directory, which nobody deploying this thinks about, and a
	// corpus that lands somewhere different depending on how the process was
	// started is a corpus that gets half-pruned.
	c.AudioDir = strings.TrimSpace(os.Getenv("CHRONICLE_AUDIO_DIR"))
	if c.AudioDir != "" && !filepath.IsAbs(c.AudioDir) {
		return c, fmt.Errorf("config: CHRONICLE_AUDIO_DIR %q must be an absolute path", c.AudioDir)
	}

	c.InboxDir = strings.TrimSpace(os.Getenv("CHRONICLE_INBOX_DIR"))
	if c.InboxDir != "" && !filepath.IsAbs(c.InboxDir) {
		return c, fmt.Errorf("config: CHRONICLE_INBOX_DIR %q must be an absolute path", c.InboxDir)
	}

	c.WatchInterval, err = optionalDuration("CHRONICLE_WATCH_INTERVAL")
	if err != nil {
		return c, err
	}
	c.WatchSettle, err = optionalDuration("CHRONICLE_WATCH_SETTLE")
	if err != nil {
		return c, err
	}

	// Both or neither, and the reason is the same one the Cloudflare pair
	// gives: a half-configured client fails every submission with a message
	// that says the credential was rejected rather than that it was never set.
	c.ASRBaseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("CHRONICLE_ASR_URL")), "/")
	c.ASRToken = strings.TrimSpace(os.Getenv("CHRONICLE_ASR_TOKEN"))
	if (c.ASRBaseURL == "") != (c.ASRToken == "") {
		return c, fmt.Errorf("config: CHRONICLE_ASR_URL and CHRONICLE_ASR_TOKEN must be set together (got one of the two)")
	}
	c.ASRModel = strings.TrimSpace(os.Getenv("CHRONICLE_ASR_MODEL"))

	c.TranscribeInterval, err = optionalDuration("CHRONICLE_TRANSCRIBE_INTERVAL")
	if err != nil {
		return c, err
	}

	c.OwnerEmail = strings.ToLower(strings.TrimSpace(os.Getenv("CHRONICLE_OWNER_EMAIL")))
	c.OwnerName = strings.TrimSpace(os.Getenv("CHRONICLE_OWNER_NAME"))

	// Stored as given, minus surrounding space. api.NewCFAccessVerifier reduces
	// it to a bare host — a value pasted from the Zero Trust dashboard with its
	// scheme attached would otherwise yield an issuer of "https://https://…"
	// that mismatches every token. Normalizing in one place keeps the two from
	// drifting; this package only needs to know whether it was set.
	c.CFAccessTeamDomain = strings.TrimSpace(os.Getenv("CHRONICLE_CF_ACCESS_TEAM_DOMAIN"))
	c.CFAccessAUD = splitList(os.Getenv("CHRONICLE_CF_ACCESS_AUD"))
	if (c.CFAccessTeamDomain == "") != (len(c.CFAccessAUD) == 0) {
		return c, fmt.Errorf("config: CHRONICLE_CF_ACCESS_TEAM_DOMAIN and CHRONICLE_CF_ACCESS_AUD must be set together (got one of the two)")
	}

	c.SecureCookies = true
	if v := strings.TrimSpace(os.Getenv("CHRONICLE_COOKIE_SECURE")); v != "" {
		c.SecureCookies, err = strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("config: CHRONICLE_COOKIE_SECURE %q is not a boolean", v)
		}
	}

	// Not parsed, not validated, not used -- only noticed, so the warning can
	// name it. See RetiredTrustedProxies.
	c.RetiredTrustedProxies = strings.TrimSpace(os.Getenv("CHRONICLE_TRUSTED_PROXIES")) != ""

	c.ProxySecret = strings.TrimSpace(os.Getenv("CHRONICLE_PROXY_SECRET"))

	// A malformed base is refused rather than shrugged at: clients prefer the
	// server's URL over one they would have built, so an unusable value both
	// produces a QR that scans to nothing and suppresses the fallback that
	// would have worked. Lyceum learned this as LYCM-102.
	c.MobileBaseURL, err = invite.NormalizeBase(os.Getenv("CHRONICLE_MOBILE_BASE_URL"))
	if err != nil {
		return c, fmt.Errorf("config: CHRONICLE_MOBILE_BASE_URL %w", err)
	}
	return c, nil
}

// Logger builds the process logger this config describes.
func (c Config) Logger(w *os.File) *slog.Logger {
	opts := &slog.HandlerOptions{Level: c.LogLevel}
	if c.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("config: CHRONICLE_LOG_LEVEL %q is not debug/info/warn/error", s)
	}
}

// splitList parses a comma-separated environment value, dropping blanks so a
// trailing comma is not a silent empty entry.
func splitList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// optionalDuration reads a positive duration, or zero when unset. Zero means
// "use the package default" rather than "no delay", which is why a negative or
// unparseable value is an error instead of being clamped: a settle window
// silently set to nothing is how a half-written upload becomes a second memo.
func optionalDuration(name string) (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("config: %s %q is not a positive duration", name, v)
	}
	return d, nil
}
