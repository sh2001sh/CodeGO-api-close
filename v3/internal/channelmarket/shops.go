package channelmarket

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/community"
)

type ShopReference struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var (
	ErrInvalidShopName        = fmt.Errorf("%w: invalid shop name", ErrInvalidName)
	ErrInvalidShopDescription = fmt.Errorf("%w: invalid shop description", ErrInvalidRemark)
)

type Shop struct {
	Rating               community.PublicRating `json:"rating"`
	ID                   string                 `json:"id"`
	Name                 string                 `json:"name"`
	Description          string                 `json:"description"`
	GroupCount           int                    `json:"group_count"`
	Models               []string               `json:"declared_models"`
	Tags                 []string               `json:"tags"`
	ReviewStatus         string                 `json:"review_status,omitempty"`
	SubmittedName        string                 `json:"submitted_name,omitempty"`
	SubmittedDescription *string                `json:"submitted_description,omitempty"`
	ReviewReason         string                 `json:"review_reason,omitempty"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
	owner                int64
}

type ShopDetail struct {
	Shop       Shop              `json:"shop"`
	Groups     []ChannelView     `json:"groups"`
	Pagination *BrowsePagination `json:"pagination,omitempty"`
}

type ShopUpdate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

const shopColumns = `s.id::text,s.owner_user_id,s.name,s.description,s.submitted_name,s.submitted_description,s.review_status,s.review_reason,s.created_at,s.updated_at`

func scanShop(row scanner, private bool) (Shop, error) {
	var shop Shop
	var description string
	err := row.Scan(&shop.ID, &shop.owner, &shop.Name, &shop.Description, &shop.SubmittedName, &description, &shop.ReviewStatus, &shop.ReviewReason, &shop.CreatedAt, &shop.UpdatedAt)
	if shop.Name == "" {
		shop.Name = fmt.Sprintf("渠道店铺 #%s", shop.ID)
	}
	shop.Models, shop.Tags = []string{}, []string{}
	if private {
		shop.SubmittedDescription = &description
	} else {
		shop.SubmittedName, shop.ReviewStatus, shop.ReviewReason = "", "", ""
	}
	return shop, err
}

func ensureShopTx(ctx context.Context, tx pgx.Tx, owner int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO v3_channelmarket.shops(owner_user_id) VALUES($1) ON CONFLICT(owner_user_id) DO NOTHING`, owner)
	return err
}

// attachShops batches owner lookups without exposing the private account ID in
// the shop reference. Legacy ChannelView ownership fields retain their contract.
func (s *Service) attachShops(ctx context.Context, groups []ChannelView) error {
	if len(groups) == 0 {
		return nil
	}
	owners := make([]int64, 0, len(groups))
	for _, group := range groups {
		owners = append(owners, group.OwnerUserID)
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text,owner_user_id,name FROM v3_channelmarket.shops WHERE owner_user_id=ANY($1)`, owners)
	if err != nil {
		return err
	}
	defer rows.Close()
	refs := map[int64]ShopReference{}
	for rows.Next() {
		var ref ShopReference
		var owner int64
		if err = rows.Scan(&ref.ID, &owner, &ref.Name); err != nil {
			return err
		}
		if ref.Name == "" {
			ref.Name = fmt.Sprintf("渠道店铺 #%s", ref.ID)
		}
		refs[owner] = ref
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range groups {
		if ref, ok := refs[groups[i].OwnerUserID]; ok {
			groups[i].Shop = &ref
		}
	}
	rows.Close()
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	ratings, err := community.ChannelSummaries(ctx, s.pool, ids)
	if err != nil {
		return err
	}
	for i := range groups {
		groups[i].Rating = ratings[groups[i].ID]
	}
	return nil
}

func (s *Service) MyShop(ctx context.Context, actor Actor) (Shop, error) {
	var shop Shop
	if actor.UserID <= 0 {
		return shop, ErrInvalid
	}
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		var ownerExists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_identity.users u WHERE u.id=$1 AND u.status='active' AND u.deleted_at IS NULL AND EXISTS(SELECT 1 FROM v3_channelmarket.groups g WHERE g.owner_user_id=u.id AND g.deleted_at IS NULL))`, actor.UserID).Scan(&ownerExists); err != nil {
			return err
		}
		if !ownerExists {
			return ErrNotFound
		}
		if err := ensureShopTx(ctx, tx, actor.UserID); err != nil {
			return err
		}
		var err error
		shop, err = scanShop(tx.QueryRow(ctx, `SELECT `+shopColumns+` FROM v3_channelmarket.shops s WHERE owner_user_id=$1`, actor.UserID), true)
		return err
	})
	return shop, err
}

