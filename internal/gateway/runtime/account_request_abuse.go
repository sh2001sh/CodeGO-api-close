package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/sh2001sh/new-api/constant"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Three complete consecutive minutes, per account/model across all tokens.
// Cache support is established by a real positive hit on that channel/model
// within 24h; an unverified provider is never classified as zero-cache abuse.
var accountSampleScript = redis.NewScript(`
local now = redis.call('TIME')
local minute = math.floor(tonumber(now[1])/60)
local key = KEYS[1]..':'..minute
redis.call('HINCRBY',key,'n',1)
redis.call('HINCRBY',key,'short',tonumber(ARGV[1]) <= 2048 and 1 or 0)
redis.call('HINCRBY',key,'input',ARGV[1])
redis.call('HINCRBY',key,'cache',ARGV[2])
redis.call('HINCRBY',key,'unknown',tonumber(ARGV[3]) == 1 and 0 or 1)
redis.call('EXPIRE',key,300)
local evidence = {}
for i=3,1,-1 do
 local v=redis.call('HMGET',KEYS[1]..':'..(minute-i),'n','short','input','cache','unknown')
 local n=tonumber(v[1]) or 0
 local short=tonumber(v[2]) or 0
 local input=tonumber(v[3]) or 0
 local cache=tonumber(v[4]) or 0
 local unknown=tonumber(v[5]) or 0
 if n<60 or short*10<n*9 or input<=0 or cache*100>=input*5 or unknown>0 then return '' end
 table.insert(evidence,{minute=minute-i,requests=n,short=short,input=input,cache=cache})
end
return cjson.encode({window_end=minute*60,minutes=evidence})
`)

var accountRPMScript = redis.NewScript(`
local now=redis.call('TIME')
local ms=tonumber(now[1])*1000+math.floor(tonumber(now[2])/1000)
redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',ms-60000)
if redis.call('ZCARD',KEYS[1])>=10 then return 0 end
redis.call('ZADD',KEYS[1],ms,ARGV[1])
redis.call('PEXPIRE',KEYS[1],60000)
return 1
`)

func abuseStateKey(userID int) string { return fmt.Sprintf("gateway:request-abuse:state:%d", userID) }

func loadAccountAbuseState(ctx context.Context, userID int) (gatewayschema.AccountRequestAbuseState, error) {
	var state gatewayschema.AccountRequestAbuseState
	key := abuseStateKey(userID)
	value, err := platformcache.RDB.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		if platformdb.DB == nil {
			return state, errors.New("account restriction database unavailable")
		}
		if err = platformdb.DB.WithContext(ctx).Where("user_id = ?", userID).Find(&state).Error; err != nil {
			return state, err
		}
		value, err = json.Marshal(state)
		if err != nil {
			return state, err
		}
		if err = platformcache.RDB.SetNX(ctx, key, value, time.Minute).Err(); err != nil {
			return state, err
		}
		value, err = platformcache.RDB.Get(ctx, key).Bytes()
	}
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(value, &state)
	return state, err
}

