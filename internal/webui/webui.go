// Package webui is the read-only topology web UI (#107 §16): static files
// embedded in the binary. The page reads the topology API with the
// signed-in user's token and changes nothing.
package webui

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static
var files embed.FS

// CSP allows only the UI's own script, style, and API; no inline script,
// no framing, no form posts.
const CSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// etags are the files' content hashes, so a browser revalidates with
// If-None-Match and gets 304 until a new server version changes a file
// (embedded files have no modification time).
var etags = func() map[string]string {
	out := map[string]string{}
	_ = fs.WalkDir(files, "static", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := files.ReadFile(path)
			sum := sha256.Sum256(b)
			out[strings.TrimPrefix(path, "static")] = `"` + hex.EncodeToString(sum[:8]) + `"`
		}
		return err
	})
	out["/"] = out["/index.html"]
	return out
}()

// Handler serves the UI's files at the root it is mounted under (strip the
// prefix first), with the CSP; the browser revalidates every file (no-cache
// with an ETag), so a new server version is picked up on reload.
func Handler() http.Handler {
	static, _ := fs.Sub(files, "static") // the embedded directory exists
	fileServer := http.FileServer(http.FS(static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", CSP)
		h.Set("Cache-Control", "no-cache")
		h.Set("Referrer-Policy", "no-referrer")
		if tag, ok := etags[r.URL.Path]; ok {
			h.Set("ETag", tag)
		}
		fileServer.ServeHTTP(w, r)
	})
}
