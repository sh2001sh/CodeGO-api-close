package runtime

import (
	"strings"

	"github.com/gin-gonic/gin"
)

const failedFaultDomainsContextKey = "failed_fault_domains"

// ExcludeFaultDomain records an upstream domain that already failed in this
// request. Subsequent retries must use a different domain when available.
func ExcludeFaultDomain(c *gin.Context, domain string) {
	if c == nil {
		return
	}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return
	}
	value, _ := c.Get(failedFaultDomainsContextKey)
	domains, _ := value.(map[string]struct{})
	if domains == nil {
		domains = make(map[string]struct{})
	}
	domains[domain] = struct{}{}
	c.Set(failedFaultDomainsContextKey, domains)
}

// IsFaultDomainExcluded reports whether this request has already failed on
// the given upstream domain.
func IsFaultDomainExcluded(c *gin.Context, domain string) bool {
	if c == nil || strings.TrimSpace(domain) == "" {
		return false
	}
	value, _ := c.Get(failedFaultDomainsContextKey)
	domains, _ := value.(map[string]struct{})
	_, excluded := domains[strings.TrimSpace(domain)]
	return excluded
}
