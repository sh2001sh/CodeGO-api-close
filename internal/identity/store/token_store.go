package store

import (
	"errors"
	"fmt"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/sh2001sh/new-api/constant"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformerrx "github.com/sh2001sh/new-api/internal/platform/errx"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"gorm.io/gorm"
	"strings"
	"time"
)

const searchHardLimit = 100

func ListUserTokens(userID int, startIdx int, pageSize int) ([]*identityschema.Token, error) {
	var tokens []*identityschema.Token
	err := platformdb.DB.Where("user_id = ?", userID).Order("id desc").Limit(pageSize).Offset(startIdx).Find(&tokens).Error
	return tokens, err
}

func CountUserTokens(userID int) (int64, error) {
	var total int64
	err := platformdb.DB.Model(&identityschema.Token{}).Where("user_id = ?", userID).Count(&total).Error
	return total, err
}

func SearchUserTokens(userID int, keyword string, token string, startIdx int, pageSize int) ([]*identityschema.Token, int64, error) {
	if pageSize <= 0 || pageSize > searchHardLimit {
		pageSize = searchHardLimit
	}
	if startIdx < 0 {
		startIdx = 0
	}

	if token != "" {
		token = strings.TrimPrefix(token, "sk-")
	}

	hasFuzzy := strings.Contains(keyword, "%") || strings.Contains(token, "%")
	if hasFuzzy {
		count, err := CountUserTokens(userID)
		if err != nil {
			platformobservability.SysLog("failed to count user tokens: " + err.Error())
			return nil, 0, errors.New("获取令牌数量失败")
		}
		if int(count) > GetMaxUserTokens() {
			return nil, 0, errors.New("令牌数量超过上限，仅允许精确搜索，请勿使用 % 通配符")
		}
	}

	baseQuery := platformdb.DB.Model(&identityschema.Token{}).Where("user_id = ?", userID)
	if keyword != "" {
		keywordPattern, err := sanitizeLikePattern(keyword)
		if err != nil {
			return nil, 0, err
		}
		baseQuery = baseQuery.Where("name LIKE ? ESCAPE '!'", keywordPattern)
	}
	if token != "" {
		tokenPattern, err := sanitizeLikePattern(token)
		if err != nil {
			return nil, 0, err
		}
		baseQuery = baseQuery.Where(tokenKeyColumn()+" LIKE ? ESCAPE '!'", tokenPattern)
	}

	var total int64
	if err := baseQuery.Limit(GetMaxUserTokens()).Count(&total).Error; err != nil {
		platformobservability.SysError("failed to count search tokens: " + err.Error())
		return nil, 0, errors.New("搜索令牌失败")
	}

	var tokens []*identityschema.Token
	if err := baseQuery.Order("id desc").Offset(startIdx).Limit(pageSize).Find(&tokens).Error; err != nil {
		platformobservability.SysError("failed to search tokens: " + err.Error())
		return nil, 0, errors.New("搜索令牌失败")
	}
	return tokens, total, nil
}

func LoadUserTokenByID(userID int, tokenID int) (*identityschema.Token, error) {
	if tokenID == 0 || userID == 0 {
		return nil, errors.New("id 或 userId 为空！")
	}
	token := identityschema.Token{Id: tokenID, UserId: userID}
	err := platformdb.DB.First(&token, "id = ? and user_id = ?", tokenID, userID).Error
	return &token, err
}

func LoadTokenByKey(key string, fromDB bool) (token *identityschema.Token, err error) {
	defer func() {
		if shouldUpdateRedis(fromDB, err) && token != nil {
			tokenCopy := *token
			gopool.Go(func() {
				if err := cacheSetToken(tokenCopy); err != nil {
					platformobservability.SysLog("failed to update user status cache: " + err.Error())
				}
			})
		}
	}()

	if !fromDB && platformcache.RedisEnabled {
		token, err = cacheGetTokenByKey(key)
		if err == nil {
			return token, nil
		}
	}

	fromDB = true
	err = platformdb.DB.Where(tokenKeyColumn()+" = ?", key).First(&token).Error
	return token, err
}

