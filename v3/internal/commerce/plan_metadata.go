package commerce

import (
	"context"
	"strings"
)

func (s *Service) normalizePlanMetadata(ctx context.Context, p *Plan) error {
	p.UpgradeGroup = strings.TrimSpace(p.UpgradeGroup)
	if len(p.UpgradeGroup) > 64 {
		return ErrInvalid
	}
	limits := make(map[string]int64, len(p.ModelLimits))
	for model, limit := range p.ModelLimits {
		model = strings.TrimSpace(model)
		if model == "" || len(model) > 200 || limit < 0 {
			return ErrInvalid
		}
		if limit > 0 {
			limits[model] = limit
		}
	}
	p.ModelLimits = limits
	if p.UpgradeGroup != "" {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_catalog.groups WHERE name=$1)`, p.UpgradeGroup).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrInvalid
		}
	}
	return nil
}
