package switchyard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ============================================================================
// The error body — what the operator is told when a create is refused.
// ============================================================================

// A 4xx is stored on CHRN-33's link row as `refused_reason` and shown to the
// operator on the memo that failed. "Bad Request" names none of the several
// ways a create can be wrong, so the body's own explanation is carried.
func TestARefusalCarriesSwitchyardsExplanation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "project ARGY is archived"})
	}))
	defer srv.Close()
	c, err := New(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.CreateTicket(context.Background(), aTicket(uuid.New()))
	var se *Error
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want a *Error", err)
	}
	if se.Detail != "project ARGY is archived" {
		t.Fatalf("Detail = %q, want Switchyard's own message", se.Detail)
	}
	if !strings.Contains(se.Error(), "project ARGY is archived") {
		t.Fatalf("Error() = %q, want it to carry the explanation", se.Error())
	}
	if se.Retryable() {
		t.Fatal("a 404 must not be retryable")
	}
}

// An unregistered status used to render as "switchyard: POST /v1/tickets: "
// with nothing after the colon, because http.StatusText answers empty for a
// code it does not know. The number is always available to fall back on.
func TestAnUnknownStatusStillSaysSomething(t *testing.T) {
	e := &Error{Status: 599, Method: "POST", Path: "/v1/tickets"}
	if !strings.Contains(e.Error(), "599") {
		t.Fatalf("Error() = %q, want the numeric status", e.Error())
	}
}

// A body that is not JSON is carried verbatim rather than dropped: guessing
// wrong about a shape is how an explanation becomes an empty string.
func TestANonJSONErrorBodyIsStillCarried(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream\n  connect  failed"))
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "tok")

	_, err := c.CreateTicket(context.Background(), aTicket(uuid.New()))
	var se *Error
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want a *Error", err)
	}
	// Whitespace collapsed: this string lands in a database column and a log
	// field, and both are read by somebody scanning.
	if se.Detail != "upstream connect failed" {
		t.Fatalf("Detail = %q", se.Detail)
	}
	if !se.Retryable() {
		t.Fatal("a 502 is retryable")
	}
}

// The token travels in a request header and never comes back in a response, so
// carrying a bounded body cannot leak it — but assert it rather than reason
// about it, because this is the one change that moved response bytes into an
// error string.
func TestTheErrorDetailNeverCarriesTheToken(t *testing.T) {
	const token = "super-secret-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer srv.Close()
	c, _ := New(srv.URL, token)

	_, err := c.CreateTicket(context.Background(), aTicket(uuid.New()))
	if strings.Contains(err.Error(), token) {
		t.Fatalf("the token reached an error: %v", err)
	}
}
