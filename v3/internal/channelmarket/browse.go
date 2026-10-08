package channelmarket

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// BrowseOptions applies filtering before pagination. Unpaged compatibility
// lists remain separate from the bounded buyer-facing browser.
type BrowseOptions struct {
	Page, PageSize                              int
	Search, Model, Tag, Scope, Sort, PriceBasis string
}

type BrowsePagination struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Total    int `json:"total"`
}

type GroupPage struct {
	Groups     []ChannelView
	Pagination BrowsePagination
	Models     []string
}

func parseBrowse(r *http.Request) (BrowseOptions, error) {
	q := r.URL.Query()
	o := BrowseOptions{Page: 1, PageSize: 24, Search: strings.TrimSpace(q.Get("search")), Model: q.Get("model"), Tag: q.Get("tag"), Scope: q.Get("scope"), Sort: q.Get("sort"), PriceBasis: q.Get("price_basis")}
	for key, dst := range map[string]*int{"page": &o.Page, "page_size": &o.PageSize} {
		if value := q.Get(key); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 || (key == "page_size" && n > 100) || (key == "page" && n > 1_000_000) {
				return o, ErrInvalid
			}
			*dst = n
		}
	}
	if o.Scope == "" {
		o.Scope = "all"
	}
	if o.Sort == "" {
		o.Sort = "recommended"
	}
	if o.PriceBasis == "" {
		o.PriceBasis = "input"
	}
	if utf8.RuneCountInString(o.Search) > 200 || len(o.Model) > 255 || (o.Tag != "" && !groupTags[o.Tag]) {
		return o, ErrInvalid
	}
	if o.Scope != "all" && o.Scope != "public" && o.Scope != "private" {
		return o, ErrInvalid
	}
	switch o.Sort {
	case "recommended", "multiplier", "price", "models", "success", "name":
	default:
		return o, ErrInvalid
	}
	switch o.PriceBasis {
	case "input", "output", "cache-read", "cache-write", "request":
	default:
		return o, ErrInvalid
	}
	return o, nil
}

const browseAccess = `g.deleted_at IS NULL AND g.lifecycle_status='active' AND c.status='enabled'
 AND (g.visibility='public' OR g.owner_user_id=$1 OR EXISTS(SELECT 1 FROM v3_channelmarket.group_access ga WHERE ga.group_id=g.id AND ga.user_id=$1))
 AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks b WHERE b.channel_id=c.id AND b.user_id=$1)`

// Minimal candidate data is sorted globally before any expensive recent-request,
// rating, verification or complete price projections are read for the page.
type browseCandidate struct {
	id, name, publicID string
	factor             int64
	models             int
	quality            *GroupQuality
	price              *PublicModelPrice
}

func browsePrice(price *PublicModelPrice, basis string) (string, *big.Rat) {
	if price == nil {
		return "", nil
	}
	var value, unit string
	switch price.Mode {
	case "per_request":
		if basis != "request" {
			return "", nil
		}
		value, unit = price.PerUnit, price.Unit
	case "token", "per_token":
		unit = "million_tokens"
		switch basis {
		case "input":
			value = price.InputPerMillion
		case "output":
			value = price.OutputPerMillion
		case "cache-read":
			value = price.CacheReadMillion
		case "cache-write":
			value = price.CacheWriteMillion
		}
	}
	n, ok := new(big.Rat).SetString(value)
	if !ok || n.Sign() < 0 {
		return "", nil
	}
	return unit, n
}

func compareBrowsePrice(a, b *PublicModelPrice, basis string) int {
	au, av := browsePrice(a, basis)
	bu, bv := browsePrice(b, basis)
	if av == nil || bv == nil {
		if av != nil {
			return -1
		}
		if bv != nil {
			return 1
		}
		return 0
	}
	if au != bu {
		return strings.Compare(au, bu)
	}
	return av.Cmp(bv)
}

