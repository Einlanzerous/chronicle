package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
)

// The error shape, once, for every route in openapi.yaml (CHRN-97).
//
// `code` is the stable machine token a client branches on; `message` is the
// sentence a person reads and may change without notice. Two fields and no
// third: a details bag is where a handler leaks the shape of the id space one
// convenience at a time, which is the regression pathUUID's comment already
// refuses.
//
// These constants are the whole vocabulary. Adding one means adding it to the
// document in the same commit, because apitest.Conform validates every response
// this package produces against openapi.yaml and an undeclared shape fails
// there rather than in production.
const (
	codeUnauthorized = "unauthorized"
	codeOwnerOnly    = "owner_only"
	codeRateLimited  = "rate_limited"
	codeInternal     = "internal"

	// codeInvalidParameter is the GENERATED wrapper's error, not a handler's.
	// See bindError.
	codeInvalidParameter = "invalid_parameter"

	// The three "configured elsewhere, or not at all" refusals. They are 503
	// and not 404 on the house rule this codebase states in four places: "not
	// configured here" and "wrong URL" are different facts and a client should
	// be able to tell them apart.
	codeAudioUnconfigured         = "audio_unconfigured"
	codeTranscriptionUnconfigured = "transcription_unconfigured"
	codeAccountsUnconfigured      = "accounts_unconfigured"
)

// writeError answers with the documented envelope.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, wire.Error{Code: code, Message: message})
}

// bindError answers the generated ServerInterfaceWrapper's parameter-binding
// failures.
//
// IT IS SUPPLIED BECAUSE THE DEFAULT UNDOES THE ERROR-SHAPE DECISION.
// oapi-codegen's HandlerWithOptions installs this when ErrorHandlerFunc is nil:
//
//	http.Error(w, err.Error(), http.StatusBadRequest)
//
// which is text/plain, and reachable on the shipped surface the moment a route
// binds a parameter -- a malformed {id}, or an unparseable Upload-Offset. That
// code lives in wire.gen.go, so it is not "a handler" and every other guard in
// this package passes while it answers.
//
// IT DOES NOT RELAY err.Error(). The generated text is
// `Invalid format for parameter id: invalid UUID length: 5`, where pathUUID
// answers a flat "invalid id" and says why in its comment: "rather than leaking
// the shape of the id space through a lookup". The code carries the
// machine-readable part; the message stays as flat as the handler's.
//
// The cause is logged, at warn, because a client sending malformed parameters
// is worth seeing and the response deliberately does not describe itself.
func bindError(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		logger.WarnContext(r.Context(), "request parameter did not bind",
			"http.url", r.URL.Path, "error", err)

		// Required-parameter failures are a different fact from malformed ones
		// -- one is a client that did not send something, the other a client
		// that sent nonsense -- and a client can act on the difference.
		var required *wire.RequiredParamError
		var requiredHeader *wire.RequiredHeaderError
		if errors.As(err, &required) || errors.As(err, &requiredHeader) {
			writeError(w, http.StatusBadRequest, codeInvalidParameter,
				"a required parameter is missing")
			return
		}
		writeError(w, http.StatusBadRequest, codeInvalidParameter,
			"a parameter is missing or malformed")
	}
}
