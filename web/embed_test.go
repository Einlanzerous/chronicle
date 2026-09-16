package web

import (
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// TestHandlerFSServesKnownFiles proves the two real-file paths: the entry
// document and a hashed asset, both fetched by their exact embedded path.
func TestHandlerFSServesKnownFiles(t *testing.T) {
	root := fstest.MapFS{
		"index.html":             {Data: []byte("<!doctype html><title>t</title>")},
		"assets/index-abc123.js": {Data: []byte("console.log(1)")},
	}
	h := HandlerFS(root)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/app/", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /app/ = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("index.html Cache-Control = %q, want no-cache", cc)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/app/assets/index-abc123.js", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /app/assets/index-abc123.js = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "console.log(1)" {
		t.Errorf("asset body = %q, want the file content verbatim", rec.Body.String())
	}
	// Vite bakes a content hash into the filename, so this asset is safe to
	// cache immutably for a year -- unlike index.html above, which must
	// always be refetched.
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("asset Cache-Control = %q, want a long-lived immutable directive", cc)
	}
}

// TestHandlerFSFallsBackToIndexForDeepLinks is the SPA behaviour a client
// reload of a deep link like /app/notes/CHR-0311 depends on: no file at that
// path exists in the bundle, so the router's own index.html is served
// instead of a 404, and client-side routing takes it from there.
func TestHandlerFSFallsBackToIndexForDeepLinks(t *testing.T) {
	root := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><title>t</title>")},
	}
	h := HandlerFS(root)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/app/notes/CHR-0311", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /app/notes/CHR-0311 = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "<!doctype html><title>t</title>" {
		t.Errorf("body = %q, want index.html's content", rec.Body.String())
	}
}

// TestHandlerFSFallsBackToIndexForADirectory is CHRN-53's review fix: a bare
// directory (e.g. /app/assets/) exists in the bundle, so a naive existence
// check would hand it to http.FileServer, which answers a directory
// listing -- a second, undocumented response from a handler whose contract
// is "a file, or index.html". fs.Stat + IsDir in HandlerFS routes it to the
// SPA fallback instead, same as any other path with no file behind it.
func TestHandlerFSFallsBackToIndexForADirectory(t *testing.T) {
	root := fstest.MapFS{
		"index.html":             {Data: []byte("<!doctype html><title>t</title>")},
		"assets/index-abc123.js": {Data: []byte("console.log(1)")},
	}
	h := HandlerFS(root)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/app/assets/", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /app/assets/ = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "<!doctype html><title>t</title>" {
		t.Errorf("body = %q, want index.html's content, not a directory listing", rec.Body.String())
	}
}

// TestHandlerFSAnswersNotBuiltWhenOnlyThePlaceholderExists is the fallback
// the checked-in dist/.gitkeep exercises for real: an fs.FS with no
// index.html at all, which is exactly what CI's Go job compiles against,
// since it does not run bun first.
func TestHandlerFSAnswersNotBuiltWhenOnlyThePlaceholderExists(t *testing.T) {
	root := fstest.MapFS{
		".gitkeep": {Data: []byte{}},
	}
	h := HandlerFS(root)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/app/", nil))
	if rec.Code != 500 {
		t.Fatalf("GET /app/ with no index.html = %d, want 500", rec.Code)
	}
	if got := rec.Body.String(); got != "web UI not built\n" {
		t.Errorf("body = %q, want a clear not-built message", got)
	}
}
