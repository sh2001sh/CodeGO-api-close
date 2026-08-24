package app

import (
	"testing"

	"github.com/glebarez/sqlite"
	communityschema "github.com/sh2001sh/new-api/internal/community/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestNormalizeGitHubResourceURL(t *testing.T) {
	normalized, repository, err := NormalizeGitHubResourceURL(
		"https://github.com/sh2001sh/new-api/tree/main/scripts",
	)
	require.NoError(t, err)
	require.Equal(t, "https://github.com/sh2001sh/new-api/tree/main/scripts", normalized)
	require.Equal(t, "https://github.com/sh2001sh/new-api", repository)

	normalized, repository, err = NormalizeGitHubResourceURL(
		"https://github.com/sh2001sh/new-api.git",
	)
	require.NoError(t, err)
	require.Equal(t, "https://github.com/sh2001sh/new-api", normalized)
	require.Equal(t, "https://github.com/sh2001sh/new-api", repository)
}

func TestDeleteResourceRemovesOnlySelectedResource(t *testing.T) {
	originalDB := platformdb.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	t.Cleanup(func() { platformdb.DB = originalDB })
	platformdb.DB = db
	require.NoError(t, db.AutoMigrate(&communityschema.Resource{}))

	first := &communityschema.Resource{Title: "first", Description: "first", Category: "tool", GitHubURL: "https://github.com/a/first", RepositoryURL: "https://github.com/a/first", SubmittedBy: 1, SubmitterName: "admin", Status: communityschema.ResourceStatusApproved}
	second := &communityschema.Resource{Title: "second", Description: "second", Category: "tool", GitHubURL: "https://github.com/a/second", RepositoryURL: "https://github.com/a/second", SubmittedBy: 1, SubmitterName: "admin", Status: communityschema.ResourceStatusApproved}
	require.NoError(t, db.Create(first).Error)
	require.NoError(t, db.Create(second).Error)

	require.NoError(t, DeleteResource(first.ID))
	require.ErrorIs(t, DeleteResource(first.ID), ErrResourceNotFound)
	var remaining int64
	require.NoError(t, db.Model(&communityschema.Resource{}).Count(&remaining).Error)
	require.Equal(t, int64(1), remaining)
}

func TestNormalizeGitHubResourceURLRejectsLookalikeHosts(t *testing.T) {
	_, _, err := NormalizeGitHubResourceURL("https://github.com.evil.example/owner/repo")
	require.Error(t, err)

	_, _, err = NormalizeGitHubResourceURL("https://user@github.com/owner/repo")
	require.Error(t, err)
}
