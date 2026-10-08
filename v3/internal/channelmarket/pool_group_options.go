package channelmarket

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// RoutePoolGroupOption is a routing candidate, not a Key binding. Official
// members use official:<catalog group> while market members retain internal IDs.
type RoutePoolGroupOption struct {
	GroupID      string      `json:"group_id"`
	RoutingGroup string      `json:"routing_group"`
	Name         string      `json:"name"`
	DisplayID    string      `json:"display_id"`
	Kind         string      `json:"kind"`
	Multiplier   json.Number `json:"multiplier"`
	Models       []string    `json:"models"`
}

func (s *Service) RoutePoolGroupOptions(ctx context.Context, a Actor) ([]RoutePoolGroupOption, error) {
	if a.UserID <= 0 {
		return nil, ErrInvalid
	}
	options := []RoutePoolGroupOption{}
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		// Selection does not need prices, verification, ratings or request
		// statistics. Keep authorization and service eligibility in the query.
		rows, err := tx.Query(ctx, `SELECT g.id,g.internal_group_name,g.display_name,g.public_channel_id,g.multiplier_ppm,
		 ARRAY(SELECT model FROM v3_catalog.channel_models cm WHERE cm.channel_id=c.id ORDER BY model)
		 `+channelFrom+` WHERE `+browseAccess+`
		 AND EXISTS(SELECT 1 FROM v3_catalog.channel_models cm WHERE cm.channel_id=c.id)
		 AND EXISTS(SELECT 1 FROM v3_catalog.channel_credentials k WHERE k.channel_id=c.id AND k.status='enabled')
		 ORDER BY g.created_at DESC,g.id`, a.UserID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var option RoutePoolGroupOption
			var factor int64
			if err = rows.Scan(&option.GroupID, &option.RoutingGroup, &option.Name, &option.DisplayID, &factor, &option.Models); err != nil {
				rows.Close()
				return err
			}
			option.Kind = "market"
			option.Multiplier = json.Number(formatFactor(factor))
			options = append(options, option)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		official, err := readPublicOfficialGroups(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		// Composite personal pools may have official channels bound to their
		// internal catalog group. They are not selectable pool members.
		names := make([]string, 0, len(official))
		for _, group := range official {
			names = append(names, group.PublicSlug)
		}
		rows, err = tx.Query(ctx, `SELECT internal_group_name FROM v3_channelmarket.route_pools WHERE internal_group_name=ANY($1)`, names)
		if err != nil {
			return err
		}
		poolNames := map[string]bool{}
		for rows.Next() {
			var name string
			if err = rows.Scan(&name); err != nil {
				rows.Close()
				return err
			}
			poolNames[name] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, group := range official {
			if poolNames[group.PublicSlug] {
				continue
			}
			options = append(options, RoutePoolGroupOption{GroupID: "official:" + group.PublicSlug, RoutingGroup: group.PublicSlug, Name: group.Name, DisplayID: group.PublicSlug, Kind: "official", Multiplier: json.Number(formatFactor(group.MultiplierPPM)), Models: append([]string{}, group.Models...)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return options, nil
}

func (s *Service) httpPoolGroupOptions(w http.ResponseWriter, r *http.Request, a Actor) {
	items, err := s.RoutePoolGroupOptions(r.Context(), a)
	s.result(w, items, err)
}
