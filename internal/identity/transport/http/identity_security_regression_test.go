package http

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAdminResetPasskeyEnforcesStoredActorRoleAndAudits(t *testing.T) {
	for _, tc := range []struct {
		name                                            string
		actorRole, targetRole, contextRole, actorStatus int
		allowed                                         bool
	}{
		{"common-cannot-forge-root", constant.RoleCommonUser, constant.RoleCommonUser, constant.RoleRootUser, constant.UserStatusEnabled, false},
		{"admin-cannot-reset-peer", constant.RoleAdminUser, constant.RoleAdminUser, constant.RoleAdminUser, constant.UserStatusEnabled, false},
		{"admin-cannot-reset-root", constant.RoleAdminUser, constant.RoleRootUser, constant.RoleAdminUser, constant.UserStatusEnabled, false},
		{"root-cannot-reset-peer", constant.RoleRootUser, constant.RoleRootUser, constant.RoleRootUser, constant.UserStatusEnabled, false},
		{"disabled-root", constant.RoleRootUser, constant.RoleCommonUser, constant.RoleRootUser, constant.UserStatusDisabled, false},
		{"admin-to-common", constant.RoleAdminUser, constant.RoleCommonUser, constant.RoleAdminUser, constant.UserStatusEnabled, true},
		{"root-to-admin", constant.RoleRootUser, constant.RoleAdminUser, constant.RoleRootUser, constant.UserStatusEnabled, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupDesktopHTTPTestDB(t)
			actor := identityschema.User{Id: 101, Username: "actor", Role: tc.actorRole, Status: tc.actorStatus, AffCode: "ACTOR"}
			target := identityschema.User{Id: 102, Username: "target", Role: tc.targetRole, Status: constant.UserStatusEnabled, AffCode: "TARGET"}
			require.NoError(t, db.Create(&actor).Error)
			require.NoError(t, db.Create(&target).Error)
			require.NoError(t, db.Create(&identitydomain.PasskeyCredential{UserID: target.Id, CredentialID: "key", PublicKey: "public"}).Error)
			ctx, recorder := newAuthenticatedContext(t, http.MethodDelete, "/api/user/102/reset_passkey", nil, actor.Id)
			ctx.Set("role", tc.contextRole)
			ctx.Params = gin.Params{{Key: "id", Value: "102"}}
			AdminResetPasskey(ctx)
			require.Equal(t, tc.allowed, decodeAPIResponse(t, recorder).Success)
			var credentials, logs int64
			require.NoError(t, db.Model(&identitydomain.PasskeyCredential{}).Where("user_id = ?", target.Id).Count(&credentials).Error)
			require.NoError(t, db.Model(&auditschema.Log{}).Where("user_id = ? AND content LIKE ?", actor.Id, "%Passkey%").Count(&logs).Error)
			if tc.allowed {
				require.Zero(t, credentials)
				require.EqualValues(t, 1, logs)
				require.Error(t, identityapp.ResetPasskeyBinding(target.Id, identityapp.AdminActionActor{UserID: actor.Id}), "repeat reset should reject missing credential")
			} else {
				require.EqualValues(t, 1, credentials)
				require.Zero(t, logs)
			}
		})
	}
}

func TestAdminResetPasskeyAuditFailureRollsBackDeletion(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	for _, u := range []identityschema.User{
		{Id: 201, Username: "root", Role: constant.RoleRootUser, Status: constant.UserStatusEnabled, AffCode: "ROOT"},
		{Id: 202, Username: "target", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, AffCode: "TARGET"},
	} {
		require.NoError(t, db.Create(&u).Error)
	}
	require.NoError(t, db.Create(&identitydomain.PasskeyCredential{UserID: 202, CredentialID: "key", PublicKey: "public"}).Error)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("reject_security_audit", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "logs" {
			tx.AddError(errors.New("injected audit failure"))
		}
	}))
	t.Cleanup(func() { db.Callback().Create().Remove("reject_security_audit") })
	require.Error(t, identityapp.ResetPasskeyBinding(202, identityapp.AdminActionActor{UserID: 201}))
	var count int64
	require.NoError(t, db.Model(&identitydomain.PasskeyCredential{}).Where("user_id = ?", 202).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.Error(t, identityapp.ResetPasskeyBinding(201, identityapp.AdminActionActor{UserID: 201}))
}

