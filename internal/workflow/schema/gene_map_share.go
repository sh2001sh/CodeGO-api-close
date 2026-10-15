package workflowschema

import (
	"crypto/rand"
	"math/big"
	"time"

	"gorm.io/gorm"
)

const geneMapShareTokenAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GeneMapShare stores a public snapshot for the API gene-map feature.
type GeneMapShare struct {
	Id           int    `json:"id"`
	UserId       int    `json:"user_id" gorm:"index"`
	ShareToken   string `json:"share_token" gorm:"size:48;uniqueIndex"`
	OwnerLabel   string `json:"owner_label" gorm:"size:96;default:''"`
	Headline     string `json:"headline" gorm:"size:255;default:''"`
	SnapshotJSON string `json:"snapshot_json" gorm:"type:text;not null"`
	CreateTime   int64  `json:"create_time" gorm:"bigint;index"`
	UpdateTime   int64  `json:"update_time" gorm:"bigint"`
}

func (GeneMapShare) TableName() string {
	return "gene_map_shares"
}

func (share *GeneMapShare) BeforeCreate(_ *gorm.DB) error {
	now := time.Now().Unix()
	if share.CreateTime <= 0 {
		share.CreateTime = now
	}
	share.UpdateTime = now
	if share.ShareToken == "" {
		token, err := generateShareToken(24)
		if err != nil {
			return err
		}
		share.ShareToken = token
	}
	return nil
}

func (share *GeneMapShare) BeforeUpdate(_ *gorm.DB) error {
	share.UpdateTime = time.Now().Unix()
	return nil
}

func generateShareToken(length int) (string, error) {
	result := make([]byte, length)
	max := big.NewInt(int64(len(geneMapShareTokenAlphabet)))
	for index := range result {
		value, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		result[index] = geneMapShareTokenAlphabet[value.Int64()]
	}
	return string(result), nil
}