func (s *Service) UpdateShop(ctx context.Context, actor Actor, input ShopUpdate) (Shop, error) {
	var shop Shop
	name, err := normalizeGroupName(input.Name)
	if err != nil {
		return shop, ErrInvalidShopName
	}
	description, err := normalizeGroupRemark(input.Description)
	if err != nil {
		return shop, ErrInvalidShopDescription
	}
	if _, err = s.MyShop(ctx, actor); err != nil {
		return shop, err
	}
	err = s.transaction(ctx, func(tx pgx.Tx) error {
		var active bool
		if err = tx.QueryRow(ctx, `SELECT status='active' AND deleted_at IS NULL FROM v3_identity.users WHERE id=$1 FOR SHARE`, actor.UserID).Scan(&active); err != nil {
			return err
		}
		if !active {
			return ErrNotFound
		}
		shop, err = scanShop(tx.QueryRow(ctx, `SELECT `+shopColumns+` FROM v3_channelmarket.shops s WHERE owner_user_id=$1 FOR UPDATE`, actor.UserID), true)
		if err != nil {
			return err
		}
		// Re-submitting a pending candidate is idempotent. Returning to approved text
		// cancels review; an empty name restores the stable system name.
		status := "pending"
		var currentName string
		if err = tx.QueryRow(ctx, `SELECT name FROM v3_channelmarket.shops WHERE owner_user_id=$1`, actor.UserID).Scan(&currentName); err != nil {
			return err
		}
		if name == currentName && description == shop.Description {
			status = "approved"
		}
		_, err = tx.Exec(ctx, `UPDATE v3_channelmarket.shops SET submitted_name=$2,submitted_description=$3,review_status=$4,review_reason='',updated_at=$5 WHERE owner_user_id=$1`, actor.UserID, name, description, status, s.cfg.Now())
		if err != nil {
			return err
		}
		return securityTx(ctx, tx, actor, 0, "shop_profile_submitted", map[string]any{"shop_id": shop.ID, "review_status": status})
	})
	if err != nil {
		return shop, err
	}
	return s.MyShop(ctx, actor)
}