func TestEmailSecurityRejectsAmbiguousLegacyOwnershipWithoutChangingPasswords(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	for i, email := range []string{"duplicate@example.com", " DUPLICATE@example.com "} {
		require.NoError(t, db.Create(&identityschema.User{Id: 301 + i, Username: fmt.Sprintf("duplicate-%d", i), Email: email, Password: fmt.Sprintf("original-%d", i), Status: constant.UserStatusEnabled, AffCode: fmt.Sprintf("DUPE%d", i)}).Error)
	}
	require.True(t, identitystore.IsEmailTaken("duplicate@example.com"))
	_, err := identitystore.UniqueUserByEmail("duplicate@example.com")
	require.ErrorIs(t, err, identitystore.ErrEmailConflict)
	require.ErrorIs(t, identitystore.ResetUserPasswordByEmail("duplicate@example.com", "Replacement123"), identitystore.ErrEmailConflict)
	require.NoError(t, db.Create(&identityschema.User{Id: 303, Username: "unbound", Status: constant.UserStatusEnabled, AffCode: "UNBOUND"}).Error)
	require.NoError(t, identityapp.RegisterVerificationCodeWithKey("duplicate@example.com", "123456", identityapp.EmailVerificationPurpose))
	err = identityapp.BindEmail(303, "duplicate@example.com", "123456")
	require.Error(t, err)
	var users []identityschema.User
	require.NoError(t, db.Order("id").Find(&users).Error)
	require.Equal(t, "original-0", users[0].Password)
	require.Equal(t, "original-1", users[1].Password)
	require.Empty(t, users[2].Email)
	called := false
	restore := stubIdentitySendEmail(func(string, string, string) error { called = true; return nil })
	defer restore()
	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/reset_password?email=duplicate@example.com", nil, 0)
	SendPasswordResetEmail(ctx)
	require.True(t, decodeAPIResponse(t, recorder).Success)
	require.False(t, called, "ambiguous email must not receive a password-reset link")
}

func TestEmailSecurityCanonicalBindingAndSingleAccountReset(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Create(&identityschema.User{Id: 401 + i, Username: fmt.Sprintf("email-%d", i), Password: "original", Status: constant.UserStatusEnabled, AffCode: fmt.Sprintf("MAIL%d", i)}).Error)
	}
	require.NoError(t, identityapp.RegisterVerificationCodeWithKey("owner@example.com", "123456", identityapp.EmailVerificationPurpose))
	require.NoError(t, identityapp.BindEmail(401, " Owner@Example.com ", "123456"))
	require.True(t, identitystore.IsEmailTaken("OWNER@example.com"))
	require.Error(t, identityapp.BindEmail(402, "owner@example.com", "123456"), "replayed verification code accepted")
	require.Error(t, identitystore.BindUserEmail(402, "owner@example.com"))
	require.NoError(t, identitystore.ResetUserPasswordByEmail("OWNER@EXAMPLE.COM", "Replacement123"))
	var owner, other identityschema.User
	require.NoError(t, db.First(&owner, 401).Error)
	require.NoError(t, db.First(&other, 402).Error)
	require.Equal(t, "owner@example.com", owner.Email)
	require.True(t, platformsecurity.ValidatePasswordAndHash("Replacement123", owner.Password))
	loggedIn, err := identitystore.AuthenticateUserCredentials(" OWNER@EXAMPLE.COM ", "Replacement123")
	require.NoError(t, err)
	require.Equal(t, owner.Id, loggedIn.Id)
	require.Equal(t, "original", other.Password)
	stale := owner
	require.NoError(t, identitystore.BindUserEmail(owner.Id, "replacement@example.com"))
	stale.DisplayName = "updated profile"
	require.NoError(t, identitystore.UpdateUser(&stale, false))
	require.Equal(t, "replacement@example.com", stale.Email, "generic update restored a stale email binding")
	owner = stale
	require.NoError(t, db.Delete(&owner).Error)
	require.True(t, identitystore.IsEmailTaken(owner.Email), "deleted owner must not free ambiguous identity")
	require.Error(t, identitystore.ResetUserPasswordByEmail(owner.Email, "Otherpass123"))
	require.NoError(t, db.Migrator().DropTable(&identityschema.User{}))
	require.True(t, identitystore.IsEmailTaken("free@example.com"), "database failure must fail closed")
	require.Error(t, identitystore.BindUserEmail(402, "free@example.com"))
}

func TestEmailSecurityAmbiguousEmailLoginPreservesUsernameLogin(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	hash, err := platformsecurity.Password2Hash("Loginpass123")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Create(&identityschema.User{Id: 501 + i, Username: fmt.Sprintf("legacy-login-%d", i), Email: "duplicate-login@example.com", Password: hash, Status: constant.UserStatusEnabled, AffCode: fmt.Sprintf("LOGIN%d", i)}).Error)
	}
	_, err = identitystore.AuthenticateUserCredentials("DUPLICATE-LOGIN@example.com", "Loginpass123")
	require.Error(t, err)
	for i := 0; i < 2; i++ {
		user, err := identitystore.AuthenticateUserCredentials(fmt.Sprintf("legacy-login-%d", i), "Loginpass123")
		require.NoError(t, err)
		require.Equal(t, 501+i, user.Id)
	}
	_, err = identitystore.AuthenticateUserCredentials("legacy-login-0", "wrong-password")
	require.Error(t, err)
}
