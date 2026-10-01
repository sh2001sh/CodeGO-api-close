package gateway

import (
	"encoding/json"
	"net/http"
)

func protocolErrorJSON(protocol Protocol, status int, typ, code, message string) (string, []byte) {
	switch protocol {
	case ProtocolAnthropic:
		body := struct {
			Type  string `json:"type"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}{Type: "error"}
		body.Error.Type, body.Error.Message = typ, message
		data, _ := json.Marshal(body)
		return "error", data
	case ProtocolGemini:
		body := struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}{}
		body.Error.Code, body.Error.Message = status, message
		body.Error.Status = googleStatus(status)
		data, _ := json.Marshal(body)
		return "", data
	case ProtocolResponses:
		data, _ := json.Marshal(struct {
			Type    string  `json:"type"`
			Code    string  `json:"code"`
			Message string  `json:"message"`
			Param   *string `json:"param"`
		}{Type: "error", Code: code, Message: message})
		return "error", data
	default:
		return "", errorJSON(typ, code, message)
	}
}

func googleStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "INVALID_ARGUMENT"
	case http.StatusUnauthorized:
		return "UNAUTHENTICATED"
	case http.StatusForbidden:
		return "PERMISSION_DENIED"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusTooManyRequests:
		return "RESOURCE_EXHAUSTED"
	case http.StatusGatewayTimeout:
		return "DEADLINE_EXCEEDED"
	default:
		return "UNAVAILABLE"
	}
}

func writeProtocolError(w http.ResponseWriter, protocol Protocol, status int, typ, code, message string) {
	if protocol == ProtocolResponses || protocol == ProtocolOpenAIChat || protocol == 0 {
		writeJSONError(w, status, typ, code, message)
		return
	}
	_, body := protocolErrorJSON(protocol, status, typ, code, message)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
