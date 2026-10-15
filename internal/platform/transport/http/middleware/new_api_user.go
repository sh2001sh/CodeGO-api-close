package middleware

import (
	"errors"
	"strconv"
	"strings"

	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

var errInvalidNewAPIUserIdentifier = errors.New("invalid New-Api-User identifier")

// matchesAuthenticatedNewAPIUser accepts either the legacy internal user ID or
// the public external ID, while ensuring it still belongs to the authenticated user.
func matchesAuthenticatedNewAPIUser(rawIdentifier string, authenticatedUserID int) (bool, error) {
	identifier := strings.TrimSpace(rawIdentifier)
	if identifier == "" {
		return false, errInvalidNewAPIUserIdentifier
	}

	externalID := strings.ToUpper(identifier)
	numericID, numericIDError := strconv.Atoi(identifier)
	hasExternalID := isExternalUserIdentifier(externalID)
	if numericIDError != nil && !hasExternalID {
		return false, errInvalidNewAPIUserIdentifier
	}
	if numericIDError == nil && !hasExternalID {
		return numericID == authenticatedUserID, nil
	}

	query := platformdb.DB.Model(&identityschema.User{}).Select("id")
	if numericIDError == nil && hasExternalID {
		query = query.Where("id = ? OR external_id = ?", numericID, externalID)
	} else if numericIDError == nil {
		query = query.Where("id = ?", numericID)
	} else {
		query = query.Where("external_id = ?", externalID)
	}

	var users []identityschema.User
	if err := query.Find(&users).Error; err != nil {
		return false, err
	}
	for _, user := range users {
		if user.Id == authenticatedUserID {
			return true, nil
		}
	}
	return false, nil
}

func isExternalUserIdentifier(identifier string) bool {
	if len(identifier) != identityschema.ExternalUserIDLength {
		return false
	}
	for _, character := range identifier {
		if !strings.ContainsRune("23456789ABCDEFGHJKLMNPQRSTUVWXYZ", character) {
			return false
		}
	}
	return true
}
