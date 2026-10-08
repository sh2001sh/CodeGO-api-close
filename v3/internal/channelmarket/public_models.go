package channelmarket

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

// PublicModelCatalog contains only customer-facing metadata and quotations.
// Routing credentials, upstream URLs, procurement costs and private settings
// are deliberately excluded from this projection.
type PublicModelCatalog struct {
	Name        string             `json:"name"`
	ModelID     *int64             `json:"model_id,omitempty"`
	Description string             `json:"description,omitempty"`
	Vendor      string             `json:"vendor,omitempty"`
	Groups      []PublicModelGroup `json:"groups"`
}

type PublicModelGroup struct {
	Slug       string            `json:"slug"`
	Name       string            `json:"name"`
	Multiplier string            `json:"multiplier"`
	Verified   bool              `json:"verified"`
	Price      *PublicModelPrice `json:"price,omitempty"`
}

// Amounts are exact decimal credits after the group's advertised multiplier.
// Expressions are disclosed as dynamic pricing rather than quoted as zero.
type PublicModelPrice struct {
	Mode              string `json:"mode"`
	Unit              string `json:"unit"`
	InputPerMillion   string `json:"input_per_million"`
	OutputPerMillion  string `json:"output_per_million"`
	CacheReadMillion  string `json:"cache_read_per_million"`
	CacheWriteMillion string `json:"cache_write_per_million"`
	PerUnit           string `json:"per_unit"`
	Expression        string `json:"expression,omitempty"`
}

func publicCreditAmount(micro, factor int64) string {
	n := new(big.Int).Mul(big.NewInt(micro), big.NewInt(factor))
	value := new(big.Rat).SetFrac(n, big.NewInt(1_000_000_000_000)).FloatString(12)
	return strings.TrimRight(strings.TrimRight(value, "0"), ".")
}

func publicPrice(p catalog.Price, factor int64) *PublicModelPrice {
	unit, _ := p.Rules["billing_unit"].(string)
	if unit == "" {
		unit = "request"
	}
	mode := p.Mode
	if mode == "" {
		mode = "per_token"
	}
	source, _ := p.Rules["expression"].(string)
	if source == "" {
		source, _ = p.Rules["expr"].(string)
	}
	return &PublicModelPrice{Mode: mode, Unit: unit,
		InputPerMillion:   publicCreditAmount(p.InputPerMTok, factor),
		OutputPerMillion:  publicCreditAmount(p.OutputPerMTok, factor),
		CacheReadMillion:  publicCreditAmount(p.CacheReadPerMTok, factor),
		CacheWriteMillion: publicCreditAmount(p.CacheWritePerMTok, factor),
		PerUnit:           publicCreditAmount(p.PerRequest, factor), Expression: source}
}

