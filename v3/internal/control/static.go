package control

import (
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
)

// Static serves precompressed Vite assets and the router entry. API misses stay
// errors, so the browser never interprets an HTML document as a successful API.
func Static(root string) http.Handler {
	dir := os.DirFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/.well-known/") {
			Fail(w, http.StatusNotFound, "not_found", "接口不存在")
			return
		}
		name, ok := resolveStaticAsset(dir, r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		setStaticCacheHeaders(w, name)
		original := name
		name = applyStaticEncoding(w, dir, name, r.Header.Get("Accept-Encoding"))
		serveStaticFile(w, r, dir, name, original)
	})
}

// resolveStaticAsset maps a request path to a file under dir, falling back
// to index.html for client-side routes (names without a file extension that
// don't exist). ok is false for a path outside dir or a dotted name that
// still doesn't exist (a genuine missing asset).
func resolveStaticAsset(dir fs.FS, requestPath string) (name string, ok bool) {
	name = strings.TrimPrefix(path.Clean("/"+requestPath), "/")
	if name == "." || name == "" {
		name = "index.html"
	}
	if !fs.ValidPath(name) {
		return "", false
	}
	if _, err := fs.Stat(dir, name); err != nil {
		if strings.Contains(path.Base(name), ".") {
			return "", false
		}
		name = "index.html"
	}
	return name, true
}

func setStaticCacheHeaders(w http.ResponseWriter, name string) {
	if strings.HasPrefix(name, "assets/") || strings.HasPrefix(name, "static/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Header().Add("Vary", "Accept-Encoding")
}

// applyStaticEncoding returns the precompressed variant of name the client
// accepts, if one exists on disk, setting Content-Encoding to match.
func applyStaticEncoding(w http.ResponseWriter, dir fs.FS, name, acceptEncoding string) string {
	for _, encoding := range []struct{ token, suffix string }{{"br", ".br"}, {"gzip", ".gz"}} {
		if acceptsEncoding(acceptEncoding, encoding.token) {
			if info, err := fs.Stat(dir, name+encoding.suffix); err == nil && !info.IsDir() {
				w.Header().Set("Content-Encoding", encoding.token)
				return name + encoding.suffix
			}
		}
	}
	return name
}

// serveStaticFile opens name within dir and streams it, using original as
// the Content-Type sniffing name (so a .br/.gz variant reports its
// uncompressed type).
func serveStaticFile(w http.ResponseWriter, r *http.Request, dir fs.FS, name, original string) {
	f, err := dir.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	reader, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "asset unavailable", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, original, info.ModTime(), reader)
}

func acceptsEncoding(header, token string) bool {
	for _, value := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(value), ";")
		if parts[0] != token && parts[0] != "*" {
			continue
		}
		for _, part := range parts[1:] {
			if q, ok := strings.CutPrefix(strings.TrimSpace(part), "q="); ok {
				quality, err := strconv.ParseFloat(q, 64)
				if err != nil || quality <= 0 {
					return false
				}
			}
		}
		return true
	}
	return false
}
