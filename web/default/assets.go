package defaultweb

import (
	"embed"

	platformhttp "github.com/sh2001sh/new-api/internal/platform/transport/http"
)

//go:embed dist
var buildFS embed.FS

//go:embed dist/index.html
var distIndexPage []byte

//go:embed index.html
var defaultIndexTemplate []byte

func BuildFS() embed.FS {
	return buildFS
}

func DefaultIndexPage() []byte {
	return platformhttp.MergeIndexShell(distIndexPage, defaultIndexTemplate)
}

func ThemeAssets(indexPage []byte) platformhttp.ThemeAssets {
	return platformhttp.ThemeAssets{
		DefaultBuildFS:   buildFS,
		DefaultIndexPage: indexPage,
	}
}
