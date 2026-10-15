package store

import (
	"github.com/sh2001sh/new-api/constant"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
)

func LoadModelSupportedEndpointTypes(modelName string) []constant.EndpointType {
	return gatewaystore.LoadModelSupportedEndpointTypes(modelName)
}
