// Package apitest validates recorded responses against openapi.yaml.
//
// ============================================================================
// WHY THIS EXISTS: THE ONE GAP THE OTHER FOUR GUARDS LEAVE.
// ============================================================================
//
// CHRN-97's contract is held up in five places. Four of them are structural:
// the generated file is byte-compared against the document, every operation
// needs a handler to compile, every route's credential is declared or the
// binary does not start, and nobody writes the route registration at all.
//
// None of those says anything about what a handler WRITES. `std-http-server`
// hands the handler an http.ResponseWriter, so it can answer any status with
// any body -- including one the document does not describe. That is the price
// of not taking `strict-server`, which would have made an undeclared status
// unrepresentable at the cost of rewriting every shipped handler (ruling 1).
//
// So: this. Every test in this epic routes its recorded response through
// Conform, and a response the document does not describe fails there rather
// than in a client three epics later.
//
// ============================================================================
// IT TAKES THE OPERATION ID RATHER THAN ROUTING THE REQUEST.
// ============================================================================
//
// The obvious design loads a router from the spec and matches the request to an
// operation. It needs a second routing implementation -- kin-openapi's router
// packages pull in a third-party mux -- and that implementation could disagree
// with the one actually serving the request, which would make this guard's
// answer about a route nobody called.
//
// The calling test already knows which operation it exercised. Naming it is one
// argument, and it removes an entire class of wrong answer.
package apitest

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

var (
	once    sync.Once
	doc     *openapi3.T
	byOp    map[string]*openapi3.Operation
	loadErr error
)

// Doc returns the parsed contract, loading it once per test binary.
//
// Exported because the policy test reads x-chronicle-policy off it: the
// document is the single source of truth for who may call what, and two tests
// that each loaded their own copy would be two chances to load the wrong file.
func Doc(t *testing.T) *openapi3.T {
	t.Helper()
	load()
	if loadErr != nil {
		t.Fatalf("apitest: %v", loadErr)
	}
	return doc
}

// Operations returns every operation in the document, keyed by operationId.
func Operations(t *testing.T) map[string]*openapi3.Operation {
	t.Helper()
	Doc(t)
	return byOp
}

func load() {
	once.Do(func() {
		path, err := find()
		if err != nil {
			loadErr = err
			return
		}
		loader := openapi3.NewLoader()
		loader.IsExternalRefsAllowed = false
		d, err := loader.LoadFromFile(path)
		if err != nil {
			loadErr = fmt.Errorf("loading %s: %w", path, err)
			return
		}
		// Validate the document itself, not only the responses against it. A
		// spec that does not validate is one oapi-codegen may still have
		// generated from, quietly, by understanding half of it.
		if err := d.Validate(loader.Context); err != nil {
			loadErr = fmt.Errorf("%s does not validate: %w", path, err)
			return
		}
		doc = d
		byOp = map[string]*openapi3.Operation{}
		for _, item := range d.Paths.Map() {
			for _, op := range item.Operations() {
				if op.OperationID != "" {
					byOp[op.OperationID] = op
				}
			}
		}
	})
}

// find locates openapi.yaml by walking up to the module root, so a test in any
// package finds the same one file.
func find() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, "openapi.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no openapi.yaml between the working directory and the filesystem root")
		}
		dir = parent
	}
}

// Conform fails the test unless the recorded response is one the document
// describes for that operation: the status code is declared, the media type
// matches, and the body validates against the declared schema.
//
// It is deliberately strict about the status code. A handler answering an
// undeclared 409 is the exact drift this guard exists to catch, and "the
// document did not mention it" is the finding rather than something to skip
// over.
func Conform(t *testing.T, operationID string, rec *httptest.ResponseRecorder) {
	t.Helper()

	op, ok := Operations(t)[operationID]
	if !ok {
		t.Fatalf("apitest: openapi.yaml has no operation %q", operationID)
	}
	if op.Responses == nil {
		t.Fatalf("apitest: operation %q declares no responses at all", operationID)
	}

	code := rec.Code
	ref := op.Responses.Status(code)
	if ref == nil || ref.Value == nil {
		if ref = op.Responses.Default(); ref == nil || ref.Value == nil {
			t.Fatalf("apitest: %s answered %d, which openapi.yaml does not declare for it. "+
				"Declare the response or stop answering it.", operationID, code)
		}
	}

	// A response with no content declared must have no body -- 204, and the
	// several places this API answers a bare status.
	if len(ref.Value.Content) == 0 {
		if body := rec.Body.Bytes(); len(body) > 0 {
			t.Fatalf("apitest: %s answered %d with a %d-byte body, but openapi.yaml declares no content for it",
				operationID, code, len(body))
		}
		return
	}

	ct := rec.Header().Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatalf("apitest: %s answered %d with an unparseable Content-Type %q: %v",
			operationID, code, ct, err)
	}
	media := ref.Value.Content.Get(mediaType)
	if media == nil {
		t.Fatalf("apitest: %s answered %d as %q, which openapi.yaml does not declare for it (it declares %v)",
			operationID, code, mediaType, contentTypes(ref.Value.Content))
	}
	if media.Schema == nil || media.Schema.Value == nil {
		return
	}

	var body any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("apitest: %s answered %d with a body that is not JSON: %v", operationID, code, err)
	}
	if err := media.Schema.Value.VisitJSON(body); err != nil {
		t.Fatalf("apitest: %s answered %d with a body openapi.yaml does not describe: %v",
			operationID, code, err)
	}
}

func contentTypes(c openapi3.Content) []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	return out
}
