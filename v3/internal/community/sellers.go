package community

import (
	"context"
	"encoding/json"
	"strings"
)

type SellerQuery struct {
	Page, PageSize          int
	Keyword, Provider, Sort string
}

func (q SellerQuery) validate() (SellerQuery, error) {
	q.Keyword, q.Provider, q.Sort = strings.TrimSpace(q.Keyword), strings.TrimSpace(q.Provider), strings.TrimSpace(q.Sort)
	if q.Page == 0 {
		q.Page = 1
	}
	if q.PageSize == 0 {
		q.PageSize = 20
	}
	if q.Sort == "" {
		q.Sort = "rating"
	}
	if q.Page < 1 || q.Page > 10000 || q.PageSize < 1 || q.PageSize > 50 || len([]rune(q.Keyword)) > 64 ||
		(q.Provider != "" && !providerPattern.MatchString(q.Provider)) {
		return q, ErrInvalidQuery
	}
	switch q.Sort {
	case "rating", "updated", "channels":
	default:
		return q, ErrInvalidQuery
	}
	return q, nil
}

const sellersCTE = `WITH public_channels AS (
 SELECT c.id AS internal_id,c.owner_user_id AS owner_id,owner.external_id AS sub,
 owner.username,COALESCE((SELECT COALESCE(NULLIF(s.name,''),'渠道店铺 #'||s.id::text) FROM v3_channelmarket.shops s WHERE s.owner_user_id=owner.id),'member-'||owner.external_id) AS display_name,c.updated_at,
 c.settings->'community'->>'id' AS id,
 COALESCE(c.settings->'community'->>'slug','') AS slug,
 COALESCE(c.settings->'community'->>'name',c.name) AS name,c.provider,
 c.settings->'community'->>'lifecycle_status' AS lifecycle_status,
 c.settings->'community'->>'verification_status' AS verification_status
 ` + eligibleChannels + ` AND COALESCE(owner.external_id,'')<>''),
 filtered_channels AS (SELECT p.* FROM public_channels p WHERE ($1::text='' OR p.provider=$1) AND ($2::text='' OR
 LOWER(username) LIKE $3 ESCAPE '!' OR LOWER(display_name) LIKE $3 ESCAPE '!'
 OR owner_id IN (SELECT owner_id FROM public_channels WHERE
 LOWER(name) LIKE $3 ESCAPE '!' OR LOWER(slug) LIKE $3 ESCAPE '!'))),
 consumer_ratings AS (SELECT p.owner_id,r.user_id,avg(r.stars) AS stars
 FROM public_channels p JOIN v3_community.channel_ratings r ON r.channel_id=p.id GROUP BY p.owner_id,r.user_id),
 owner_ratings AS (SELECT owner_id,avg(stars)*2 AS average_score,count(*) AS rating_count FROM consumer_ratings GROUP BY owner_id),
 sellers AS (SELECT p.owner_id,p.sub,p.username,p.display_name,
 count(DISTINCT p.id) AS channel_count,COALESCE(r.average_score,0)::float8 AS average_score,
 COALESCE(r.rating_count,0) AS rating_count,max(p.updated_at) AS updated_at
 FROM filtered_channels p LEFT JOIN owner_ratings r ON r.owner_id=p.owner_id
 GROUP BY p.owner_id,p.sub,p.username,p.display_name,r.average_score,r.rating_count)`

func (s *Service) ListSellers(ctx context.Context, query SellerQuery) (SellerList, error) {
	q, err := query.validate()
	if err != nil {
		return SellerList{}, err
	}
	if s.pool == nil {
		return SellerList{}, ErrUnavailable
	}
	result := SellerList{Items: []Seller{}, Page: q.Page, PageSize: q.PageSize}
	args := []any{q.Provider, q.Keyword, keywordPattern(q.Keyword)}
	err = s.pool.QueryRow(ctx, sellersCTE+` SELECT count(*) FROM sellers`, args...).Scan(&result.Total)
	if err != nil {
		return SellerList{}, err
	}
	order := "average_score DESC,rating_count DESC,updated_at DESC,sub"
	switch q.Sort {
	case "updated":
		order = "updated_at DESC,sub"
	case "channels":
		order = "channel_count DESC,updated_at DESC,sub"
	}
	rows, err := s.pool.Query(ctx, sellersCTE+` SELECT sub,username,display_name,
 channel_count,average_score,rating_count,COALESCE((SELECT jsonb_agg(to_jsonb(preview)) FROM (
 SELECT p.id,p.slug,p.name,p.provider,p.lifecycle_status,p.verification_status,
 COALESCE((SELECT avg(stars)*2 FROM v3_community.channel_ratings WHERE channel_id=p.id),0)::float8 AS average_score,
 (SELECT count(*) FROM v3_community.channel_ratings WHERE channel_id=p.id) AS rating_count,
 0 AS viewer_stars FROM filtered_channels p WHERE p.owner_id=sellers.owner_id
 ORDER BY average_score DESC,rating_count DESC,p.updated_at DESC,p.id LIMIT 3) preview),'[]'::jsonb)
 FROM sellers ORDER BY `+order+` LIMIT $4 OFFSET $5`, append(args, q.PageSize, (q.Page-1)*q.PageSize)...)
	if err != nil {
		return SellerList{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var seller Seller
		var previews []byte
		if err := rows.Scan(&seller.Subject, &seller.Username, &seller.DisplayName, &seller.ChannelCount,
			&seller.AverageScore, &seller.RatingCount, &previews); err != nil {
			return SellerList{}, err
		}
		if err := json.Unmarshal(previews, &seller.Channels); err != nil {
			return SellerList{}, err
		}
		seller.Username, seller.DisplayName = publicIdentity(seller.Subject, seller.Username, seller.DisplayName)
		result.Items = append(result.Items, seller)
	}
	return result, rows.Err()
}
