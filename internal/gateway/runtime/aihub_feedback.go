package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
)

const (
	aiHubFeedbackPathEnv   = "AIHUB_ROUTER_CODEGO_FEEDBACK_PATH"
	aiHubChannelMapEnv     = "AIHUB_ROUTER_CHANNEL_KEY_MAP"
	aiHubFeedbackMaxBytes  = 64 * 1024 * 1024
	aiHubFeedbackFaultHead = "aihub:"
)

var aiHubFeedbackWriteMu sync.Mutex

type codeGoHealthEvent struct {
	Timestamp     time.Time `json:"timestamp"`
	ChannelID     int       `json:"channel_id"`
	KeyID         int64     `json:"key_id,omitempty"`
	GroupID       int64     `json:"group_id,omitempty"`
	Model         string    `json:"model"`
	Success       bool      `json:"success"`
	StatusCode    int       `json:"status_code"`
	ErrorClass    string    `json:"error_class,omitempty"`
	FirstByteMs   int64     `json:"first_byte_ms,omitempty"`
	RequestIDHash string    `json:"request_id_hash,omitempty"`
}

// RecordAIHubHealthSuccess sends an anonymous, local-only health signal to the
// AIHubRouter process. It never blocks the request and is disabled by default.
func RecordAIHubHealthSuccess(c *gin.Context, channelID int, model string, ttft time.Duration) {
	recordAIHubHealthEvent(c, codeGoHealthEvent{
		ChannelID:   channelID,
		Model:       strings.TrimSpace(model),
		Success:     true,
		FirstByteMs: maxDurationMilliseconds(ttft),
	})
}

// RecordAIHubHealthFailure records only transient upstream failures. User
// cancellations and validation/billing errors must not influence AIHub routes.
func RecordAIHubHealthFailure(c *gin.Context, channelID int, model string, statusCode int, failureClass string) {
	if statusCode != 502 && statusCode != 503 && statusCode != 504 && statusCode != 524 {
		return
	}
	recordAIHubHealthEvent(c, codeGoHealthEvent{
		ChannelID:  channelID,
		Model:      strings.TrimSpace(model),
		Success:    false,
		StatusCode: statusCode,
		ErrorClass: strings.TrimSpace(failureClass),
	})
}

func recordAIHubHealthEvent(c *gin.Context, event codeGoHealthEvent) {
	path := strings.TrimSpace(os.Getenv(aiHubFeedbackPathEnv))
	if path == "" || event.ChannelID <= 0 || event.Model == "" {
		return
	}
	event.Timestamp = time.Now().UTC()
	event.KeyID = channelKeyID(event.ChannelID)
	event.GroupID = aihubGroupID(c)
	if c != nil {
		requestID := c.GetString(constant.RequestIdKey)
		if requestID != "" {
			hash := sha256.Sum256([]byte(requestID))
			event.RequestIDHash = hex.EncodeToString(hash[:8])
		}
	}
	go appendAIHubHealthEvent(path, event)
}

func appendAIHubHealthEvent(path string, event codeGoHealthEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	aiHubFeedbackWriteMu.Lock()
	defer aiHubFeedbackWriteMu.Unlock()
	if info, statErr := os.Stat(path); statErr == nil && info.Size() >= aiHubFeedbackMaxBytes {
		_ = os.Rename(path, path+".1")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(payload, '\n'))
}

func channelKeyID(channelID int) int64 {
	raw := strings.TrimSpace(os.Getenv(aiHubChannelMapEnv))
	if raw == "" {
		return 0
	}
	var values map[string]int64
	if json.Unmarshal([]byte(raw), &values) != nil {
		return 0
	}
	return values[strconv.Itoa(channelID)]
}

func aihubGroupID(c *gin.Context) int64 {
	if c == nil {
		return 0
	}
	faultDomain := strings.ToLower(strings.TrimSpace(c.GetString("channel_fault_domain")))
	if !strings.HasPrefix(faultDomain, aiHubFeedbackFaultHead) {
		return 0
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(faultDomain, aiHubFeedbackFaultHead), 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

func maxDurationMilliseconds(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	return value.Milliseconds()
}
