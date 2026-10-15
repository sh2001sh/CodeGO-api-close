package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	DesktopReleaseManifestJSONEnv = "CODEGO_DESKTOP_RELEASE_MANIFEST_JSON"
	DesktopReleaseManifestFileEnv = "CODEGO_DESKTOP_RELEASE_MANIFEST_FILE"
	desktopReleaseDefaultPagePath = "/download"

	desktopReleaseGitHubEnabledEnv  = "CODEGO_DESKTOP_RELEASE_GITHUB_FALLBACK_ENABLED"
	desktopReleaseGitHubRepoEnv     = "CODEGO_DESKTOP_RELEASE_GITHUB_REPOSITORY"
	defaultDesktopReleaseGitHubRepo = "sh2001sh/CodeGO"
)

var ErrDesktopReleaseNotConfigured = errors.New("desktop release channel is not configured")

var DesktopReleaseHTTPClient = &http.Client{Timeout: 10 * time.Second}
var DesktopReleaseGitHubAPIBaseURL = "https://api.github.com"

type DesktopReleaseAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest,omitempty"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Platform           string `json:"platform,omitempty"`
	Arch               string `json:"arch,omitempty"`
	TauriTarget        string `json:"tauri_target,omitempty"`
}

type DesktopReleasePlatform struct {
	Signature string `json:"signature"`
	URL       string `json:"url"`
}

type DesktopReleaseManifest struct {
	TagName     string                            `json:"tag_name"`
	Version     string                            `json:"version"`
	HTMLURL     string                            `json:"html_url"`
	PublishedAt string                            `json:"published_at,omitempty"`
	Notes       string                            `json:"notes,omitempty"`
	HomebrewURL string                            `json:"homebrew_url,omitempty"`
	Assets      []DesktopReleaseAsset             `json:"assets"`
	Platforms   map[string]DesktopReleasePlatform `json:"platforms,omitempty"`
}

type DesktopUpdaterManifest struct {
	Version   string                            `json:"version"`
	Notes     string                            `json:"notes,omitempty"`
	PubDate   string                            `json:"pub_date,omitempty"`
	Platforms map[string]DesktopReleasePlatform `json:"platforms"`
}

type desktopGitHubRelease struct {
	TagName     string               `json:"tag_name"`
	Name        string               `json:"name"`
	HTMLURL     string               `json:"html_url"`
	PublishedAt string               `json:"published_at"`
	Assets      []desktopGitHubAsset `json:"assets"`
}

type desktopGitHubAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// LoadDesktopReleaseManifest returns the public desktop release manifest.
func LoadDesktopReleaseManifest() (*DesktopReleaseManifest, error) {
	configured, err := loadConfiguredDesktopReleaseManifest()
	if !desktopReleaseGitHubFallbackEnabled() {
		return configured, err
	}

	githubManifest, githubErr := fetchDesktopGitHubReleaseManifest()
	if githubErr != nil {
		if configured != nil {
			platformobservability.SysLog(fmt.Sprintf("desktop release GitHub fallback failed: %v", githubErr))
			return configured, nil
		}
		return nil, githubErr
	}
	if err != nil && errors.Is(err, ErrDesktopReleaseNotConfigured) {
		return githubManifest, nil
	}
	if err != nil {
		return nil, err
	}
	if isDesktopReleaseManifestNewer(githubManifest, configured) {
		return githubManifest, nil
	}
	return configured, nil
}

// LoadDesktopUpdaterReleaseManifest returns the desktop updater manifest source.
func LoadDesktopUpdaterReleaseManifest() (*DesktopReleaseManifest, error) {
	configured, err := loadConfiguredDesktopReleaseManifest()
	if !desktopReleaseGitHubFallbackEnabled() {
		return configured, err
	}

	githubManifest, githubErr := fetchDesktopGitHubReleaseManifest()
	if githubErr != nil {
		if configured != nil {
			platformobservability.SysLog(fmt.Sprintf("desktop release GitHub fallback failed: %v", githubErr))
			return configured, nil
		}
		return nil, githubErr
	}
	if len(githubManifest.Platforms) == 0 {
		if configured != nil {
			return configured, nil
		}
		return githubManifest, nil
	}
	if err != nil && errors.Is(err, ErrDesktopReleaseNotConfigured) {
		return githubManifest, nil
	}
	if err != nil {
		return nil, err
	}
	if isDesktopReleaseManifestNewer(githubManifest, configured) {
		return githubManifest, nil
	}
	return configured, nil
}

func desktopReleaseDefaultPageURL() string {
	return BuildURL(NormalizeDesktopServerAddress(""), desktopReleaseDefaultPagePath)
}

func normalizeDesktopReleaseURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	base := NormalizeDesktopServerAddress("")
	resolved := BuildURL(base, value)
	parsed, err := url.Parse(resolved)
	if err != nil {
		return "", fmt.Errorf("invalid release url %q: %w", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("release url must resolve to an absolute URL: %s", raw)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func deriveDesktopReleaseVersion(tagName string) string {
	version := strings.TrimSpace(tagName)
	version = strings.TrimPrefix(version, "v")
	version = strings.TrimPrefix(version, "V")
	return strings.TrimSpace(version)
}

func readDesktopReleaseManifestSource() ([]byte, error) {
	if raw := strings.TrimSpace(os.Getenv(DesktopReleaseManifestJSONEnv)); raw != "" {
		return []byte(raw), nil
	}

	path := strings.TrimSpace(os.Getenv(DesktopReleaseManifestFileEnv))
	if path == "" {
		return nil, ErrDesktopReleaseNotConfigured
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read desktop release manifest file %q: %w", path, err)
	}
	return content, nil
}

func normalizeDesktopReleaseManifest(manifest *DesktopReleaseManifest) error {
	if manifest == nil {
		return errors.New("desktop release manifest is empty")
	}

	manifest.TagName = strings.TrimSpace(manifest.TagName)
	manifest.Version = strings.TrimSpace(manifest.Version)
	if manifest.Version == "" {
		manifest.Version = deriveDesktopReleaseVersion(manifest.TagName)
	}
	if manifest.Version == "" {
		return errors.New("desktop release manifest is missing version")
	}
	if manifest.TagName == "" {
		manifest.TagName = "v" + manifest.Version
	}

	if published := strings.TrimSpace(manifest.PublishedAt); published != "" {
		if _, err := time.Parse(time.RFC3339, published); err != nil {
			return fmt.Errorf("desktop release published_at must be RFC3339: %w", err)
		}
		manifest.PublishedAt = published
	}

	if htmlURL := strings.TrimSpace(manifest.HTMLURL); htmlURL != "" {
		resolved, err := normalizeDesktopReleaseURL(htmlURL)
		if err != nil {
			return err
		}
		manifest.HTMLURL = resolved
	} else {
		manifest.HTMLURL = desktopReleaseDefaultPageURL()
	}

	if homebrewURL := strings.TrimSpace(manifest.HomebrewURL); homebrewURL != "" {
		resolved, err := normalizeDesktopReleaseURL(homebrewURL)
		if err != nil {
			return err
		}
		manifest.HomebrewURL = resolved
	}

	for i := range manifest.Assets {
		asset := &manifest.Assets[i]
		asset.Name = strings.TrimSpace(asset.Name)
		if asset.Name == "" {
			return fmt.Errorf("desktop release asset %d is missing name", i)
		}
		resolved, err := normalizeDesktopReleaseURL(asset.BrowserDownloadURL)
		if err != nil {
			return err
		}
		if resolved == "" {
			return fmt.Errorf("desktop release asset %q is missing browser_download_url", asset.Name)
		}
		asset.BrowserDownloadURL = resolved
		asset.Platform = strings.TrimSpace(asset.Platform)
		asset.Arch = strings.TrimSpace(asset.Arch)
		asset.TauriTarget = strings.TrimSpace(asset.TauriTarget)
		asset.Digest = strings.TrimSpace(asset.Digest)
	}

	if manifest.Platforms == nil {
		manifest.Platforms = map[string]DesktopReleasePlatform{}
	}
	normalizedPlatforms := make(map[string]DesktopReleasePlatform, len(manifest.Platforms))
	for key, platform := range manifest.Platforms {
		target := strings.TrimSpace(key)
		if target == "" {
			return errors.New("desktop release platforms contains an empty target")
		}
		resolved, err := normalizeDesktopReleaseURL(platform.URL)
		if err != nil {
			return err
		}
		if resolved == "" {
			return fmt.Errorf("desktop release platform %q is missing url", target)
		}
		signature := strings.TrimSpace(platform.Signature)
		if signature == "" {
			return fmt.Errorf("desktop release platform %q is missing signature", target)
		}
		normalizedPlatforms[target] = DesktopReleasePlatform{
			Signature: signature,
			URL:       resolved,
		}
	}
	manifest.Platforms = normalizedPlatforms
	return nil
}

func loadConfiguredDesktopReleaseManifest() (*DesktopReleaseManifest, error) {
	content, err := readDesktopReleaseManifestSource()
	if err != nil {
		return nil, err
	}

	var manifest DesktopReleaseManifest
	if err = json.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("failed to decode desktop release manifest: %w", err)
	}
	if err = normalizeDesktopReleaseManifest(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func desktopReleaseGitHubFallbackEnabled() bool {
	value := strings.TrimSpace(os.Getenv(desktopReleaseGitHubEnabledEnv))
	if value == "" {
		return true
	}
	return !strings.EqualFold(value, "false") &&
		!strings.EqualFold(value, "off") &&
		!strings.EqualFold(value, "no") &&
		value != "0"
}

func desktopReleaseGitHubRepository() string {
	repository := strings.TrimSpace(os.Getenv(desktopReleaseGitHubRepoEnv))
	if repository == "" {
		repository = defaultDesktopReleaseGitHubRepo
	}
	if !isSafeGitHubRepository(repository) {
		return defaultDesktopReleaseGitHubRepo
	}
	return repository
}

func isSafeGitHubRepository(repository string) bool {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if !isSafeGitHubRepositoryPart(part) {
			return false
		}
	}
	return true
}

func isSafeGitHubRepositoryPart(part string) bool {
	if part == "" || strings.Contains(part, "..") {
		return false
	}
	for _, r := range part {
		if r != '-' && r != '_' && r != '.' && (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func fetchDesktopGitHubReleaseManifest() (*DesktopReleaseManifest, error) {
	repository := desktopReleaseGitHubRepository()
	endpoint := strings.TrimRight(DesktopReleaseGitHubAPIBaseURL, "/") + "/repos/" + repository + "/releases/latest"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "CodeGoDesktopReleaseChannel")

	resp, err := DesktopReleaseHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch desktop release from GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub release request failed with HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read GitHub release response: %w", err)
	}
	return decodeDesktopGitHubReleaseManifest(body)
}

func decodeDesktopGitHubReleaseManifest(body []byte) (*DesktopReleaseManifest, error) {
	var release desktopGitHubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub release response: %w", err)
	}
	manifest := DesktopReleaseManifest{
		TagName:     strings.TrimSpace(release.TagName),
		HTMLURL:     strings.TrimSpace(release.HTMLURL),
		PublishedAt: strings.TrimSpace(release.PublishedAt),
		Notes:       strings.TrimSpace(release.Name),
		Assets:      buildDesktopReleaseAssetsFromGitHub(release.Assets),
		Platforms:   map[string]DesktopReleasePlatform{},
	}
	if manifest.Notes == "" {
		manifest.Notes = "Code Go Desktop " + manifest.TagName
	}
	if latestJSONURL := findDesktopGitHubLatestJSONURL(release.Assets); latestJSONURL != "" {
		updaterManifest, err := fetchDesktopGitHubUpdaterManifest(latestJSONURL)
		if err != nil {
			platformobservability.SysLog(fmt.Sprintf("desktop release GitHub latest.json fallback failed: %v", err))
		} else {
			applyDesktopUpdaterManifest(&manifest, updaterManifest, release.Assets)
		}
	}
	if err := normalizeDesktopReleaseManifest(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func findDesktopGitHubLatestJSONURL(assets []desktopGitHubAsset) string {
	for _, asset := range assets {
		if strings.EqualFold(strings.TrimSpace(asset.Name), "latest.json") {
			return strings.TrimSpace(asset.BrowserDownloadURL)
		}
	}
	return ""
}

func fetchDesktopGitHubUpdaterManifest(downloadURL string) (*DesktopUpdaterManifest, error) {
	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CodeGoDesktopReleaseChannel")

	resp, err := DesktopReleaseHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch desktop updater manifest from GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub updater manifest request failed with HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read GitHub updater manifest response: %w", err)
	}

	var manifest DesktopUpdaterManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub updater manifest: %w", err)
	}
	return &manifest, nil
}

func applyDesktopUpdaterManifest(
	releaseManifest *DesktopReleaseManifest,
	updaterManifest *DesktopUpdaterManifest,
	assets []desktopGitHubAsset,
) {
	if releaseManifest == nil || updaterManifest == nil {
		return
	}
	if version := strings.TrimSpace(updaterManifest.Version); version != "" {
		releaseManifest.Version = version
		releaseManifest.TagName = "v" + version
	}
	if notes := strings.TrimSpace(updaterManifest.Notes); notes != "" {
		releaseManifest.Notes = notes
	}
	if pubDate := strings.TrimSpace(updaterManifest.PubDate); pubDate != "" {
		releaseManifest.PublishedAt = pubDate
	}
	if len(updaterManifest.Platforms) > 0 {
		releaseManifest.Platforms = rewriteDesktopUpdaterPlatformURLs(updaterManifest.Platforms, assets)
	}
}

func rewriteDesktopUpdaterPlatformURLs(
	platforms map[string]DesktopReleasePlatform,
	assets []desktopGitHubAsset,
) map[string]DesktopReleasePlatform {
	if len(platforms) == 0 {
		return platforms
	}
	assetURLs := make(map[string]string, len(assets))
	for _, asset := range assets {
		name := strings.TrimSpace(asset.Name)
		downloadURL := strings.TrimSpace(asset.BrowserDownloadURL)
		if name != "" && downloadURL != "" {
			assetURLs[name] = downloadURL
		}
	}

	rewritten := make(map[string]DesktopReleasePlatform, len(platforms))
	for target, platform := range platforms {
		platform.URL = rewriteDesktopUpdaterPlatformURL(platform.URL, assetURLs)
		rewritten[target] = platform
	}
	return rewritten
}

func rewriteDesktopUpdaterPlatformURL(rawURL string, assetURLs map[string]string) string {
	value := strings.TrimSpace(rawURL)
	if value == "" || len(assetURLs) == 0 {
		return value
	}
	if downloadURL, ok := assetURLs[desktopReleaseURLFileName(value)]; ok {
		return downloadURL
	}
	return value
}

func desktopReleaseURLFileName(rawURL string) string {
	value := strings.TrimSpace(rawURL)
	if value == "" {
		return ""
	}
	if index := strings.IndexAny(value, "?#"); index >= 0 {
		value = value[:index]
	}
	if index := strings.LastIndex(value, "/"); index >= 0 {
		value = value[index+1:]
	}
	return value
}

func buildDesktopReleaseAssetsFromGitHub(assets []desktopGitHubAsset) []DesktopReleaseAsset {
	releaseAssets := make([]DesktopReleaseAsset, 0, len(assets))
	for _, asset := range assets {
		if !strings.HasPrefix(asset.Name, "CodeGo_") {
			continue
		}
		platform, arch, tauriTarget := inferDesktopReleaseAssetTarget(asset.Name)
		if platform == "" {
			continue
		}
		releaseAssets = append(releaseAssets, DesktopReleaseAsset{
			Name:               asset.Name,
			Size:               asset.Size,
			BrowserDownloadURL: asset.BrowserDownloadURL,
			Platform:           platform,
			Arch:               arch,
			TauriTarget:        tauriTarget,
		})
	}
	return releaseAssets
}

func inferDesktopReleaseAssetTarget(name string) (platform string, arch string, tauriTarget string) {
	lowerName := strings.ToLower(name)
	arch = "x64"
	if strings.Contains(lowerName, "_arm64") || strings.Contains(lowerName, "_aarch64") {
		arch = "arm64"
	}
	switch {
	case strings.HasSuffix(lowerName, ".msi") || strings.Contains(lowerName, "portable"):
		platform = "windows"
	case strings.HasSuffix(lowerName, ".dmg") || strings.HasSuffix(lowerName, ".app.tar.gz"):
		platform = "macos"
	case strings.HasSuffix(lowerName, ".appimage") || strings.HasSuffix(lowerName, ".deb") || strings.HasSuffix(lowerName, ".rpm"):
		platform = "linux"
	}
	if platform == "" {
		return "", arch, ""
	}
	return platform, arch, desktopReleaseTauriTarget(platform, arch)
}

func desktopReleaseTauriTarget(platform string, arch string) string {
	switch platform {
	case "windows":
		if arch == "arm64" {
			return "windows-aarch64"
		}
		return "windows-x86_64"
	case "linux":
		if arch == "arm64" {
			return "linux-aarch64"
		}
		return "linux-x86_64"
	case "macos":
		return "darwin-universal"
	default:
		return ""
	}
}

func isDesktopReleaseManifestNewer(candidate *DesktopReleaseManifest, current *DesktopReleaseManifest) bool {
	if candidate == nil || current == nil {
		return candidate != nil
	}
	candidateTime, candidateErr := time.Parse(time.RFC3339, candidate.PublishedAt)
	currentTime, currentErr := time.Parse(time.RFC3339, current.PublishedAt)
	if candidateErr == nil && currentErr == nil && !candidateTime.Equal(currentTime) {
		return candidateTime.After(currentTime)
	}
	return compareDesktopReleaseVersions(candidate.Version, current.Version) > 0
}

func compareDesktopReleaseVersions(left string, right string) int {
	leftParts := desktopReleaseVersionParts(left)
	rightParts := desktopReleaseVersionParts(right)
	for i := 0; i < len(leftParts) || i < len(rightParts); i++ {
		leftValue, rightValue := 0, 0
		if i < len(leftParts) {
			leftValue = leftParts[i]
		}
		if i < len(rightParts) {
			rightValue = rightParts[i]
		}
		if leftValue != rightValue {
			return leftValue - rightValue
		}
	}
	return 0
}

func desktopReleaseVersionParts(version string) []int {
	version = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(version), "v"), "V")
	parts := strings.Split(version, ".")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		value := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			value = value*10 + int(r-'0')
		}
		values = append(values, value)
	}
	return values
}
