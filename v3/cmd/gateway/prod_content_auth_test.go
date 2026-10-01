package main

import (
	"encoding/base64"
	"testing"
)

func TestBrowserContentAuthorizationRejectsInvalidSessionConfiguration(t *testing.T) {
	for _, value := range []string{"", "invalid-base64", base64.StdEncoding.EncodeToString([]byte("short"))} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("V3_SESSION_SECRET", value)
			if _, err := newContentAuthorizer(nil, runtimeLogger()); err == nil {
				t.Fatal("invalid session configuration silently disabled browser authentication")
			}
		})
	}
}
