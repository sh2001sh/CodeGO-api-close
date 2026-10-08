package channelmarket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
)

type ShopPage struct {
	Shops      []Shop
	Pagination BrowsePagination
	Models     []string
}

// Apply shop filters to authorized public inventory before counting/paging;
// summary and rating aggregation then touches only the returned shops.
func (s *Service) BrowseShops(ctx context.Context, a Actor, o BrowseOptions) (ShopPage, error) {
	result := ShopPage{Shops: []Shop{}, Models: []string{}, Pagination: BrowsePagination{Page: o.Page, PageSize: o.PageSize}}
	if s.pool == nil {
		return result, ErrUnavailable
	}
	if o.Page < 1 || o.PageSize < 1 || o.PageSize > 100 {
		return result, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	accessible := `g.owner_user_id=s.owner_user_id AND g.visibility='public' AND g.lifecycle_status='active' AND g.deleted_at IS NULL AND c.status='enabled' AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=c.id AND b.user_id=$1)`
	from := ` FROM v3_channelmarket.shops s JOIN v3_identity.users u ON u.id=s.owner_user_id WHERE u.status='active' AND u.deleted_at IS NULL
 AND EXISTS(SELECT 1 ` + channelFrom + ` WHERE ` + accessible + `)
 AND ($2='' OR strpos(lower(coalesce(nullif(s.name,''),'渠道店铺 #'||s.id)||' '||s.id::text||' '||s.description),lower($2))>0 OR EXISTS(SELECT 1 ` + channelFrom + ` WHERE ` + accessible + ` AND (strpos(lower(g.display_name||' '||g.public_channel_id||' '||coalesce(c.settings->'market'->>'remark','')),lower($2))>0 OR EXISTS(SELECT 1 FROM v3_catalog.channel_models cm WHERE cm.channel_id=c.id AND strpos(lower(cm.model),lower($2))>0))))
 AND ($3='' OR EXISTS(SELECT 1 ` + channelFrom + ` JOIN v3_catalog.channel_models cm ON cm.channel_id=c.id WHERE ` + accessible + ` AND cm.model=$3))
 AND ($4='' OR EXISTS(SELECT 1 ` + channelFrom + ` WHERE ` + accessible + ` AND coalesce(c.settings->'market'->'tags','[]') ? $4))`
	args := []any{a.UserID, o.Search, o.Model, o.Tag}
	if err = tx.QueryRow(ctx, `SELECT count(*)::int`+from, args...).Scan(&result.Pagination.Total); err != nil {
		return result, err
	}
	order := "s.id"
	if o.Sort == "name" {
		order = "coalesce(nullif(s.name,''),'渠道店铺 #'||s.id),s.id"
	}
	rows, err := tx.Query(ctx, `SELECT `+shopColumns+from+` ORDER BY `+order+` LIMIT $5 OFFSET $6`, append(args, o.PageSize, (o.Page-1)*o.PageSize)...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		shop, e := scanShop(rows, false)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Shops = append(result.Shops, shop)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = tx.Query(ctx, `SELECT DISTINCT cm.model `+channelFrom+` JOIN v3_catalog.channel_models cm ON cm.channel_id=c.id WHERE `+browseAccess+` AND g.visibility='public' ORDER BY cm.model`, a.UserID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var model string
		if err = rows.Scan(&model); err != nil {
			rows.Close()
			return result, err
		}
		result.Models = append(result.Models, model)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	if err = s.attachShopSummaries(ctx, a, result.Shops, false); err != nil {
		return result, err
	}
	if err = s.attachShopRatings(ctx, result.Shops); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) BrowseShop(ctx context.Context, a Actor, id string, o BrowseOptions) (ShopDetail, error) {
	var detail ShopDetail
	if s.pool == nil {
		return detail, ErrUnavailable
	}
	if !validShopID(id) {
		return detail, ErrNotFound
	}
	numeric, _ := strconv.ParseInt(id, 10, 64)
	shop, err := scanShop(s.pool.QueryRow(ctx, `SELECT `+shopColumns+` FROM v3_channelmarket.shops s JOIN v3_identity.users u ON u.id=s.owner_user_id WHERE s.id=$1 AND u.status='active' AND u.deleted_at IS NULL`, numeric), false)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return detail, err
	}
	shops := []Shop{shop}
	if err = s.attachShopSummaries(ctx, a, shops, false); err != nil {
		return detail, err
	}
	if shops[0].GroupCount == 0 {
		return detail, ErrNotFound
	}
	if err = s.attachShopRatings(ctx, shops); err != nil {
		return detail, err
	}
	o.Scope = "public"
	page, err := s.browseGroups(ctx, a, o, shop.owner)
	if err != nil {
		return detail, err
	}
	detail.Shop, detail.Groups, detail.Pagination = shops[0], page.Groups, &page.Pagination
	return detail, nil
}

func respondShopPage(w http.ResponseWriter, p ShopPage) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "", "data": p.Shops, "pagination": p.Pagination, "models": p.Models})
}
