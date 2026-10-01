package marketplace

import "github.com/sh2001sh/new-api/v3/pkg/credits"

type Guarantees struct {
	First      []Reward      `json:"first,omitempty"`
	Small      []Reward      `json:"small,omitempty"`
	Big        []Reward      `json:"big,omitempty"`
	SmallAfter int           `json:"small_after,omitempty"`
	BigAfter   int           `json:"big_after,omitempty"`
	SmallReset credits.Micro `json:"small_reset_micro,omitempty"`
	BigReset   credits.Micro `json:"big_reset_micro,omitempty"`
}
type PityState struct {
	Opened        int64 `json:"opened"`
	SmallProgress int   `json:"small_progress"`
	BigProgress   int   `json:"big_progress"`
}

func validateGuarantees(g Guarantees) error {
	if g.SmallAfter < 0 || g.BigAfter < 0 || g.SmallAfter > 1000000 || g.BigAfter > 1000000 {
		return ErrInvalidInput
	}
	if g.SmallAfter > 0 && (g.SmallReset <= 0 || len(g.Small) == 0) {
		return ErrInvalidInput
	}
	if g.BigAfter > 0 && (g.BigReset <= 0 || len(g.Big) == 0) {
		return ErrInvalidInput
	}
	for _, rs := range [][]Reward{g.First, g.Small, g.Big} {
		if len(rs) == 0 {
			continue
		}
		p := Pool{Name: "guarantee", Price: 1, DailyLimit: 1, Rewards: rs}
		// Nested policies are zero; validation stops after these reward lists.
		if err := validatePool(p); err != nil {
			return err
		}
	}
	return nil
}
func drawWithGuarantee(rewards []Reward, g Guarantees, state *PityState, draw func(int64) (int64, error)) (Reward, string, error) {
	kind := "none"
	switch {
	case g.BigAfter > 0 && state.BigProgress >= g.BigAfter-1:
		rewards = g.Big
		kind = "big"
	case g.SmallAfter > 0 && state.SmallProgress >= g.SmallAfter-1:
		rewards = g.Small
		kind = "small"
	case state.Opened == 0 && len(g.First) > 0:
		rewards = g.First
		kind = "first"
	}
	r, err := chooseReward(rewards, draw)
	if err != nil {
		return r, kind, err
	}
	state.Opened++
	advancePityCounters(r, g, state)
	return r, kind, nil
}
func advancePity(r Reward, g Guarantees, state *PityState) {
	state.Opened++
	advancePityCounters(r, g, state)
}
func advancePityCounters(r Reward, g Guarantees, state *PityState) {
	value := credits.Micro(0)
	if r.Kind == "credits" {
		value = r.Amount
	}
	if g.BigReset > 0 && value >= g.BigReset {
		state.SmallProgress = 0
		state.BigProgress = 0
	} else {
		if g.SmallReset > 0 && value >= g.SmallReset {
			state.SmallProgress = 0
		} else if g.SmallAfter > 0 {
			state.SmallProgress = min(state.SmallProgress+1, g.SmallAfter)
		}
		if g.BigAfter > 0 {
			state.BigProgress = min(state.BigProgress+1, g.BigAfter)
		}
	}
}