func validShopID(id string) bool {
	n, err := strconv.ParseInt(id, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == id
}

func (s *Service) ReviewShop(ctx context.Context, actor Actor, id string, approved bool, reason string) error {
	if !actor.Admin {
		return ErrNotFound
	}
	if !validShopID(id) || len([]rune(reason)) > 500 {
		return ErrInvalid
	}
	numericID, _ := strconv.ParseInt(id, 10, 64)
	return s.transaction(ctx, func(tx pgx.Tx) error {
		shop, err := scanShop(tx.QueryRow(ctx, `SELECT `+shopColumns+` FROM v3_channelmarket.shops s WHERE s.id=$1 FOR UPDATE`, numericID), true)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if shop.ReviewStatus != "pending" {
			return ErrConflict
		}
		status := "rejected"
		if approved {
			status = "approved"
			name, err := normalizeGroupName(shop.SubmittedName)
			if err != nil {
				return ErrInvalidShopName
			}
			description, err := normalizeGroupRemark(*shop.SubmittedDescription)
			if err != nil {
				return ErrInvalidShopDescription
			}
			if _, err = tx.Exec(ctx, `UPDATE v3_channelmarket.shops SET name=$2,description=$3 WHERE id=$1`, numericID, name, description); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE v3_channelmarket.shops SET review_status=$2,review_reason=$3,updated_at=$4 WHERE id=$1`, numericID, status, strings.TrimSpace(reason), s.cfg.Now())
		if err != nil {
			return err
		}
		return securityTx(ctx, tx, actor, 0, "shop_profile_"+status, map[string]any{"shop_id": id, "owner_user_id": shop.owner, "reason": strings.TrimSpace(reason)})
	})
}

func (s *Service) ListShops(ctx context.Context, actor Actor, admin bool) ([]Shop, error) {
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	if admin && !actor.Admin {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `SELECT `+shopColumns+` FROM v3_channelmarket.shops s JOIN v3_identity.users u ON u.id=s.owner_user_id WHERE u.status='active' AND u.deleted_at IS NULL AND ($1 OR EXISTS(SELECT 1 FROM v3_channelmarket.groups g JOIN v3_catalog.channels c ON c.id=g.channel_id WHERE g.owner_user_id=s.owner_user_id AND g.visibility='public' AND g.lifecycle_status='active' AND g.deleted_at IS NULL AND c.status='enabled'  AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=c.id AND b.user_id=$2))) ORDER BY s.id`, admin, actor.UserID)
	if err != nil {
		return nil, err
	}
	shops := []Shop{}
	for rows.Next() {
		shop, e := scanShop(rows, admin)
		if e != nil {
			rows.Close()
			return nil, e
		}
		shops = append(shops, shop)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = s.attachShopSummaries(ctx, actor, shops, admin); err != nil {
		return nil, err
	}
	if err = s.attachShopRatings(ctx, shops); err != nil {
		return nil, err
	}
	return shops, nil
}

func (s *Service) attachShopRatings(ctx context.Context, shops []Shop) error {
	owners := make([]int64, 0, len(shops))
	for _, shop := range shops {
		owners = append(owners, shop.owner)
	}
	ratings, err := community.OwnerSummaries(ctx, s.pool, owners)
	if err != nil {
		return err
	}
	for i := range shops {
		shops[i].Rating = ratings[shops[i].owner]
	}
	return nil
}

func (s *Service) attachShopSummaries(ctx context.Context, actor Actor, shops []Shop, admin bool) error {
	if len(shops) == 0 {
		return nil
	}
	owners := make([]int64, 0, len(shops))
	for _, shop := range shops {
		owners = append(owners, shop.owner)
	}
	rows, err := s.pool.Query(ctx, `SELECT g.owner_user_id,count(*)::int,
	 ARRAY(SELECT DISTINCT m.model FROM v3_catalog.channel_models m JOIN v3_channelmarket.groups mg ON mg.channel_id=m.channel_id WHERE mg.owner_user_id=g.owner_user_id AND mg.deleted_at IS NULL AND ($3 OR (mg.visibility='public' AND mg.lifecycle_status='active' AND EXISTS(SELECT 1 FROM v3_catalog.channels mc WHERE mc.id=mg.channel_id AND mc.status='enabled') AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=mg.channel_id AND b.user_id=$2))) ORDER BY m.model),
	 ARRAY(SELECT DISTINCT provider_tag.value FROM v3_catalog.channels tc JOIN v3_channelmarket.groups tg ON tg.channel_id=tc.id CROSS JOIN LATERAL jsonb_array_elements_text(coalesce(tc.settings->'market'->'tags','[]')) AS provider_tag(value) WHERE tg.owner_user_id=g.owner_user_id AND tg.deleted_at IS NULL AND ($3 OR (tg.visibility='public' AND tg.lifecycle_status='active' AND tc.status='enabled' AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=tc.id AND b.user_id=$2))) ORDER BY provider_tag.value)
	 FROM v3_channelmarket.groups g JOIN v3_catalog.channels c ON c.id=g.channel_id WHERE g.owner_user_id=ANY($1) AND g.deleted_at IS NULL AND ($3 OR (g.visibility='public' AND g.lifecycle_status='active' AND c.status='enabled' AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=c.id AND b.user_id=$2))) GROUP BY g.owner_user_id`, owners, actor.UserID, admin)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := map[int64]int{}
	for i, shop := range shops {
		index[shop.owner] = i
	}
	for rows.Next() {
		var owner int64
		var count int
		var models, tags []string
		if err = rows.Scan(&owner, &count, &models, &tags); err != nil {
			return err
		}
		if i, ok := index[owner]; ok {
			shops[i].GroupCount = count
			shops[i].Models = models
			shops[i].Tags = visibleProviderTags(tags)
		}
	}
	return rows.Err()
}

func populateShop(shop *Shop, groups []ChannelView) {
	models, tags := map[string]bool{}, map[string]bool{}
	for _, group := range groups {
		if group.OwnerUserID != shop.owner || group.Visibility != "public" {
			continue
		}
		shop.GroupCount++
		for _, m := range group.Models {
			if !models[m] {
				models[m] = true
				shop.Models = append(shop.Models, m)
			}
		}
		for _, tag := range group.Tags {
			if !tags[tag] {
				tags[tag] = true
				shop.Tags = append(shop.Tags, tag)
			}
		}
	}
}

func (s *Service) GetShop(ctx context.Context, actor Actor, id string) (ShopDetail, error) {
	var detail ShopDetail
	if s.pool == nil {
		return detail, ErrUnavailable
	}
	if !validShopID(id) {
		return detail, ErrNotFound
	}
	numericID, _ := strconv.ParseInt(id, 10, 64)
	shop, err := scanShop(s.pool.QueryRow(ctx, `SELECT `+shopColumns+` FROM v3_channelmarket.shops s JOIN v3_identity.users u ON u.id=s.owner_user_id WHERE s.id=$1 AND u.status='active' AND u.deleted_at IS NULL`, numericID), false)
	if errors.Is(err, pgx.ErrNoRows) {
		return detail, ErrNotFound
	}
	if err != nil {
		return detail, err
	}
	groups, err := s.publicShopGroups(ctx, actor, shop.owner)
	if err != nil {
		return detail, err
	}
	detail.Groups = []ChannelView{}
	for _, group := range groups {
		if group.OwnerUserID == shop.owner && group.Visibility == "public" {
			detail.Groups = append(detail.Groups, group)
		}
	}
	if len(detail.Groups) == 0 {
		return detail, ErrNotFound
	}
	populateShop(&shop, detail.Groups)
	shops := []Shop{shop}
	if err = s.attachShopRatings(ctx, shops); err != nil {
		return detail, err
	}
	shop = shops[0]
	detail.Shop = shop
	return detail, nil
}

func (s *Service) publicShopGroups(ctx context.Context, actor Actor, owner int64) ([]ChannelView, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+channelColumns+channelFrom+` WHERE g.owner_user_id=$1 AND g.deleted_at IS NULL AND g.visibility='public' AND g.lifecycle_status='active' AND c.status='enabled' AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=c.id AND b.user_id=$2) ORDER BY g.created_at DESC,g.id`, owner, actor.UserID)
	if err != nil {
		return nil, err
	}
	groups := []ChannelView{}
	for rows.Next() {
		group, e := scanChannel(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		hideNameReview(&group, actor)
		groups = append(groups, group)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = attachRecentRequests(ctx, s.pool, groups, s.cfg.Now()); err != nil {
		return nil, err
	}
	if err = s.attachShops(ctx, groups); err != nil {
		return nil, err
	}
	return groups, nil
}
