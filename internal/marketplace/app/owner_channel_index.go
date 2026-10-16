package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

// Only overlapping reads are shared. No result survives the in-flight load and
// the key includes both the database handle and authenticated owner identity.
var ownerChannelIndexLoads singleflight.Group

// OwnerChannelIndexItem contains only the fields used by global inventory search,
// income ranking and channel selectors. Full probe results and prices are fetched
// for the visible page through ListOwnerChannelsByIDs.
type OwnerChannelIndexItem struct {
	ID                     string     `json:"id"`
	SystemDisplayName      string     `json:"system_display_name"`
	CustomName             string     `json:"custom_name,omitempty"`
	SubmittedSourceLabel   string     `json:"submitted_source_label"`
	SearchText             string     `json:"search_text"`
	Multiplier             float64    `json:"multiplier"`
	LifecycleStatus        string     `json:"lifecycle_status"`
	VerificationStatus     string     `json:"verification_status"`
	ConnectivityTestStatus string     `json:"connectivity_test_status"`
	NeedsAttention         bool       `json:"needs_attention"`
	Checking               bool       `json:"checking"`
	TotalIncome            int64      `json:"total_income"`
	DeletedAt              *time.Time `json:"deleted_at,omitempty"`
}

func ListOwnerChannelIndex(ownerUserID int) ([]OwnerChannelIndexItem, error) {
	if ownerUserID <= 0 {
		return nil, errors.New("用户未登录")
	}
	db := platformdb.DB
	value, err, _ := ownerChannelIndexLoads.Do(fmt.Sprintf("%p:%d", db, ownerUserID), func() (any, error) {
		return loadOwnerChannelIndex(db, ownerUserID)
	})
	if err != nil {
		return nil, err
	}
	items := slices.Clone(value.([]OwnerChannelIndexItem))
	for index := range items {
		if items[index].DeletedAt != nil {
			deletedAt := *items[index].DeletedAt
			items[index].DeletedAt = &deletedAt
		}
	}
	return items, nil
}

func loadOwnerChannelIndex(db *gorm.DB, ownerUserID int) ([]OwnerChannelIndexItem, error) {
	type indexRow struct {
		ID, SubmittedSourceLabel, SourceLabelStatus     string
		DeclaredModels, ModelVerificationResults        string
		ConnectivityTestStatus, CustomName, Description string
		Tags, LifecycleStatus, VerificationStatus       string
		Multiplier                                      float64
		TotalIncome                                     int64
		DeletedAt                                       *time.Time
	}
	channels := marketplaceschema.Channel{}.TableName()
	groups := marketplaceschema.Group{}.TableName()
	settlements := marketplaceschema.Settlement{}.TableName()
	rows, err := db.Table(channels+" AS c").
		Select(`c.id, COALESCE(c.submitted_source_label, ''), COALESCE(c.source_label_status, ''),
			COALESCE(c.declared_models, ''), COALESCE(c.model_verification_results, ''), COALESCE(c.connectivity_test_status, ''),
			COALESCE(g.custom_name, ''), COALESCE(g.description, ''), COALESCE(g.tags, ''), g.lifecycle_status, g.verification_status,
			g.multiplier, c.deleted_at, COALESCE(earnings.total_income, 0) AS total_income`).
		Joins("JOIN "+groups+" AS g ON g.channel_id = c.id AND g.owner_user_id = c.owner_user_id").
		Joins("LEFT JOIN (SELECT group_id, SUM(owner_net_amount) AS total_income FROM "+settlements+
			" WHERE owner_user_id = ? GROUP BY group_id) earnings ON earnings.group_id = g.id", ownerUserID).
		Where("c.owner_user_id = ?", ownerUserID).
		Order("c.deleted_at asc, c.created_at desc, c.id asc").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]OwnerChannelIndexItem, 0)
	for rows.Next() {
		var row indexRow
		if err := rows.Scan(&row.ID, &row.SubmittedSourceLabel, &row.SourceLabelStatus,
			&row.DeclaredModels, &row.ModelVerificationResults, &row.ConnectivityTestStatus,
			&row.CustomName, &row.Description, &row.Tags, &row.LifecycleStatus, &row.VerificationStatus,
			&row.Multiplier, &row.DeletedAt, &row.TotalIncome); err != nil {
			return nil, err
		}
		failedModels := false
		// Unmarshal only status bits. Probe error text can be several KiB per model;
		// it is neither retained nor copied into the global index.
		var statuses []struct {
			Status string `json:"status"`
			Listed bool   `json:"listed"`
		}
		if err := json.Unmarshal([]byte(row.ModelVerificationResults), &statuses); err == nil {
			for _, result := range statuses {
				if result.Status == "failed" || !result.Listed {
					failedModels = true
					break
				}
			}
		}
		name := marketplaceDisplayName(row.SubmittedSourceLabel, row.Multiplier, row.ID)
		search := []string{row.ID, name, row.CustomName, row.Description, row.SubmittedSourceLabel}
		search = append(search, decodeModels(row.Tags)...)
		search = append(search, decodeModels(row.DeclaredModels)...)
		items = append(items, OwnerChannelIndexItem{
			ID: row.ID, SystemDisplayName: name, CustomName: row.CustomName,
			SubmittedSourceLabel: row.SubmittedSourceLabel,
			SearchText:           strings.ToLower(strings.Join(search, "\n")),
			Multiplier:           row.Multiplier, LifecycleStatus: row.LifecycleStatus,
			VerificationStatus: row.VerificationStatus, ConnectivityTestStatus: row.ConnectivityTestStatus,
			NeedsAttention: row.LifecycleStatus == "degraded" || row.LifecycleStatus == "suspended" ||
				row.LifecycleStatus == "pending_review" || row.ConnectivityTestStatus == "failed" ||
				failedModels || row.SourceLabelStatus == "rejected",
			Checking: row.LifecycleStatus == "verifying" || row.LifecycleStatus == "queued" ||
				row.LifecycleStatus == "running" || row.ConnectivityTestStatus == "queued" ||
				row.ConnectivityTestStatus == "running",
			TotalIncome: row.TotalIncome, DeletedAt: row.DeletedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
