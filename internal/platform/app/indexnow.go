package app

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
)

const (
	indexNowEndpointDefault = "https://api.indexnow.org/indexnow"
	indexNowDistPath        = "web/default/dist"
	indexNowSubmitTimeout   = 15 * time.Second
)

type indexNowPayload struct {
	Host        string   `json:"host"`
	Key         string   `json:"key"`
	KeyLocation string   `json:"keyLocation"`
	URLList     []string `json:"urlList"`
}

type indexNowURLSet struct {
	URLs []struct {
		Location string `xml:"loc"`
	} `xml:"url"`
}

var indexNowBootstrapOnce sync.Once

func StartIndexNowSubmissionTask(buildFS fs.FS) {
	indexNowBootstrapOnce.Do(func() {
		if !platformconfig.IsMasterNode {
			return
		}
		if strings.EqualFold(strings.TrimSpace(os.Getenv("INDEXNOW_ENABLED")), "false") {
			platformobservability.SysLog("indexnow bootstrap disabled by INDEXNOW_ENABLED=false")
			return
		}

		gopool.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), indexNowSubmitTimeout)
			defer cancel()

			if err := submitIndexNowFromFS(ctx, buildFS); err != nil {
				platformobservability.SysError("indexnow bootstrap failed: " + err.Error())
			}
		})
	})
}

func submitIndexNowFromFS(ctx context.Context, buildFS fs.FS) error {
	payload, err := buildIndexNowPayload(buildFS)
	if err != nil {
		return err
	}
	if len(payload.URLList) == 0 {
		return fmt.Errorf("indexnow skipped: no urls discovered from sitemap")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal indexnow payload: %w", err)
	}

	endpoint := strings.TrimSpace(os.Getenv("INDEXNOW_ENDPOINT"))
	if endpoint == "" {
		endpoint = indexNowEndpointDefault
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create indexnow request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := platformhttpx.GetHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("submit indexnow request: %w", err)
	}
	defer closeresp(resp)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("read indexnow response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("indexnow returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	platformobservability.SysLog(fmt.Sprintf("indexnow submitted %d urls for host %s", len(payload.URLList), payload.Host))
	return nil
}

func buildIndexNowPayload(buildFS fs.FS) (indexNowPayload, error) {
	distFS, err := fs.Sub(buildFS, indexNowDistPath)
	if err != nil {
		return indexNowPayload{}, fmt.Errorf("open dist root for indexnow: %w", err)
	}

	keyFileName, keyValue, err := findIndexNowKey(distFS)
	if err != nil {
		return indexNowPayload{}, err
	}

	urls, err := loadIndexNowURLs(distFS)
	if err != nil {
		return indexNowPayload{}, err
	}
	if len(urls) == 0 {
		return indexNowPayload{}, nil
	}

	firstURL, err := url.Parse(urls[0])
	if err != nil {
		return indexNowPayload{}, fmt.Errorf("parse first sitemap url: %w", err)
	}
	if firstURL.Scheme == "" || firstURL.Host == "" {
		return indexNowPayload{}, fmt.Errorf("invalid sitemap url: %s", urls[0])
	}

	return indexNowPayload{
		Host:        firstURL.Host,
		Key:         keyValue,
		KeyLocation: fmt.Sprintf("%s://%s/%s", firstURL.Scheme, firstURL.Host, keyFileName),
		URLList:     urls,
	}, nil
}

func findIndexNowKey(distFS fs.FS) (string, string, error) {
	entries, err := fs.ReadDir(distFS, ".")
	if err != nil {
		return "", "", fmt.Errorf("read dist root for indexnow key: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".txt") {
			continue
		}

		content, err := fs.ReadFile(distFS, entry.Name())
		if err != nil {
			return "", "", fmt.Errorf("read indexnow key file %s: %w", entry.Name(), err)
		}

		keyValue := strings.TrimSpace(string(content))
		if keyValue == "" {
			continue
		}

		fileBase := strings.TrimSuffix(filepath.Base(entry.Name()), filepath.Ext(entry.Name()))
		if keyValue == fileBase {
			return entry.Name(), keyValue, nil
		}
	}

	return "", "", fmt.Errorf("indexnow key file not found in dist root")
}

func loadIndexNowURLs(distFS fs.FS) ([]string, error) {
	sitemapContent, err := fs.ReadFile(distFS, "sitemap.xml")
	if err != nil {
		return nil, fmt.Errorf("read sitemap.xml for indexnow: %w", err)
	}

	var sitemap indexNowURLSet
	if err := xml.Unmarshal(sitemapContent, &sitemap); err != nil {
		return nil, fmt.Errorf("parse sitemap.xml for indexnow: %w", err)
	}

	var urls []string
	seen := make(map[string]struct{}, len(sitemap.URLs))
	siteHost := ""
	siteScheme := ""

	for _, item := range sitemap.URLs {
		loc := strings.TrimSpace(item.Location)
		if loc == "" {
			continue
		}

		parsed, err := url.Parse(loc)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		if siteHost == "" {
			siteHost = parsed.Host
			siteScheme = parsed.Scheme
		}
		if parsed.Host != siteHost {
			continue
		}

		normalized := parsed.String()
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		urls = append(urls, normalized)
	}

	if siteHost != "" {
		homeURL := fmt.Sprintf("%s://%s/", siteScheme, siteHost)
		if _, exists := seen[homeURL]; !exists {
			urls = append([]string{homeURL}, urls...)
		}
	}
	return urls, nil
}

func closeresp(httpResponse *http.Response) {
	if httpResponse == nil || httpResponse.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(httpResponse.Body, 1024))
	_ = httpResponse.Body.Close()
}