func browseLess(a, b browseCandidate, o BrowseOptions) bool {
	compare := 0
	switch o.Sort {
	case "recommended":
		evidence := func(q *GroupQuality) int {
			if q == nil {
				return 0
			}
			if q.Observing {
				return 1
			}
			return 2
		}
		compare = evidence(b.quality) - evidence(a.quality)
		if compare == 0 {
			wilson := func(q *GroupQuality) float64 {
				if q != nil && q.WilsonSuccessRate != nil {
					return *q.WilsonSuccessRate
				}
				return -1
			}
			if x, y := wilson(a.quality), wilson(b.quality); x != y {
				if x > y {
					compare = -1
				} else {
					compare = 1
				}
			}
		}
		if compare == 0 && o.Model != "" {
			compare = compareBrowsePrice(a.price, b.price, o.PriceBasis)
		}
	case "price":
		compare = compareBrowsePrice(a.price, b.price, o.PriceBasis)
	case "success":
		rate := func(q *GroupQuality) float64 {
			if q != nil && q.SuccessRate != nil {
				return *q.SuccessRate
			}
			return -1
		}
		if x, y := rate(a.quality), rate(b.quality); x != y {
			if x > y {
				compare = -1
			} else {
				compare = 1
			}
		}
	case "models":
		compare = b.models - a.models
	case "multiplier":
		if a.factor != b.factor {
			if a.factor < b.factor {
				compare = -1
			} else {
				compare = 1
			}
		}
	}
	if compare == 0 {
		compare = strings.Compare(a.name, b.name)
	}
	if compare == 0 {
		compare = strings.Compare(a.publicID, b.publicID)
	}
	if compare == 0 {
		compare = strings.Compare(a.id, b.id)
	}
	return compare < 0
}

func (s *Service) BrowseGroups(ctx context.Context, a Actor, o BrowseOptions) (GroupPage, error) {
	return s.browseGroups(ctx, a, o, 0)
}