func checkAccountRequestAbuse(parent context.Context, userID int) (bool, ChannelConcurrencyAdmission) {
	if !platformconfig.RequestAbuseGuardEnabled || userID <= 0 {
		return false, ChannelConcurrencyAdmitted
	}
	if !platformcache.RedisReady() {
		return false, ChannelConcurrencyDependencyUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	state, err := loadAccountAbuseState(ctx, userID)
	if err != nil {
		reportAccountAbuseError(err)
		return false, ChannelConcurrencyDependencyUnavailable
	}
	if state.Blocked {
		return false, AccountRequestDisabled
	}
	if state.RestrictedUntil <= time.Now().Unix() {
		return false, ChannelConcurrencyAdmitted
	}
	allowed, err := accountRPMScript.Run(ctx, platformcache.RDB, []string{fmt.Sprintf("gateway:request-abuse:rpm:%d", userID)}, channelConcurrencyLeaseToken()).Int()
	if err != nil {
		reportAccountAbuseError(err)
		return true, ChannelConcurrencyDependencyUnavailable
	}
	if allowed != 1 {
		return true, AccountRequestRPMReached
	}
	return true, ChannelConcurrencyAdmitted
}

// RecordAccountRequestSample runs after settlement, outside the response path.
// It keeps bounded five-minute Redis counters, never scans the usage log table.
func RecordAccountRequestSample(userID, channelID int, model string, input, cache int64) {
	if !platformconfig.RequestAbuseGuardEnabled || userID <= 0 || input <= 0 || cache < 0 || cache > input {
		return
	}
	if !platformcache.RedisReady() {
		reportAccountAbuseError(errors.New("sample Redis unavailable"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(model)))
	supportKey := fmt.Sprintf("gateway:request-abuse:cache-support:%d:%s", channelID, hash)
	known := int64(0)
	if cache > 0 {
		if err := platformcache.RDB.Set(ctx, supportKey, "1", 24*time.Hour).Err(); err != nil {
			reportAccountAbuseError(err)
			return
		}
		known = 1
	} else {
		var err error
		known, err = platformcache.RDB.Exists(ctx, supportKey).Result()
		if err != nil {
			reportAccountAbuseError(err)
			return
		}
	}
	base := fmt.Sprintf("gateway:request-abuse:samples:{%d}:%s", userID, hash)
	evidence, err := accountSampleScript.Run(ctx, platformcache.RDB, []string{base}, input, cache, known).Text()
	if err != nil {
		reportAccountAbuseError(err)
		return
	}
	if evidence == "" {
		return
	}
	var window struct {
		WindowEnd int64 `json:"window_end"`
	}
	if err = json.Unmarshal([]byte(evidence), &window); err != nil {
		reportAccountAbuseError(err)
		return
	}
	lock := fmt.Sprintf("gateway:request-abuse:action:%d:%d", userID, window.WindowEnd)
	acquired, err := platformcache.RDB.SetNX(ctx, lock, "1", time.Minute).Result()
	if err != nil {
		reportAccountAbuseError(err)
		return
	}
	if !acquired {
		return
	}
	_, err = ApplyAccountRequestAbuse(userID, window.WindowEnd, evidence)
	if err != nil {
		reportAccountAbuseError(err)
		if deleteErr := platformcache.RDB.Del(ctx, lock).Err(); deleteErr != nil {
			reportAccountAbuseError(deleteErr)
		}
	}
}

// ApplyAccountRequestAbuse persists one episode and synchronizes authentication
// caches. WindowEnd identifies the end of the three-minute evidence window.
func ApplyAccountRequestAbuse(userID int, windowEnd int64, evidence string) (gatewayschema.AccountRequestAbuseState, error) {
	var state gatewayschema.AccountRequestAbuseState
	now := time.Now().Unix()
	if userID <= 0 || windowEnd <= 0 || windowEnd > now {
		return state, errors.New("invalid account restriction window")
	}
	if platformdb.DB == nil {
		return state, errors.New("account restriction database unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	changed := false
	err := platformdb.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user identityschema.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "role", "status").First(&user, userID).Error; err != nil {
			return err
		}
		if user.Role >= constant.RoleAdminUser {
			return nil
		}
		initial := gatewayschema.AccountRequestAbuseState{UserID: userID}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&state, "user_id = ?", userID).Error; err != nil {
			return err
		}
		// Require the entire new observation window to start after recovery.
		if state.Blocked || windowEnd <= state.LastWindowEnd || (state.Strikes > 0 && (now < state.RestrictedUntil || windowEnd-180 < state.RestrictedUntil)) {
			return nil
		}
		state.Strikes++
		state.LastWindowEnd = windowEnd
		state.Evidence = evidence
		if state.Strikes == 1 {
			state.RestrictedUntil = now + 86400
		} else {
			state.Blocked = true
			if err := tx.Model(&identityschema.User{}).Where("id = ?", userID).Update("status", constant.UserStatusDisabled).Error; err != nil {
				return err
			}
		}
		changed = true
		return tx.Save(&state).Error
	})
	if err != nil {
		return state, err
	}
	if state.UserID == 0 {
		return state, nil
	}
	if !platformcache.RedisReady() {
		return state, errors.New("restriction saved but Redis unavailable")
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return state, err
	}
	if err = platformcache.RDB.Set(ctx, abuseStateKey(userID), encoded, time.Minute).Err(); err != nil {
		return state, err
	}
	if state.Blocked {
		if err = identitystore.InvalidateUserCache(userID); err != nil {
			return state, err
		}
		if err = identitystore.InvalidateUserTokensCache(userID); err != nil {
			return state, err
		}
	}
	if changed {
		platformobservability.SysLog(fmt.Sprintf("account request abuse action user=%d strikes=%d restricted_until=%d blocked=%t", userID, state.Strikes, state.RestrictedUntil, state.Blocked))
	}
	return state, nil
}

func reportAccountAbuseError(err error) {
	platformobservability.SysError("account request abuse guard: " + err.Error())
}
