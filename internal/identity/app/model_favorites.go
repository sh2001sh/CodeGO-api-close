package app

import (
	"errors"
	"sort"

	"github.com/sh2001sh/new-api/internal/identity/domain"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
)

func GetFavoriteModelIDs(userID int) ([]int, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	user, err := identitystore.LoadUserByID(userID, true)
	if err != nil {
		return nil, err
	}
	ids := append([]int(nil), domain.GetSetting(user).FavoriteModelIDs...)
	sort.Ints(ids)
	return ids, nil
}

func SetFavoriteModel(userID, modelID int, favorite bool) ([]int, error) {
	if userID <= 0 || modelID <= 0 {
		return nil, errors.New("invalid favorite model request")
	}
	user, err := identitystore.LoadUserByID(userID, true)
	if err != nil {
		return nil, err
	}
	settings := domain.GetSetting(user)
	ids := make(map[int]struct{}, len(settings.FavoriteModelIDs)+1)
	for _, id := range settings.FavoriteModelIDs {
		if id > 0 {
			ids[id] = struct{}{}
		}
	}
	if favorite {
		ids[modelID] = struct{}{}
	} else {
		delete(ids, modelID)
	}
	settings.FavoriteModelIDs = settings.FavoriteModelIDs[:0]
	for id := range ids {
		settings.FavoriteModelIDs = append(settings.FavoriteModelIDs, id)
	}
	sort.Ints(settings.FavoriteModelIDs)
	domain.SetSetting(user, settings)
	if err := identitystore.UpdateUser(user, false); err != nil {
		return nil, err
	}
	return settings.FavoriteModelIDs, nil
}
