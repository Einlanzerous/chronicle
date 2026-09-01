// Package config loads Chronicle's configuration from the environment. No
// config files — house style is env-only, CHRONICLE_-prefixed, with a
// DATABASE_URL fallback so the shared-Postgres convention keeps working.
package config

import (
	"fmt"
	"log/slog"
	"net/url"
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

// DefaultASRMaxAttempts mirrors transcribe.DefaultMaxAttempts. Written as its
// own constant rather than imported: internal/config is the leaf every package
// reads, and pointing it at the pump would invert that.
const DefaultASRMaxAttempts = 5

// DefaultScribeMaxAttempts is CHRN-32 §7's retry ceiling: how many times one
// memo's proposal is asked for before the failure is recorded instead. Three,
// because attempts two and three carry the validation error back to the model,
// and a model told "confidence must be between 0 and 1, got 1.5" usually fixes
// it — one completion is cheaper than an operator's attention.
const DefaultScribeMaxAttempts = 3

// DefaultScribePreacceptMin is deliberately 1.01 — a threshold NO confidence
// can clear, so nothing is pre-accepted until somebody sets this on purpose.
//
// CHRN-32 §8 gives the reasoning: the contract carries confidence and does not
// interpret it, and the value that makes ACCEPT ALL safe is CHRN-36's to
// measure. Defaulting to something plausible-looking like 0.8 would licence
// batch acceptance on a number nobody has checked, which is the exact trade
// the epic warns about — "a router at 70% that claims 0.9 confidence is worse
// than no router, because it spends trust it has not earned".
const DefaultScribePreacceptMin = 1.01

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

	// ASRMaxAttempts is CHRN-28's ceiling: attempts per memo before it is held
	// for a human rather than tried again.
	ASRMaxAttempts int

	// TranscribeInterval is how often the pump sweeps. Default in
	// internal/transcribe.
	TranscribeInterval time.Duration

	// Tier1DatabaseURL is the pool Scribe and every other DERIVED writer
	// connects on, as chronicle_tier1 (CHRN-32 §1.1, ruling R4).
	//
	// Migration 0007 grants that role SELECT on tier2.memos and
	// tier2.transcripts and nothing else, so a process holding this DSN can
	// read the corpus it derives from and cannot write a word of it. That is
	// the enforcement mechanism CLAUDE.md names, and it only enforces anything
	// if something actually connects on it — which is why this exists in E4
	// rather than waiting for CHRN-52.
	//
	// FALLS BACK to DatabaseURL when unset, so a single-DSN deployment keeps
	// working and the tier-1 pool is an addition rather than a prerequisite.
	// Tier1IsSeparate reports whether the fallback was taken; boot warns when
	// it was, because a tier-1 pool silently running as `chronicle` is
	// enforcement that enforces nothing. CHRN-52 decides whether the fallback
	// may survive in production at all.
	Tier1DatabaseURL string

	// CHRN-32 — Scribe. Empty ScribeOllamaURL disables routing entirely.
	ScribeOllamaURL string

	// ScribeModel is the Ollama model, e.g. `gemma4:31b`. It is qualified to
	// `ollama/<model>@<promptversion>` for the proposer string, on
	// tier2.transcripts.model's pattern — that column holds
	// `whisper.cpp/small.en` rather than `small.en`, because a bare name says
	// nothing about what ran it.
	ScribeModel string

	// ScribePreacceptMin is the confidence at or above which a proposal MAY be
	// pre-selected for ACCEPT ALL. Owned by CHRN-36, which is the only thing
	// that will ever know the right value; see DefaultScribePreacceptMin for
	// why the default admits nothing.
	//
	// It is a floor and not the only gate: CHRN-32 §4 excludes DISCARD from
	// pre-acceptance by contract, at any confidence, because `discarded` is
	// terminal in the memo state machine and no threshold can express "never".
	ScribePreacceptMin float64

	// ScribeMaxAttempts is §7's ceiling before a failure is recorded.
	ScribeMaxAttempts int
}

// ScribeEnabled reports whether Chronicle will produce routing proposals.
func (c Config) ScribeEnabled() bool { return c.ScribeOllamaURL != "" }

