package identitydto

import "encoding/json"

// KeyJSON applies the same retained key representation inside desktop
// compound responses as the ordinary token management endpoints.
func KeyJSON(raw json.RawMessage) (json.RawMessage, error) { return keyOutput(raw) }
