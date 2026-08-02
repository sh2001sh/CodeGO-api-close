package app

import (
	"strings"
	"testing"
)

func TestRegistrationVerificationEmailEscapesCode(t *testing.T) {
	content := renderRegistrationVerificationEmail("Code Go", "<123456>", 10)

	if !strings.Contains(content, "&lt;123456&gt;") {
		t.Fatalf("verification code was not HTML escaped: %s", content)
	}
	if !strings.Contains(content, "验证你的邮箱") {
		t.Fatalf("verification email title is missing: %s", content)
	}
}

func TestPasswordResetEmailEscapesAndIncludesFallbackLink(t *testing.T) {
	link := "https://shu26.cfd/user/reset?email=a%2Bb%40example.com&token=abc"
	content := renderPasswordResetEmail("Code Go", link, 10)

	if !strings.Contains(content, "重置密码") {
		t.Fatalf("password reset action is missing: %s", content)
	}
	if !strings.Contains(content, "a%2Bb%40example.com") {
		t.Fatalf("encoded reset link is missing: %s", content)
	}
}