func loadTokenByKeyWithSource(key string, fromDB bool) (*identityschema.Token, string, error) {
	if !fromDB && platformcache.RedisReady() {
		if token, err := cacheGetTokenByKey(key); err == nil {
			return token, "redis", nil
		}
	}
	token, err := loadTokenByKeyFromDB(key)
	return token, "database", err
}

func loadTokenByKeyFromDB(key string) (*identityschema.Token, error) {
	var token *identityschema.Token
	err := platformdb.DB.Where(tokenKeyColumn()+" = ?", key).First(&token).Error
	return token, err
}

func ValidateUserToken(key string) (token *identityschema.Token, err error) {
	if key == "" {
		logTokenAuthFailure("missing_key", key, "none")
		return nil, identitydomain.ErrTokenNotProvided
	}
	var source string
	token, source, err = loadTokenByKeyWithSource(key, false)
	if err == nil {
		if reason := tokenInvalidReason(token); reason != "" {
			if source == "redis" {
				dbToken, dbErr := loadTokenByKeyFromDB(key)
				if dbErr == nil {
					dbReason := tokenInvalidReason(dbToken)
					if dbReason == "" {
						if cacheErr := cacheSetToken(*dbToken); cacheErr != nil {
							platformobservability.SysLog("token_auth_cache_refresh_failed: " + cacheErr.Error())
						}
						platformobservability.SysLog(fmt.Sprintf("token_auth_cache_fallback: key_fp=%s cached_reason=%s result=database_valid", tokenKeyFingerprint(key), reason))
						return dbToken, nil
					}
					logTokenAuthFailure("database_"+dbReason, key, "database")
					persistTokenInvalidStatusIfNeeded(dbToken, dbReason)
					return dbToken, identitydomain.ErrTokenInvalid
				}
				if errors.Is(dbErr, gorm.ErrRecordNotFound) {
					logTokenAuthFailure("not_found", key, "database")
					return nil, identitydomain.ErrTokenInvalid
				}
				logTokenAuthFailure("database_error", key, "database")
				return nil, fmt.Errorf("%w: %v", platformerrx.ErrDatabase, dbErr)
			}
			logTokenAuthFailure(reason, key, source)
			persistTokenInvalidStatusIfNeeded(token, reason)
			return token, identitydomain.ErrTokenInvalid
		}
		return token, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		logTokenAuthFailure("not_found", key, source)
	} else {
		logTokenAuthFailure("database_error", key, source)
	}
	platformobservability.SysLog("ValidateUserToken: failed to get token: " + err.Error())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, identitydomain.ErrTokenInvalid
	}
	return nil, fmt.Errorf("%w: %v", platformerrx.ErrDatabase, err)
}

func tokenInvalidReason(token *identityschema.Token) string {
	if token == nil {
		return "not_found"
	}
	if token.Status == constant.TokenStatusExhausted {
		return "exhausted"
	}
	if token.Status == constant.TokenStatusExpired {
		return "expired"
	}
	if token.Status != constant.TokenStatusEnabled {
		return "disabled"
	}
	if token.ExpiredTime != -1 && token.ExpiredTime < platformruntime.GetTimestamp() {
		return "expired"
	}
	if !token.UnlimitedQuota && token.RemainQuota <= 0 {
		return "exhausted"
	}
	return ""
}

func persistTokenInvalidStatusIfNeeded(token *identityschema.Token, reason string) {
	if platformcache.RedisEnabled || token == nil {
		return
	}
	if reason == "expired" {
		token.Status = constant.TokenStatusExpired
	} else if reason == "exhausted" {
		token.Status = constant.TokenStatusExhausted
	} else {
		return
	}
	if updateErr := updateTokenStatus(token); updateErr != nil {
		platformobservability.SysLog("failed to update token status: " + updateErr.Error())
	}
}

func tokenKeyFingerprint(key string) string {
	fingerprint := platformsecurity.GenerateHMAC(strings.TrimSpace(key))
	if len(fingerprint) > 12 {
		return fingerprint[:12]
	}
	return fingerprint
}

func logTokenAuthFailure(reason, key, source string) {
	platformobservability.SysLog(fmt.Sprintf("token_auth_failed: key_fp=%s reason=%s source=%s", tokenKeyFingerprint(key), reason, source))
}

