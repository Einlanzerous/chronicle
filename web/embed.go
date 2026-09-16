// Package web embeds the built Vite SPA (web/dist/) so the Go binary can
// serve the app UI same-origin with the JSON API, under the /app/ prefix
// (CHRN-53). The API owns the root -- GET /notes/{ref} is JSON, and E9/E10
// consume it there -- so a client route like /app/notes/CHR-0311 cannot also
// be a page at /notes/CHR-0311; see the decision comment on CHRN-53 for the
// full router doctrine. internal/api/router.go mounts Handler() at "/app/"
// directly on the mux, outside policyRouter: it serves files, takes no
// credential and returns no API payload, so it is not an operation the
// credential policy table is about.
//
// The embed pattern always resolves because a placeholder dist/.gitkeep is
// checked in (the `all:` prefix includes dotfiles); a real bundle is produced
// by `bun run build` (deploy/Dockerfile's web stage) before `go build`. When
// only the placeholder is present, Handler serves a clear "web UI not built"
// message rather than a stale or empty page -- see HandlerFS's doc for how
// that is proven without a real bun build in CI's Go job.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// prefix is the base path Vite built the bundle for (web/vite.config.ts's
// `base`) and the mount point router.go registers this handler at. The two
// must agree: hashed asset URLs baked into index.html at build time carry it.
const prefix = "/app/"

// dist returns the embedded bundle rooted at dist/.
func dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Only possible if the embed directive and this path disagree, which is
		// a compile-time-adjacent programming error, not a runtime condition.
		panic("web: dist subtree missing: " + err.Error())
	}
	return sub
}

// Handler serves the embedded SPA, mounted at the /app/ prefix.
func Handler() http.Handler {
	return HandlerFS(dist())
}

// HandlerFS is Handler's logic over an arbitrary fs.FS. Existing files
// (index.html, hashed assets) are served directly; any other path under
// /app/ falls back to index.html so client-side routing handles a deep link
// like /app/notes/CHR-0311 on reload.
//
// Factored out from Handler so a test can exercise the SPA-serving
// behaviour -- the index fallback, the not-built answer -- against a small
// fixture rather than a real bun build. CI's Go job (build, vet, test)
// deliberately does not run bun first, so the compiled-in dist/ there is
// only the checked-in placeholder; embed_test.go and
// internal/api's router test both call this with an in-memory fs.FS instead.
func HandlerFS(root fs.FS) http.Handler {
	fileServer := http.StripPrefix(prefix, http.FileServer(http.FS(root)))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean(strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, prefix), "/"))
		if clean == "." || clean == "" {
			serveIndex(w, root)
			return
		}
		// Serve the file if it exists in the bundle; otherwise SPA-fallback.
		if f, err := root.Open(clean); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		serveIndex(w, root)
	})
}

// serveIndex writes index.html as the SPA entry document. It is marked
// no-cache: the document is tiny and must always reflect the current asset
// hashes, while the hashed assets themselves are safely cacheable. When the
// bundle is only the checked-in placeholder, there is no index.html to read,
// and that is answered plainly rather than with a stale or empty page.
func serveIndex(w http.ResponseWriter, root fs.FS) {
	data, err := fs.ReadFile(root, "index.html")
	if err != nil {
		http.Error(w, "web UI not built", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}
