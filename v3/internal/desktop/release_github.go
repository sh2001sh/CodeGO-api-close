package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type updaterManifest struct {
	Version   string                     `json:"version"`
	Notes     string                     `json:"notes"`
	PubDate   string                     `json:"pub_date"`
	Platforms map[string]ReleasePlatform `json:"platforms"`
}

var releaseRepository = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func LoadReleaseManifest(ctx context.Context) (any, error) {
	configured, configErr := configuredReleaseManifest()
	enabled := strings.ToLower(strings.TrimSpace(os.Getenv("CODEGO_DESKTOP_RELEASE_GITHUB_FALLBACK_ENABLED")))
	if enabled == "false" || enabled == "off" || enabled == "no" || enabled == "0" {
		return configured, configErr
	}
	remote, err := githubRelease(ctx, &http.Client{Timeout: 10 * time.Second}, "https://api.github.com")
	if err != nil {
		if configured != nil {
			slog.Warn("desktop release GitHub fallback unavailable", "err", err)
			return configured, nil
		}
		return nil, err
	}
	if configured == nil {
		if configErr != nil && (os.Getenv("CODEGO_DESKTOP_RELEASE_MANIFEST_JSON") != "" || os.Getenv("CODEGO_DESKTOP_RELEASE_MANIFEST_FILE") != "") {
			return nil, configErr
		}
		return remote, nil
	}
	if newerVersion(remote.Version, configured.Version) {
		return remote, nil
	}
	return configured, nil
}

func githubRelease(ctx context.Context, client *http.Client, base string) (*ReleaseManifest, error) {
	repo := strings.TrimSpace(os.Getenv("CODEGO_DESKTOP_RELEASE_GITHUB_REPOSITORY"))
	if repo == "" {
		repo = "sh2001sh/CodeGO"
	}
	if !releaseRepository.MatchString(repo) || strings.Contains(repo, "..") {
		return nil, ErrInvalid
	}
	b, err := releaseGet(ctx, client, strings.TrimRight(base, "/")+"/repos/"+repo+"/releases/latest")
	if err != nil {
		return nil, err
	}
	var source struct {
		TagName     string         `json:"tag_name"`
		Name        string         `json:"name"`
		HTMLURL     string         `json:"html_url"`
		PublishedAt string         `json:"published_at"`
		Assets      []ReleaseAsset `json:"assets"`
	}
	if json.Unmarshal(b, &source) != nil {
		return nil, ErrInvalid
	}
	out := &ReleaseManifest{TagName: source.TagName, Version: strings.TrimPrefix(source.TagName, "v"), HTMLURL: source.HTMLURL, PublishedAt: source.PublishedAt, Notes: source.Name, Assets: []ReleaseAsset{}, Platforms: map[string]ReleasePlatform{}}
	if out.Version == "" {
		return nil, ErrInvalid
	}
	assets := map[string]string{}
	latest := ""
	for _, asset := range source.Assets {
		if !releaseURL(asset.BrowserDownloadURL) {
			return nil, ErrInvalid
		}
		assets[asset.Name] = asset.BrowserDownloadURL
		if asset.Name == "latest.json" {
			latest = asset.BrowserDownloadURL
		}
		if strings.HasPrefix(asset.Name, "CodeGo_") {
			asset.Platform, asset.Arch, asset.TauriTarget = assetTarget(asset.Name)
			if asset.Platform != "" {
				out.Assets = append(out.Assets, asset)
			}
		}
	}
	if latest != "" {
		// Metadata came from GitHub, but never follow an arbitrary host supplied
		// by a compromised manifest into the local control-plane network.
		u, _ := url.Parse(latest)
		baseURL, _ := url.Parse(base)
		if u.Hostname() != "github.com" && u.Hostname() != "objects.githubusercontent.com" && u.Host != baseURL.Host {
			return nil, ErrInvalid
		}
		b, err := releaseGet(ctx, client, latest)
		if err != nil {
			return nil, err
		}
		var update updaterManifest
		if json.Unmarshal(b, &update) != nil {
			return nil, ErrInvalid
		}
		if update.Version != "" {
			out.Version = update.Version
			out.TagName = "v" + update.Version
		}
		if update.Notes != "" {
			out.Notes = update.Notes
		}
		if update.PubDate != "" {
			out.PublishedAt = update.PubDate
		}
		for target, p := range update.Platforms {
			u, err := url.Parse(p.URL)
			if err != nil {
				return nil, ErrInvalid
			}
			if mapped := assets[path.Base(u.Path)]; mapped != "" {
				p.URL = mapped
			}
			if !releaseURL(p.URL) || p.Signature == "" {
				return nil, ErrInvalid
			}
			out.Platforms[target] = p
		}
	}
	return out, nil
}
func releaseGet(ctx context.Context, client *http.Client, endpoint string) ([]byte, error) {
	r, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", "CodeGoDesktopReleaseChannel")
	resp, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("desktop release HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 1<<20 {
		return nil, ErrInvalid
	}
	return b, nil
}
func assetTarget(name string) (string, string, string) {
	name = strings.ToLower(name)
	arch := "x86_64"
	if strings.Contains(name, "aarch64") || strings.Contains(name, "arm64") {
		arch = "aarch64"
	}
	switch {
	case strings.HasSuffix(name, ".dmg"), strings.HasSuffix(name, ".app.tar.gz"):
		return "macos", arch, "darwin-" + arch
	case strings.HasSuffix(name, ".msi"), strings.HasSuffix(name, ".exe"):
		return "windows", arch, "windows-" + arch
	case strings.HasSuffix(name, ".appimage"), strings.HasSuffix(name, ".deb"):
		return "linux", arch, "linux-" + arch
	default:
		return "", "", ""
	}
}
func newerVersion(a, b string) bool {
	aa, bb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < max(len(aa), len(bb)); i++ {
		av, bv := 0, 0
		if i < len(aa) {
			av, _ = strconv.Atoi(aa[i])
		}
		if i < len(bb) {
			bv, _ = strconv.Atoi(bb[i])
		}
		if av != bv {
			return av > bv
		}
	}
	return false
}