func buildPublicModels(groups []ChannelView, metadata catalog.MetadataSnapshot, base map[string]catalog.Price) ([]PublicModelCatalog, error) {
	byName := map[string]*PublicModelCatalog{}
	for _, group := range groups {
		prices, err := catalog.ParseMarketPrices(group.Prices)
		if err != nil {
			return nil, err
		}
		for _, name := range group.Models {
			model := byName[name]
			if model == nil {
				model = &PublicModelCatalog{Name: name, Groups: []PublicModelGroup{}}
				if info, vendor := metadata.Describe(name); info != nil {
					id := info.ID
					model.ModelID, model.Description = &id, info.Description
					if vendor != nil {
						model.Vendor = vendor.Name
					}
				}
				byName[name] = model
			}
			quote := PublicModelGroup{Slug: group.PublicSlug, Name: group.Name,
				Multiplier: formatFactor(group.MultiplierPPM), Verified: group.Verification == "passed"}
			price, found := base[name]
			// As in the settler, any explicit seller price map replaces the
			// global map completely. Missing entries must not appear as free.
			if len(prices) > 0 {
				price, found = prices[name]
			}
			if found {
				quote.Price = publicPrice(price, group.MultiplierPPM)
			}
			model.Groups = append(model.Groups, quote)
		}
	}
	result := make([]PublicModelCatalog, 0, len(byName))
	for _, model := range byName {
		sort.Slice(model.Groups, func(i, j int) bool { return model.Groups[i].Slug < model.Groups[j].Slug })
		result = append(result, *model)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (s *Service) PublicModels(ctx context.Context, a Actor) ([]PublicModelCatalog, error) {
	// Pricing reads its own lean, complete projection. Browser pagination and
	// expensive usage/rating attachments must not limit or delay the directory.
	groups, err := s.readPublicMarketGroups(ctx, a)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	official, err := readPublicOfficialGroups(ctx, tx, a.UserID)
	if err != nil {
		return nil, err
	}
	metadata, err := catalog.ReadMetadata(ctx, tx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT model,mode,input_per_mtok,output_per_mtok,cache_read_per_mtok,cache_write_per_mtok,per_request,rules FROM v3_catalog.model_prices`)
	if err != nil {
		return nil, err
	}
	prices := map[string]catalog.Price{}
	for rows.Next() {
		var price catalog.Price
		var rules []byte
		if err = rows.Scan(&price.Model, &price.Mode, &price.InputPerMTok, &price.OutputPerMTok,
			&price.CacheReadPerMTok, &price.CacheWritePerMTok, &price.PerRequest, &rules); err != nil {
			rows.Close()
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(rules))
		decoder.UseNumber()
		if err = decoder.Decode(&price.Rules); err != nil {
			rows.Close()
			return nil, err
		}
		prices[price.Model] = price
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	for i := range official {
		for _, name := range official[i].Models {
			if name != "*" {
				continue
			}
			concrete := map[string]bool{}
			for _, model := range official[i].Models {
				if model != "*" {
					concrete[model] = true
				}
			}
			for model := range prices {
				if model != "*" {
					concrete[model] = true
				}
			}
			official[i].Models = make([]string, 0, len(concrete))
			for model := range concrete {
				official[i].Models = append(official[i].Models, model)
			}
			break
		}
	}
	groups = append(groups, official...)
	return buildPublicModels(groups, metadata, prices)
}

func (s *Service) readPublicMarketGroups(ctx context.Context, a Actor) ([]ChannelView, error) {
	if s.pool == nil {
		return nil, ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, `SELECT g.public_slug,g.display_name,g.multiplier_ppm,g.verification_status,
	 ARRAY(SELECT model FROM v3_catalog.channel_models cm WHERE cm.channel_id=c.id ORDER BY model),g.model_prices
	 `+channelFrom+` WHERE `+browseAccess+` ORDER BY g.id`, a.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []ChannelView{}
	for rows.Next() {
		var group ChannelView
		if err = rows.Scan(&group.PublicSlug, &group.Name, &group.MultiplierPPM, &group.Verification, &group.Models, &group.Prices); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func readPublicOfficialGroups(ctx context.Context, tx pgx.Tx, userID int64) ([]ChannelView, error) {
	// Anonymous visitors may inspect globally usable official groups. Signed
	// in users follow the same allowed_groups policy as API key creation.
	rows, err := tx.Query(ctx, `SELECT g.name,g.description,(g.multiplier*1000000)::bigint,
		array_agg(DISTINCT cm.model ORDER BY cm.model)
		FROM v3_catalog.groups g JOIN v3_catalog.channel_groups cg ON cg.group_name=g.name
		JOIN v3_catalog.channels c ON c.id=cg.channel_id JOIN v3_catalog.channel_models cm ON cm.channel_id=c.id
		WHERE c.status='enabled' AND c.scope='official'
		AND EXISTS(SELECT 1 FROM v3_catalog.channel_credentials cc WHERE cc.channel_id=c.id AND cc.status='enabled')
		AND (($1::bigint>0 AND g.name IN (SELECT v3_identity.allowed_groups($1))) OR
		($1::bigint=0 AND g.name IN (SELECT jsonb_object_keys(coalesce(
		(SELECT value FROM v3_platform.settings WHERE key='UserUsableGroups' AND NOT sensitive),
		'{"default":"默认分组","vip":"vip分组"}'::jsonb)))))
		GROUP BY g.name,g.description,g.multiplier ORDER BY g.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []ChannelView{}
	for rows.Next() {
		var group ChannelView
		if err = rows.Scan(&group.PublicSlug, &group.Name, &group.MultiplierPPM, &group.Models); err != nil {
			return nil, err
		}
		if group.Name == "" {
			group.Name = group.PublicSlug
		}
		group.Prices = json.RawMessage(`{}`)
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (s *Service) httpPublicModels(w http.ResponseWriter, r *http.Request, a Actor) {
	items, err := s.PublicModels(r.Context(), a)
	s.result(w, items, err)
}