func DeleteUserToken(userID int, tokenID int) error {
	if tokenID == 0 || userID == 0 {
		return errors.New("id 或 userId 为空！")
	}
	token := identityschema.Token{Id: tokenID, UserId: userID}
	if err := platformdb.DB.Where(token).First(&token).Error; err != nil {
		return err
	}
	return deleteTokenRecord(&token)
}

func BatchDeleteUserTokens(userID int, ids []int) (int, error) {
	if len(ids) == 0 {
		return 0, errors.New("ids 不能为空！")
	}

	tx := platformdb.DB.Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}

	var tokens []identityschema.Token
	if err := tx.Where("user_id = ? AND id IN (?)", userID, ids).Find(&tokens).Error; err != nil {
		tx.Rollback()
		return 0, err
	}

	if err := tx.Where("user_id = ? AND id IN (?)", userID, ids).Delete(&identityschema.Token{}).Error; err != nil {
		tx.Rollback()
		return 0, err
	}

	if err := tx.Commit().Error; err != nil {
		return 0, err
	}

	if platformcache.RedisEnabled {
		tokenCopies := append([]identityschema.Token(nil), tokens...)
		gopool.Go(func() {
			for _, token := range tokenCopies {
				_ = cacheDeleteToken(token.Key)
			}
		})
	}

	return len(tokens), nil
}

func LoadUserTokenKeys(userID int, ids []int) ([]identityschema.Token, error) {
	var tokens []identityschema.Token
	err := platformdb.DB.Select("id", tokenKeyColumn()).
		Where("user_id = ? AND id IN (?)", userID, ids).
		Find(&tokens).Error
	return tokens, err
}

func InvalidateUserTokensCache(userID int) error {
	if !platformcache.RedisEnabled {
		return nil
	}
	if userID <= 0 {
		return errors.New("userId 无效")
	}

	var tokens []identityschema.Token
	if err := platformdb.DB.Unscoped().
		Select("id", tokenKeyColumn()).
		Where("user_id = ?", userID).
		Find(&tokens).Error; err != nil {
		return err
	}

	var firstErr error
	for _, token := range tokens {
		if token.Key == "" {
			continue
		}
		if err := cacheDeleteToken(token.Key); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func sanitizeLikePattern(input string) (string, error) {
	input = strings.ReplaceAll(input, "!", "!!")
	input = strings.ReplaceAll(input, `_`, `!_`)

	if strings.Contains(input, "%%") {
		return "", errors.New("搜索模式中不允许包含连续的 % 通配符")
	}

	count := strings.Count(input, "%")
	if count > 2 {
		return "", errors.New("搜索模式中最多允许包含 2 个 % 通配符")
	}

	if count > 0 {
		stripped := strings.ReplaceAll(input, "%", "")
		if len(stripped) < 2 {
			return "", errors.New("使用模糊搜索时，关键词长度至少为 2 个字符")
		}
		return input, nil
	}
	return input, nil
}

func shouldUpdateRedis(fromDB bool, err error) bool {
	return platformcache.RedisReady() && fromDB && err == nil
}

func tokenKeyColumn() string {
	switch platformdb.DB.Dialector.Name() {
	case "postgres", "sqlite":
		return `"key"`
	case "mysql":
		return "`key`"
	default:
		return "key"
	}
}

func cacheSetToken(token identityschema.Token) error {
	key := platformsecurity.GenerateHMAC(token.Key)
	token.Clean()
	return platformcache.RedisHSetObj(
		fmt.Sprintf("token:%s", key),
		&token,
		time.Duration(platformcache.RedisKeyCacheSeconds())*time.Second,
	)
}

func cacheGetTokenByKey(key string) (*identityschema.Token, error) {
	if !platformcache.RedisReady() {
		return nil, fmt.Errorf("redis is not ready")
	}
	var token identityschema.Token
	err := platformcache.RedisHGetObj(fmt.Sprintf("token:%s", platformsecurity.GenerateHMAC(key)), &token)
	if err != nil {
		return nil, err
	}
	token.Key = key
	return &token, nil
}

func cacheDeleteToken(key string) error {
	return platformcache.RedisDelKey(fmt.Sprintf("token:%s", platformsecurity.GenerateHMAC(key)))
}