func (s *Service) browseGroups(ctx context.Context, a Actor, o BrowseOptions, owner int64) (GroupPage, error) {
	result := GroupPage{Groups: []ChannelView{}, Models: []string{}, Pagination: BrowsePagination{Page: o.Page, PageSize: o.PageSize}}
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
	where := browseAccess + `
	 AND ($2='' OR strpos(lower(g.display_name||' '||g.public_channel_id||' '||coalesce(c.settings->'market'->>'remark','')||' '||g.source_label||' '||c.provider||' '||coalesce(sh.name,'')||' '||coalesce(sh.id::text,'')),lower($2))>0 OR EXISTS(SELECT 1 FROM v3_catalog.channel_models cm WHERE cm.channel_id=c.id AND strpos(lower(cm.model),lower($2))>0) OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(coalesce(c.settings->'market'->'tags','[]')) tag(value) WHERE strpos(lower(tag.value),lower($2))>0))
 AND ($3='' OR EXISTS(SELECT 1 FROM v3_catalog.channel_models cm WHERE cm.channel_id=c.id AND cm.model=$3))
 AND ($4='' OR coalesce(c.settings->'market'->'tags','[]') ? $4)
 AND ($5='all' OR g.visibility=$5) AND ($6::bigint=0 OR (g.owner_user_id=$6 AND g.visibility='public'))`
	rows, err := tx.Query(ctx, `SELECT g.id,g.display_name,g.public_channel_id,g.multiplier_ppm,
 (SELECT count(*)::int FROM v3_catalog.channel_models cm WHERE cm.channel_id=c.id),
 (SELECT jsonb_build_object('observing',rs.observing,'success_rate',CASE WHEN rs.request_count>0 THEN rs.raw_success_rate END,'wilson_success_rate',CASE WHEN rs.request_count>0 THEN rs.wilson_success_rate END) FROM v3_channelmarket.ranking_snapshots rs WHERE rs.group_id=g.id AND rs.window_hours=24 AND rs.ranking_version='v3-usage' AND rs.calculated_at>=$7::timestamptz-interval '1 hour' AND rs.calculated_at<=$7::timestamptz+interval '5 minutes' ORDER BY rs.calculated_at DESC LIMIT 1),
 CASE WHEN g.model_prices ? $3 THEN jsonb_build_object($3,g.model_prices->$3) ELSE '{}'::jsonb END,
 coalesce((SELECT jsonb_agg(jsonb_build_object('Model',p.model,'Mode',p.mode,'InputPerMTok',p.input_per_mtok,'OutputPerMTok',p.output_per_mtok,'CacheReadPerMTok',p.cache_read_per_mtok,'CacheWritePerMTok',p.cache_write_per_mtok,'PerRequest',p.per_request,'Rules',p.rules)) FROM v3_catalog.model_prices p WHERE p.model=$3 AND g.model_prices='{}'::jsonb),'[]')
 `+channelFrom+` LEFT JOIN v3_channelmarket.shops sh ON sh.owner_user_id=g.owner_user_id WHERE `+where, a.UserID, o.Search, o.Model, o.Tag, o.Scope, owner, s.cfg.Now())
	if err != nil {
		return result, err
	}
	var candidates []browseCandidate
	for rows.Next() {
		var c browseCandidate
		var quality, prices, base []byte
		if err = rows.Scan(&c.id, &c.name, &c.publicID, &c.factor, &c.models, &quality, &prices, &base); err != nil {
			rows.Close()
			return result, err
		}
		if c.quality, err = parseGroupQuality(quality); err != nil {
			rows.Close()
			return result, err
		}
		quotes, e := effectiveModelPrices(ChannelView{Models: []string{o.Model}, Prices: prices, MultiplierPPM: c.factor}, base)
		if e != nil {
			rows.Close()
			return result, e
		}
		c.price = quotes[o.Model]
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	result.Pagination.Total = len(candidates)
	sort.Slice(candidates, func(i, j int) bool { return browseLess(candidates[i], candidates[j], o) })
	start := min((o.Page-1)*o.PageSize, len(candidates))
	end := min(start+o.PageSize, len(candidates))
	ids := make([]string, 0, end-start)
	for _, c := range candidates[start:end] {
		ids = append(ids, c.id)
	}
	if len(ids) > 0 {
		// Browsing never ships the full group × model quotation matrix. A
		// selected model quote is sufficient; details use the existing GET.
		columns := strings.Replace(channelColumns, "g.model_prices", `CASE WHEN g.model_prices ? $3 THEN jsonb_build_object($3,g.model_prices->$3) ELSE '{}'::jsonb END`, 1)
		columns = strings.Replace(columns, "WHERE p.model IN (SELECT model FROM v3_catalog.channel_models WHERE channel_id=c.id)", "WHERE p.model=$3 AND g.model_prices='{}'::jsonb", 1)
		rows, err = tx.Query(ctx, `SELECT `+columns+channelFrom+` WHERE `+browseAccess+` AND g.id=ANY($2::text[]) ORDER BY array_position($2::text[],g.id)`, a.UserID, ids, o.Model)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			c, e := scanChannel(rows)
			if e != nil {
				rows.Close()
				return result, e
			}
			hideNameReview(&c, a)
			result.Groups = append(result.Groups, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
	}
	rows, err = tx.Query(ctx, `SELECT DISTINCT cm.model `+channelFrom+` JOIN v3_catalog.channel_models cm ON cm.channel_id=c.id WHERE `+browseAccess+` AND ($2::bigint=0 OR (g.owner_user_id=$2 AND g.visibility='public')) ORDER BY cm.model`, a.UserID, owner)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return result, err
		}
		result.Models = append(result.Models, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	if err = attachRecentRequests(ctx, s.pool, result.Groups, s.cfg.Now()); err != nil {
		return result, err
	}
	if err = s.attachShops(ctx, result.Groups); err != nil {
		return result, err
	}
	return result, nil
}

func respondGroupPage(w http.ResponseWriter, p GroupPage) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "", "data": p.Groups, "pagination": p.Pagination, "models": p.Models})
}
