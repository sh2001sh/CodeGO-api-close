package config

import (
	"crypto/tls"
	"time"

	"github.com/google/uuid"
)

var SessionSecret = uuid.New().String()
var CryptoSecret = uuid.New().String()

var DebugEnabled bool
var MemoryCacheEnabled bool
var IsMasterNode bool

// NodeName identifies the current node in audit logs and clustered deployments.
var NodeName string

var TLSInsecureSkipVerify bool
var InsecureTLSConfig = &tls.Config{InsecureSkipVerify: true}

var RequestInterval time.Duration
var SyncFrequency int
var BatchUpdateInterval int

var RelayTimeout int

// RelayResponseHeaderTimeout bounds outbound connection, TLS, and response-header waits
// without limiting a stream after the upstream has started responding.
var RelayResponseHeaderTimeout int

// ImageResponseHeaderTimeout bounds the initial wait for synchronous image
// generation/edit responses. Image providers commonly spend longer preparing
// an image before sending response headers, so text-model first-byte limits do
// not apply to these endpoints.
var ImageResponseHeaderTimeout int
var RelayMaxIdleConns int
var RelayMaxIdleConnsPerHost int
var RelayMaxConcurrentRequests int
var TrustedProxies []string
