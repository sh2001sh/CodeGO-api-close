package runtime

import "time"

const (
	// GPTInitialFailureRetryWindow bounds retries before a GPT response starts.
	GPTInitialFailureRetryWindow = 30 * time.Second

	// GPTNonLongContextFirstByteTimeout leaves time to route a first-header
	// failure to a healthy fallback within GPTInitialFailureRetryWindow.
	GPTNonLongContextFirstByteTimeout = 20 * time.Second
)
