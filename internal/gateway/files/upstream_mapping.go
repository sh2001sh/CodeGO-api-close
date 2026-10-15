package files

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	gatewaySchema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UpstreamTarget struct {
	ChannelID          int
	KeyFingerprint     string
	BaseURLFingerprint string
	Protocol           string
}

var upstreamFileUploads singleflight.Group

func FingerprintCredential(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}

func FingerprintBaseURL(value string) string {
	normalized := strings.TrimRight(strings.TrimSpace(value), "/")
	if parsed, err := url.Parse(normalized); err == nil && parsed.Host != "" {
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		parsed.Host = strings.ToLower(parsed.Host)
		normalized = strings.TrimRight(parsed.String(), "/")
	}
	return FingerprintCredential(normalized)
}

// ResolveUpstreamFile returns the credential-scoped upstream file ID. Calls
// for the same mapping are coalesced so one process performs only one upload.
func ResolveUpstreamFile(file *gatewaySchema.UserFile, target UpstreamTarget, upload func() (string, error)) (string, error) {
	if file == nil || upload == nil || !target.valid() {
		return "", fmt.Errorf("invalid upstream file mapping request")
	}
	key := target.mappingKey(file.ID)
	value, err, _ := upstreamFileUploads.Do(key, func() (any, error) {
		if existing, lookupErr := findMapping(file.ID, target); lookupErr != nil {
			return "", lookupErr
		} else if existing != nil {
			if existing.Status == gatewaySchema.UpstreamFileMappingFailed {
				if time.Since(existing.UpdatedAt) < time.Hour {
					return "", fmt.Errorf("upstream file upload recently failed: %s", existing.LastError)
				}
			} else {
				touchUpstreamMapping(existing)
				return existing.UpstreamFileID, nil
			}
		}
		upstreamID, uploadErr := upload()
		if uploadErr != nil {
			persistMappingResult(file.ID, target, "", gatewaySchema.UpstreamFileMappingFailed, uploadErr.Error())
			return "", uploadErr
		}
		if strings.TrimSpace(upstreamID) == "" {
			uploadErr = fmt.Errorf("upstream file upload returned an empty id")
			persistMappingResult(file.ID, target, "", gatewaySchema.UpstreamFileMappingFailed, uploadErr.Error())
			return "", uploadErr
		}
		if err := persistMappingResult(file.ID, target, upstreamID, gatewaySchema.UpstreamFileMappingReady, ""); err != nil {
			return "", err
		}
		return upstreamID, nil
	})
	if err != nil {
		return "", err
	}
	return value.(string), nil
}

func (target UpstreamTarget) valid() bool {
	return target.ChannelID > 0 && target.KeyFingerprint != "" && target.BaseURLFingerprint != "" && target.Protocol != ""
}

func (target UpstreamTarget) mappingKey(localFileID string) string {
	return fmt.Sprintf("%s:%d:%s:%s:%s", localFileID, target.ChannelID, target.KeyFingerprint, target.BaseURLFingerprint, target.Protocol)
}

func mappingQuery(localFileID string, target UpstreamTarget) *gorm.DB {
	return platformdb.DB.Where(
		"local_file_id = ? AND channel_id = ? AND key_fingerprint = ? AND base_url_fingerprint = ? AND protocol = ?",
		localFileID, target.ChannelID, target.KeyFingerprint, target.BaseURLFingerprint, target.Protocol,
	)
}

func findMapping(localFileID string, target UpstreamTarget) (*gatewaySchema.UpstreamFileMapping, error) {
	var mapping gatewaySchema.UpstreamFileMapping
	err := mappingQuery(localFileID, target).First(&mapping).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &mapping, err
}

func persistMappingResult(localFileID string, target UpstreamTarget, upstreamID, status, lastError string) error {
	now := time.Now().UTC()
	if len(lastError) > 4096 {
		lastError = lastError[:4096]
	}
	mapping := gatewaySchema.UpstreamFileMapping{
		LocalFileID: localFileID, ChannelID: target.ChannelID,
		KeyFingerprint: target.KeyFingerprint, BaseURLFingerprint: target.BaseURLFingerprint,
		Protocol: target.Protocol, UpstreamFileID: upstreamID, Status: status,
		LastError: lastError, LastUsedAt: now,
	}
	return platformdb.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "local_file_id"}, {Name: "channel_id"}, {Name: "key_fingerprint"}, {Name: "base_url_fingerprint"}, {Name: "protocol"}},
		DoUpdates: clause.AssignmentColumns([]string{"upstream_file_id", "status", "last_error", "last_used_at", "updated_at"}),
	}).Create(&mapping).Error
}

func touchUpstreamMapping(mapping *gatewaySchema.UpstreamFileMapping) {
	if mapping == nil || time.Since(mapping.LastUsedAt) < time.Hour {
		return
	}
	now := time.Now().UTC()
	_ = platformdb.DB.Model(mapping).Update("last_used_at", now).Error
}

func deleteUpstreamMappings(db *gorm.DB, localFileID string) error {
	if db == nil || localFileID == "" || !db.Migrator().HasTable(&gatewaySchema.UpstreamFileMapping{}) {
		return nil
	}
	return db.Where("local_file_id = ?", localFileID).Delete(&gatewaySchema.UpstreamFileMapping{}).Error
}
