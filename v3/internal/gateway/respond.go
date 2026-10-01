package gateway

import (
	"encoding/json"
	"net/http"
)

type errorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// errorJSON renders an OpenAI-compatible error object.
func errorJSON(typ, code, message string) []byte {
	var body errorBody
	body.Error.Message, body.Error.Type, body.Error.Code = message, typ, code
	b, _ := json.Marshal(body) // cannot fail: only string fields
	return b
}

func writeJSONError(w http.ResponseWriter, status int, typ, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(errorJSON(typ, code, message))
}
