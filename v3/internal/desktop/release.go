package desktop

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type ReleaseAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest,omitempty"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Platform           string `json:"platform,omitempty"`
	Arch               string `json:"arch,omitempty"`
	TauriTarget        string `json:"tauri_target,omitempty"`
}
type ReleasePlatform struct {
	Signature string `json:"signature"`
	URL       string `json:"url"`
}
type ReleaseManifest struct {
	TagName     string                     `json:"tag_name"`
	Version     string                     `json:"version"`
	HTMLURL     string                     `json:"html_url"`
	PublishedAt string                     `json:"published_at,omitempty"`
	Notes       string                     `json:"notes,omitempty"`
	HomebrewURL string                     `json:"homebrew_url,omitempty"`
	Assets      []ReleaseAsset             `json:"assets"`
	Platforms   map[string]ReleasePlatform `json:"platforms,omitempty"`
}

// LoadReleaseManifest retains the deployed inline/file manifest variables.
// Missing release metadata is an explicit unavailable response.
func configuredReleaseManifest() (*ReleaseManifest, error) {
	b := []byte(os.Getenv("CODEGO_DESKTOP_RELEASE_MANIFEST_JSON"))
	if len(b) == 0 {
		path := os.Getenv("CODEGO_DESKTOP_RELEASE_MANIFEST_FILE")
		if path == "" {
			return nil, errors.New("desktop release channel is not configured")
		}
		var err error
		b, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}
	}
	if len(b) > 1<<20 {
		return nil, ErrInvalid
	}
	var out ReleaseManifest
	if json.Unmarshal(b, &out) != nil {
		return nil, ErrInvalid
	}
	if out.Version == "" {
		out.Version = strings.TrimPrefix(out.TagName, "v")
	}
	if out.Version == "" {
		return nil, ErrInvalid
	}
	for _, asset := range out.Assets {
		if !releaseURL(asset.BrowserDownloadURL) {
			return nil, ErrInvalid
		}
	}
	for _, p := range out.Platforms {
		if !releaseURL(p.URL) || p.Signature == "" {
			return nil, ErrInvalid
		}
	}
	return &out, nil
}
func releaseURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http")
}
func (s *Service) releaseHTTP(w http.ResponseWriter, r *http.Request) {
	load := s.cfg.ReleaseManifest
	if load == nil {
		load = LoadReleaseManifest
	}
	out, err := load(r.Context())
	if err != nil {
		reply(w, nil, err)
		return
	}
	if strings.HasSuffix(r.URL.Path, ".json") {
		b, err := json.Marshal(out)
		if err != nil {
			reply(w, nil, err)
			return
		}
		var manifest ReleaseManifest
		if json.Unmarshal(b, &manifest) != nil || len(manifest.Platforms) == 0 {
			reply(w, nil, ErrInvalid)
			return
		}
		out = map[string]any{"version": manifest.Version, "notes": manifest.Notes, "pub_date": manifest.PublishedAt, "platforms": manifest.Platforms}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
