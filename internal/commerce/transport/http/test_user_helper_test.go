package http

import (
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
)

func loadUserByIDForTest(userID int, selectAll bool) (*identityschema.User, error) {
	return identitystore.LoadUserByID(userID, selectAll)
}
