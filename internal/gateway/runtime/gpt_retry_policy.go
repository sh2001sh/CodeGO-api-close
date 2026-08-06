package runtime

import "time"

const (
	// GPTInitialFailureRetryWindow bounds retries before a GPT response starts.
	GPTInitialFailureRetryWindow = 30 * time.Second
)
