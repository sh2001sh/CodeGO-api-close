package marketplace

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWeightedRewardBoundaries(t *testing.T) {
	rewards := []Reward{{Kind: "credits", Title: "a", Weight: 2, Amount: 1}, {Kind: "extra_draw", Title: "b", Weight: 3}}
	for _, tc := range []struct {
		draw  int64
		title string
	}{{0, "a"}, {1, "a"}, {2, "b"}, {4, "b"}} {
		r, err := chooseReward(rewards, func(limit int64) (int64, error) {
			if limit != 5 {
				t.Fatalf("limit=%d", limit)
			}
			return tc.draw, nil
		})
		if err != nil || r.Title != tc.title {
			t.Fatalf("draw=%d reward=%v err=%v", tc.draw, r, err)
		}
	}
	if _, err := chooseReward(rewards, func(int64) (int64, error) { return 5, nil }); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid random value: %v", err)
	}
	if _, err := chooseReward([]Reward{{Weight: math.MaxInt64}, {Weight: 1}}, func(int64) (int64, error) { return 0, nil }); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("overflow: %v", err)
	}
}

func TestPoolRejectsInvalidFinancialReward(t *testing.T) {
	p := Pool{Name: "default", Price: 1, DailyLimit: 100, Rewards: []Reward{{Kind: "credits", Title: "reward", Weight: 1, Amount: 1}}}
	if err := validatePool(p); err != nil {
		t.Fatal(err)
	}
	p.Rewards[0].Amount = 0
	if err := validatePool(p); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero reward accepted: %v", err)
	}
	p.Rewards[0] = Reward{Kind: "multiplier", Title: "card", Weight: 1, MultiplierPPM: 1000001, DurationSeconds: 60}
	if err := validatePool(p); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid multiplier accepted: %v", err)
	}
	p.Rewards[0] = Reward{Kind: "multiplier", Title: "card", Weight: 1, MultiplierPPM: 100000, DurationSeconds: 60}
	if err := validatePool(p); err != nil {
		t.Fatalf("uncapped native card rejected: %v", err)
	}
	p.Rewards[0].MaxDiscountMicro = 10
	if err := validatePool(p); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsupported native cap accepted: %v", err)
	}
	p.Rewards[0].PropType = "consume_discount_10"
	if err := validatePool(p); err != nil {
		t.Fatalf("typed historical informational cap rejected: %v", err)
	}
}

func TestGuaranteesPersistAcrossDrawsAndReset(t *testing.T) {
	raw := []Reward{{Kind: "credits", Title: "base", Weight: 1, Amount: 1}}
	g := Guarantees{First: []Reward{{Kind: "credits", Title: "first", Weight: 1, Amount: 5}}, Small: []Reward{{Kind: "credits", Title: "small", Weight: 1, Amount: 10}}, Big: []Reward{{Kind: "credits", Title: "big", Weight: 1, Amount: 100}}, SmallAfter: 3, BigAfter: 5, SmallReset: 10, BigReset: 100}
	state := PityState{}
	draw := func(int64) (int64, error) { return 0, nil }
	for _, tc := range []struct {
		kind       string
		small, big int
	}{{"first", 1, 1}, {"none", 2, 2}, {"small", 0, 3}, {"none", 1, 4}, {"big", 0, 0}} {
		_, kind, err := drawWithGuarantee(raw, g, &state, draw)
		if err != nil || kind != tc.kind || state.SmallProgress != tc.small || state.BigProgress != tc.big {
			t.Fatalf("kind=%s state=%+v err=%v want=%+v", kind, state, err, tc)
		}
	}
}

func TestRewardRangeUsesIntegerSteps(t *testing.T) {
	rewards := []Reward{{Kind: "credits", Title: "range", Weight: 1, Minimum: 100, Maximum: 130, Step: 10}}
	calls := 0
	r, err := chooseReward(rewards, func(limit int64) (int64, error) {
		calls++
		if calls == 1 {
			return 0, nil
		}
		if limit != 4 {
			t.Fatalf("range limit=%d", limit)
		}
		return 3, nil
	})
	if err != nil || r.Amount != 130 {
		t.Fatalf("range=%+v %v", r, err)
	}
}

func TestHTTPAuthenticationAndBodyBoundary(t *testing.T) {
	s := New(nil, nil, nil, nil, nil, Config{})
	unauthorized := s.Handler(func(*http.Request) (int64, bool, error) { return 0, false, errors.New("bad session") })
	w := httptest.NewRecorder()
	unauthorized.ServeHTTP(w, httptest.NewRequest("POST", "/api/blind-box/inventory/open", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized=%d", w.Code)
	}
	h := s.Handler(func(*http.Request) (int64, bool, error) { return 1, false, nil })
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("PUT", "/api/blind-box/admin/pools", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("admin=%d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/blind-box/inventory/open", strings.NewReader(`{"count":1} {"count":2}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("extra JSON=%d", w.Code)
	}
}
