package legacy

import "testing"

func TestWalletSamplesIncludeBalanceBoundariesWithoutChangingSource(t *testing.T) {
	users := make([]sourceUser, 130)
	for i := range users {
		users[i] = sourceUser{ID: int64(i + 1), WalletUnits: int64(129 - i)}
	}
	samples := sampledWallets(users)
	if len(samples) != 64 || samples[0].WalletUnits != 0 || samples[63].WalletUnits != 129 {
		t.Fatal("missing zero or maximum balance")
	}
	seen := map[int64]bool{}
	for _, u := range samples {
		if seen[u.ID] {
			t.Fatal("duplicate sample")
		}
		seen[u.ID] = true
	}
	if users[0].WalletUnits != 129 {
		t.Fatal("changed source order")
	}
	if len(sampledWallets(nil)) != 0 || len(sampledWallets(users[:1])) != 1 {
		t.Fatal("small population boundary")
	}
}

func TestSampledNativeChargesUseOriginalDecimalCoefficients(t *testing.T) {
	options := map[string]string{"ModelRatio": `{"tokens":0.75}`, "ModelPrice": `{"call":0.000003}`, "CompletionRatio": `{"tokens":2.5}`, "CacheRatio": `{"tokens":0.1}`}
	prices, err := buildPrices(options)
	if err != nil {
		t.Fatal(err)
	}
	n, err := checkSampledPriceCharges(options, prices)
	if err != nil || n < 8 {
		t.Fatalf("probes=%d err=%v", n, err)
	}
	p := prices["tokens"]
	p.InputPerMTok += 100000
	prices["tokens"] = p
	if _, err = checkSampledPriceCharges(options, prices); err == nil {
		t.Fatal("changed quote passed original-price comparison")
	}
}
