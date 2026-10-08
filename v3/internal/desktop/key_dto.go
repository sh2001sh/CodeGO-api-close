package desktop

import (
	"encoding/json"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/internal/legacy/identitydto"
)

func keyDTO(r *http.Request, k identity.KeyRecord) (any, error) {
	if identitydto.IsV3(r) {
		return k, nil
	}
	b, err := json.Marshal(k)
	if err != nil {
		return nil, err
	}
	return identitydto.KeyJSON(b)
}
