package store

import (
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

func TestAdjustTokenQuotaDoesNotOverdrawToken(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&identityschema.Token{}); err != nil {
		t.Fatalf("migrate token: %v", err)
	}
	originalDB := platformdb.DB
	platformdb.DB = database
	t.Cleanup(func() { platformdb.DB = originalDB })

	token := identityschema.Token{Id: 1, UserId: 1, Key: "token-quota-test", RemainQuota: 100}
	if err := database.Create(&token).Error; err != nil {
		t.Fatalf("create token: %v", err)
	}

	if err := AdjustTokenQuota(token.Id, token.Key, 75); err != nil {
		t.Fatalf("initial decrease: %v", err)
	}
	if err := AdjustTokenQuota(token.Id, token.Key, 50); !errors.Is(err, ErrTokenQuotaInsufficient) {
		t.Fatalf("expected insufficient quota error, got: %v", err)
	}

	var stored identityschema.Token
	if err := database.First(&stored, token.Id).Error; err != nil {
		t.Fatalf("reload token: %v", err)
	}
	if stored.RemainQuota != 25 || stored.UsedQuota != 75 {
		t.Fatalf("token quota changed after rejected decrease: remain=%d used=%d", stored.RemainQuota, stored.UsedQuota)
	}
}