// Tier1IsSeparate reports whether the tier-1 pool has a DSN of its own rather
// than falling back to the main one.
//
// False means derived writers are connecting as `chronicle`, which can read
// and write tier 2 — so the role grant is in place and nothing is standing
// behind it. Serving warns; CHRN-52 decides whether it should refuse.
func (c Config) Tier1IsSeparate() bool {
	return c.Tier1DatabaseURL != "" && c.Tier1DatabaseURL != c.DatabaseURL
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
	if c.ASRBaseURL != "" {
		// Parsed here rather than left to the generated client, which only
		// string-concatenates and does its url.Parse per request. Unparsed, a
		// typo boots cleanly, logs "transcription enabled", and then fails
		// every submission with a warn line -- the "configured and unusable"
		// shape runServe refuses outright three lines away when the audio
		// directory is missing.
		u, err := url.Parse(c.ASRBaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return c, fmt.Errorf("config: CHRONICLE_ASR_URL %q is not an absolute http(s) URL", c.ASRBaseURL)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return c, fmt.Errorf("config: CHRONICLE_ASR_URL %q must be http or https, got scheme %q", c.ASRBaseURL, u.Scheme)
		}
	}
	c.ASRModel = strings.TrimSpace(os.Getenv("CHRONICLE_ASR_MODEL"))

	// CHRN-28's ceiling: how many attempts one memo gets before it is held for
	// a human. Zero is refused rather than read as "no ceiling" — an unbounded
	// retry loop is the thing this setting exists to close, and it should not
	// be reachable by typing 0.
	c.ASRMaxAttempts = DefaultASRMaxAttempts
	if v := strings.TrimSpace(os.Getenv("CHRONICLE_ASR_MAX_ATTEMPTS")); v != "" {
		c.ASRMaxAttempts, err = strconv.Atoi(v)
		if err != nil || c.ASRMaxAttempts < 1 {
			return c, fmt.Errorf("config: CHRONICLE_ASR_MAX_ATTEMPTS %q is not a positive integer", v)
		}
	}

	c.TranscribeInterval, err = optionalDuration("CHRONICLE_TRANSCRIBE_INTERVAL")
	if err != nil {
		return c, err
	}

	// The tier-1 pool. Falls back rather than erroring: see the field comment.
	c.Tier1DatabaseURL = firstNonEmpty(
		strings.TrimSpace(os.Getenv("CHRONICLE_TIER1_DATABASE_URL")), c.DatabaseURL)

	// Parsed by LoadScribe so the same rules apply whether the whole config is
	// being read or only the routing half. CHRN-30's `chronicle eval` needs the
	// second: a synthetic run must work with no CHRONICLE_DATABASE_URL at all.
	sc, err := LoadScribe()
	if err != nil {
		return c, err
	}
	c.ScribeOllamaURL, c.ScribeModel = sc.OllamaURL, sc.Model
	c.ScribePreacceptMin, c.ScribeMaxAttempts = sc.PreacceptMin, sc.MaxAttempts

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

// Scribe is the routing half of the configuration, and it is loadable BY
// ITSELF.
//
// That is not tidiness. `chronicle eval --stratum synthetic` is deliberately
// runnable with no database and no environment — that is what makes it the
// half CI can run — and Load() refuses the moment CHRONICLE_DATABASE_URL is
// unset. A router built from the full config would make a synthetic run fail
// naming the DATABASE, and the honest complaint about an unset
// CHRONICLE_SCRIBE_OLLAMA_URL would never be reached.
//
// One parser, two callers: Load delegates here, so the pair rule and the
// bounds cannot drift between them.
type Scribe struct {
	// OllamaURL is CHRONICLE_SCRIBE_OLLAMA_URL. Empty disables routing.
	OllamaURL string
	// Model is CHRONICLE_SCRIBE_MODEL, e.g. `gemma4:31b`.
	Model string
	// PreacceptMin is CHRN-36's to set; the default admits nothing.
	PreacceptMin float64
	// MaxAttempts is CHRN-32 §7's ceiling.
	MaxAttempts int
}

// Enabled reports whether there is a model to ask.
func (s Scribe) Enabled() bool { return s.OllamaURL != "" }

// LoadScribe reads only the CHRONICLE_SCRIBE_* variables.
func LoadScribe() (Scribe, error) {
	var s Scribe
	var err error

	s.OllamaURL = strings.TrimRight(strings.TrimSpace(os.Getenv("CHRONICLE_SCRIBE_OLLAMA_URL")), "/")
	if s.OllamaURL != "" {
		u, err := url.Parse(s.OllamaURL)
		if err != nil || !u.IsAbs() || u.Host == "" {
			return s, fmt.Errorf("config: CHRONICLE_SCRIBE_OLLAMA_URL %q is not an absolute http(s) URL", s.OllamaURL)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return s, fmt.Errorf("config: CHRONICLE_SCRIBE_OLLAMA_URL %q must be http or https, got scheme %q", s.OllamaURL, u.Scheme)
		}
	}

	// Required alongside the URL. The proposer string is built from it and is
	// the identity of every row Scribe writes -- CHRN-36 attributes a
	// regression through it -- so there is no sensible default to invent, and
	// guessing one would silently credit a model that never ran.
	s.Model = strings.TrimSpace(os.Getenv("CHRONICLE_SCRIBE_MODEL"))
	if (s.OllamaURL == "") != (s.Model == "") {
		return s, fmt.Errorf("config: set both CHRONICLE_SCRIBE_OLLAMA_URL and CHRONICLE_SCRIBE_MODEL, or neither")
	}

	s.PreacceptMin = DefaultScribePreacceptMin
	if v := strings.TrimSpace(os.Getenv("CHRONICLE_SCRIBE_PREACCEPT_MIN")); v != "" {
		s.PreacceptMin, err = strconv.ParseFloat(v, 64)
		// Above 1 is allowed and is how pre-acceptance is turned off on
		// purpose; below 0 is refused, because a negative floor admits every
		// proposal including the ones the model had no confidence in at all.
		if err != nil || s.PreacceptMin < 0 {
			return s, fmt.Errorf("config: CHRONICLE_SCRIBE_PREACCEPT_MIN %q is not a number >= 0", v)
		}
	}

	s.MaxAttempts = DefaultScribeMaxAttempts
	if v := strings.TrimSpace(os.Getenv("CHRONICLE_SCRIBE_MAX_ATTEMPTS")); v != "" {
		s.MaxAttempts, err = strconv.Atoi(v)
		if err != nil || s.MaxAttempts < 1 {
			return s, fmt.Errorf("config: CHRONICLE_SCRIBE_MAX_ATTEMPTS %q is not a positive integer", v)
		}
	}
	return s, nil
}
