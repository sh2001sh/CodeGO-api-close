package legacy

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

type importData struct {
	users          []sourceUser
	keys, channels []json.RawMessage
	options        map[string]string
	prices         map[string]catalog.Price
	commerce       *commerceData
	marketplace    *marketplaceData
	channelMarket  *channelMarketData
	history        *historyData
	funding        *fundingData
	entitlements   *entitlementsData
	oidc           *oidcData
	catalog        *catalogData
	security       *securityData
}

func inspectSource(ctx context.Context, tx pgx.Tx, sources map[string]string) (*importData, Report, error) {
	r := Report{Issues: []Issue{}, UnmappedSources: []string{}, Counts: map[string]int64{}, Amounts: map[string]string{}, OpeningMicroCredits: "0"}
	if err := validateSourceCoverage(ctx, tx, sources, &r); err != nil {
		return nil, r, err
	}
	if err := reportRetiredSources(ctx, tx, sources, &r); err != nil {
		return nil, r, err
	}
	users, err := loadUsers(ctx, tx, sources)
	if err != nil {
		return nil, r, err
	}
	r.Users = len(users)
	reportRetiredUserPoints(users, &r)
	userIDs, usernames := map[int64]bool{}, map[string]bool{}
	for _, u := range users {
		if userIDs[u.ID] || usernames[u.Username] {
			r.Issues = append(r.Issues, Issue{"user", u.ID, "duplicate_user", "source ID or username is duplicated"})
		}
		userIDs[u.ID], usernames[u.Username] = true, true
		amount, issues := ValidateWallet(u.wallet)
		r.Issues = append(r.Issues, issues...)
		if u.ID <= 0 || u.Username == "" {
			r.Issues = append(r.Issues, Issue{"user", u.ID, "invalid_user", "user ID and username are required"})
		}
		if u.Setting != "" && !json.Valid([]byte(u.Setting)) {
			r.Issues = append(r.Issues, Issue{"user", u.ID, "invalid_settings", "settings must be valid JSON"})
		}
		for name, amount := range map[string]int64{"affiliate": u.AffUnits, "affiliate_history": u.AffHistory, "used_credits": u.UsedUnits} {
			if _, err := OpeningBalance(amount); err != nil {
				r.Issues = append(r.Issues, Issue{"user", u.ID, "invalid_user_amount", name + ": " + err.Error()})
			}
		}
		if u.AffCount < 0 || u.RequestCount < 0 {
			r.Issues = append(r.Issues, Issue{"user", u.ID, "invalid_user_count", "source counters must be nonnegative"})
		}
		if err := r.addOpening(amount); err != nil {
			return nil, r, err
		}
	}
	for _, u := range users {
		if u.InviterID > 0 && !userIDs[u.InviterID] {
			r.Issues = append(r.Issues, Issue{"user", u.ID, "missing_inviter", "inviter references an absent source user"})
		}
	}
	keys, err := loadRows(ctx, tx, sources["tokens"])
	if err != nil {
		return nil, r, err
	}
	r.Keys = len(keys)
	if err = validateSourceState(ctx, tx, sources, keys, &r); err != nil {
		return nil, r, err
	}
	for _, row := range keys {
		key, _, _, validationErr := decodeKey(row)
		if validationErr != nil {
			r.Issues = append(r.Issues, Issue{"api_key", key.ID, "invalid_key", validationErr.Error()})
		}
		if !userIDs[key.UserID] {
			r.Issues = append(r.Issues, Issue{"api_key", key.ID, "missing_key_owner", "key references an absent source user"})
		}
	}
	channels, err := loadRows(ctx, tx, sources["channels"])
	if err != nil {
		return nil, r, err
	}
	r.Channels = len(channels)
	for _, row := range channels {
		channel, _, validationErr := decodeChannel(row)
		if validationErr != nil {
			r.Issues = append(r.Issues, Issue{"channel", channel.ID, "invalid_channel", validationErr.Error()})
		}
	}
	options, err := loadOptions(ctx, tx, sources["options"])
	if err != nil {
		return nil, r, err
	}
	prices, err := buildPrices(options)
	if err != nil {
		r.Issues = append(r.Issues, Issue{"pricing", 0, "invalid_pricing", err.Error()})
	}
	commerce, err := loadCommerce(ctx, tx, sources)
	if err != nil {
		return nil, r, err
	}
	commerce.validate(&r)
	marketplace, err := loadMarketplace(ctx, tx, sources)
	if err != nil {
		return nil, r, err
	}
	marketplace.validate(&r)
	channelMarket, err := loadChannelMarket(ctx, tx, sources)
	if err != nil {
		return nil, r, err
	}
	channelMarket.validate(&r)
	history, err := loadHistory(ctx, tx, sources)
	if err != nil {
		return nil, r, err
	}
	history.validate(&r)
	funding, err := loadFunding(ctx, tx, sources)
	if err != nil {
		return nil, r, err
	}
	funding.validate(&r)
	entitlements, err := loadEntitlements(ctx, tx, sources)
	if err != nil {
		return nil, r, err
	}
	entitlements.validate(&r)
	oidc, err := loadOIDCData(ctx, tx, sources)
	if err != nil {
		return nil, r, err
	}
	oidc.validate(&r)
	catalog, err := loadCatalogData(ctx, tx, sources)
	if err != nil {
		return nil, r, err
	}
	catalog.validate(&r)
	security, err := loadSecurityData(ctx, tx, sources)
	if err != nil {
		return nil, r, err
	}
	security.validate(&r)

	return &importData{users: users, keys: keys, channels: channels, options: options, prices: prices, commerce: commerce, marketplace: marketplace, channelMarket: channelMarket, history: history, funding: funding, entitlements: entitlements, oidc: oidc, catalog: catalog, security: security}, r, nil
}
