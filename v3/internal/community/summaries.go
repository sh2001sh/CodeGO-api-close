package community

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PublicRating is the shared marketplace/community aggregate. A zero count
// means no reviews, rather than a measured score of zero.
type PublicRating struct {
	AverageScore float64 `json:"average_score"`
	RatingCount  int64   `json:"rating_count"`
}

func ChannelSummaries(ctx context.Context, pool *pgxpool.Pool, ids []string) (map[string]PublicRating, error) {
	out := map[string]PublicRating{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := pool.Query(ctx, `SELECT channel_id,avg(stars)*2,count(*) FROM v3_community.channel_ratings WHERE channel_id=ANY($1) GROUP BY channel_id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var r PublicRating
		if err = rows.Scan(&id, &r.AverageScore, &r.RatingCount); err != nil {
			return nil, err
		}
		out[id] = r
	}
	return out, rows.Err()
}

// OwnerSummaries weights consumers equally across every eligible public
// channel. Search/provider filters must never change a seller's reputation.
func OwnerSummaries(ctx context.Context, pool *pgxpool.Pool, owners []int64) (map[int64]PublicRating, error) {
	out := map[int64]PublicRating{}
	if len(owners) == 0 {
		return out, nil
	}
	rows, err := pool.Query(ctx, `SELECT owner_id,avg(stars)*2,count(*) FROM (
 SELECT channels.owner_id,r.user_id,avg(r.stars) AS stars FROM (
 SELECT c.owner_user_id AS owner_id,c.settings->'community'->>'id' AS public_id
 `+eligibleChannels+` AND c.owner_user_id=ANY($1)) channels
 JOIN v3_community.channel_ratings r ON r.channel_id=channels.public_id
 GROUP BY channels.owner_id,r.user_id) consumers GROUP BY owner_id`, owners)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var r PublicRating
		if err = rows.Scan(&id, &r.AverageScore, &r.RatingCount); err != nil {
			return nil, err
		}
		out[id] = r
	}
	return out, rows.Err()
}
