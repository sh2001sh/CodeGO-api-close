package app

import (
	"errors"

	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
)

var ErrInvalidRequestBody = errors.New("invalid request body")

type EmailBindRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

// BindEmail verifies the email code and persists the user's bound email address.
func BindEmail(userID int, email string, code string) error {
	email = identitystore.NormalizeEmail(email)
	if err := validateVerificationEmail(email); err != nil {
		return err
	}
	if !VerifyCodeWithKey(email, code, EmailVerificationPurpose) {
		return ErrVerificationCodeInvalid
	}

	return identitystore.BindUserEmail(userID, email)
}
