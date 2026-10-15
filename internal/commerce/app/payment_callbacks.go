package app

import (
	commercestore "github.com/sh2001sh/new-api/internal/commerce/paymentsettings"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
)

func CallbackAddress() string {
	if commercestore.CustomCallbackAddress == "" {
		return platformconfig.ServerAddress
	}
	return commercestore.CustomCallbackAddress
}
