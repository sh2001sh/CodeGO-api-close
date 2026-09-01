package filex

import (
	"fmt"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/types"
)

// PrefetchFileSources loads distinct file sources concurrently into the
// request-scoped cache. Adaptors can then build the upstream payload without
// serially waiting on every remote image.
//
// Prefetch is best effort: the caller still performs its normal load and
// validation path for each source, preserving existing error semantics.
func PrefetchFileSources(c *gin.Context, sources []types.FileSource, reason string) {
	if len(sources) == 0 {
		return
	}

	// Deduplicate by identifier before starting workers. Separate DTO objects
	// often refer to the same historical image URL but carry different source
	// instances; loading one is enough because LoadFileSource shares the
	// request-scoped cache by URL.
	unique := make([]types.FileSource, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if source == nil {
			continue
		}
		// GetIdentifier is intentionally truncated for logging; use the raw
		// source value as the deduplication key to avoid collapsing distinct URLs
		// that share a long prefix.
		identifier := source.GetRawData()
		if identifier == "" {
			identifier = source.GetIdentifier()
		}
		if identifier == "" {
			identifier = fmt.Sprintf("%T:%p", source, source)
		}
		if _, ok := seen[identifier]; ok {
			continue
		}
		seen[identifier] = struct{}{}
		unique = append(unique, source)
	}
	if len(unique) == 0 {
		return
	}
	if len(unique) == 1 {
		_, _ = LoadFileSource(c, unique[0], reason)
		return
	}

	workers := len(unique)
	if workers > 4 {
		workers = 4
	}
	jobs := make(chan types.FileSource)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for source := range jobs {
				// Errors are intentionally ignored here. The conversion path will
				// load the source again and report the same detailed error as before.
				_, _ = LoadFileSource(c, source, reason)
			}
		}()
	}
	for _, source := range unique {
		jobs <- source
	}
	close(jobs)
	wg.Wait()
}
