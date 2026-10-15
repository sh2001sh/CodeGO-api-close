package notifyx

import (
	"strings"

	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
)

func PaymentReturnURL(suffix string) string {
	base := strings.TrimRight(platformconfig.ServerAddress, "/")
	return base + platformconfig.ThemeAwarePath(suffix)
}
