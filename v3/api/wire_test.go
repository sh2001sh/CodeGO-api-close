package api_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/v3/api"
	"github.com/sh2001sh/new-api/v3/internal/audit"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/catalogcontrol"
	"github.com/sh2001sh/new-api/v3/internal/channelmarket"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"github.com/sh2001sh/new-api/v3/internal/community"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
	"github.com/sh2001sh/new-api/v3/internal/security"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// A response decoded into the generated DTO must retain the domain's wire
// fields, nulls, exact monetary integers and JSON-string amounts. Testing actual
// domain values catches contract drift independently of specification parsing.
func TestGeneratedDTOsRetainDomainWireValues(t *testing.T) {
	const large = 9007199254740993
	at := time.Date(2026, 9, 30, 10, 20, 30, 0, time.UTC)
	for _, tc := range []struct {
		name              string
		domain, generated any
	}{
		{"key", identity.KeyRecord{KeyInput: identity.KeyInput{ID: 1, Name: "test", BudgetLimited: true, BudgetMicroCredits: ptr(int64(large))}, CreatedAt: at}, &api.KeyRecord{}},
		{"user_affiliate_balance", identity.User{ID: 1, AffiliateMicroCredits: large}, &api.User{}},
		{"affiliate_transfer", identity.AffiliateTransfer{AffiliateTransferInput: identity.AffiliateTransferInput{AmountMicroCredits: large, OperationID: "affiliate-transfer-1"}, AffiliateMicroCredits: large, WalletMicroCredits: large}, &api.AffiliateTransfer{}},
		{"audit_usage", audit.Usage{Amount: large, CreatedAt: at}, &api.AuditUsage{}},
		{"audit_summary", audit.Summary{Amount: large}, &api.AuditSummary{}},
		{"security_audit_string_ids_and_nulls", security.Event{ID: "original:security-event/uuid", UserID: large, TokenID: large, ChannelID: large, OwnerUserID: large, ReviewedBy: large, ReviewStatus: "unreviewed"}, &api.SecurityAuditEvent{}},
		{"security_audit_nested_list", security.EventList{Items: []security.Event{{ID: "retained:security-event", UserID: large, TokenID: large, ChannelID: large, OwnerUserID: large, ReviewedBy: large, CreatedAt: &at, ReviewedAt: &at}}, Total: large, Page: 1, PageSize: 20}, &api.SecurityAuditEventList{}},
		{"audit_event_history", audit.EventPage{Items: []audit.Event{{ID: large, Amount: -large, EventType: 6, CreatedAt: at}}, NextCursor: "event-cursor", HasMore: true, PageSize: 50}, &api.AuditEventPage{}},
		{"audit_request_history", audit.RequestPage{Items: []audit.RequestAudit{{RequestID: "original:request/string", Amount: -large, StartedAt: at, CompletedAt: at}}, PageSize: 50}, &api.AuditRequestPage{}},
		{"audit_attempt_history", audit.AttemptPage{Items: []audit.AttemptAudit{{RequestID: "original:request/string", AttemptID: "attempt:1", AttemptNo: large, StartedAt: at, CompletedAt: at}}, NextCursor: "attempt-cursor", HasMore: true, PageSize: 50}, &api.AuditAttemptPage{}},
		{"ledger_history_signed_and_null", ledger.HistoryPage{Items: []ledger.HistoricalEntry{
			{ID: "original:credit", SourceAccount: "wallet:original-account", Amount: large, BalanceAfter: ptr(credits.Micro(large)), CreatedAt: at},
			{ID: "original:debit", SourceAccount: "key:original-account", Amount: -large, BalanceAfter: ptr(credits.Micro(-large)), CreatedAt: at},
			{ID: "original:unknown-balance", SourceAccount: "subscription:original-account", Amount: -large, BalanceAfter: nil, CreatedAt: at},
		}, Before: "history-cursor"}, &api.LedgerHistoryPage{}},
		{"ledger_history_no_cursor", ledger.HistoryPage{Items: []ledger.HistoricalEntry{}}, &api.LedgerHistoryPage{}},
		{"funding_economics_exact_signed", ledger.FundingDailyEconomics{Date: "2026-10-01", RecognizedRevenue: large, RecognizedCost: large + 1, RecognizedProfit: -1, UnattributedCost: large, Sources: []ledger.FundingEconomicsSource{
			{Source: "subscription", Amount: large, Revenue: large, Cost: large + 1, Profit: -1},
			{Source: "legacy_unattributed", Amount: large, Cost: large, Profit: -large},
		}}, &api.FundingDailyEconomics{}},
		{"funding_economics_empty_sources", ledger.FundingDailyEconomics{Date: "0001-01-01", Sources: []ledger.FundingEconomicsSource{}}, &api.FundingDailyEconomics{}},
		{"rating", community.RatingResult{Channel: community.RatingSummary{AverageScore: 8.2, RatingCount: 3}}, &api.CommunityRatingResult{}},
		{"credential", catalogcontrol.Credential{ID: 5}, &api.CatalogCredential{}},
		{"model_metadata", catalog.ModelMetadata{ID: large, ModelName: "gpt-", NameRule: catalog.NameRulePrefix, VendorID: large, Status: 1, SyncOfficial: 1}, &api.CatalogModelMetadata{}},
		{"vendor_metadata", catalog.VendorMetadata{ID: large, Name: "vendor", Status: 1}, &api.CatalogVendorMetadata{}},
		{"prefill_metadata", catalog.PrefillGroup{ID: large, Name: "endpoint-template", Type: "endpoint", Items: json.RawMessage(`{"limit":9007199254740993,"path":"/v1/chat/completions"}`)}, &api.CatalogPrefillGroup{}},
		// The private metadataPriceView embeds the public Price with these
		// metadata pointers. Cover exact prices plus the new enriched wire fields.
		{"price_metadata", struct {
			catalogcontrol.Price
			Metadata *catalog.ModelMetadata  `json:"metadata,omitempty"`
			Vendor   *catalog.VendorMetadata `json:"vendor,omitempty"`
		}{Price: catalogcontrol.Price{Model: "gpt", InputPerMTok: large, Rules: json.RawMessage(`{}`)}, Metadata: &catalog.ModelMetadata{ID: large, ModelName: "gpt", VendorID: large}, Vendor: &catalog.VendorMetadata{ID: large, Name: "vendor"}}, &api.CatalogPriceView{}},
		{"price_without_metadata", catalogcontrol.Price{Model: "gpt", InputPerMTok: large, Rules: json.RawMessage(`{}`)}, &api.CatalogPriceView{}},
		{"official_route_pool", catalogcontrol.RoutePool{ID: large, Name: "official", ModelScope: "gpt-", Members: []catalogcontrol.PoolMember{{ChannelID: large, CostMultiplier: json.Number("1.000000000000000003"), ModelCostOverrides: map[string]json.Number{"gpt": json.Number("0.0100000000000000003")}, FaultDomain: "provider", Enabled: ptr(true)}}}, &api.CatalogRoutePool{}},
		{"channel_income", channelmarket.Income{}, &api.ChannelMarketIncome{}},
		{"channel_notice", channelmarket.Notice{CreatedAt: at}, &api.ChannelMarketNotice{}},
		{"wallet_transfer", commerce.WalletTransferItem{Amount: large, BalanceAfter: large}, &api.WalletTransferItem{}},
		{"plan", commerce.Plan{Credits: large, PeriodCredits: large}, &api.Plan{}},
		{"plan_paid_group_model_limits", commerce.Plan{Credits: large, PeriodCredits: large, UpgradeGroup: "vip", ModelLimits: map[string]int64{"chat": large, "small": 1}}, &api.Plan{}},
		{"subscription", commerce.Subscription{Balance: large, TotalCredits: large, PeriodCredits: large}, &api.Subscription{}},
		{"order", commerce.Order{AmountMinor: large, Credits: large, CreatedAt: at, ExpiresAt: at}, &api.Order{}},
		{"checkout_discount", commerce.CheckoutDiscount{OriginalMinor: large, PaidMinor: large, Multiplier: "0.01", State: "reserved"}, &api.CheckoutDiscount{}},
		{"redemption_credits", commerce.RedemptionResult{RedeemType: "credits", Credits: large}, &api.RedemptionResult{}},
		{"redemption_subscription", commerce.RedemptionResult{RedeemType: "subscription", PlanID: large, PlanTitle: "保留订阅", UserSubscriptionID: large}, &api.RedemptionResult{}},
		{"redemption_blind_box", commerce.RedemptionResult{RedeemType: "blind_box", BlindBoxQuantity: 100, BlindBoxOrderID: large}, &api.RedemptionResult{}},
		{"redemption_code", commerce.RedemptionCode{ID: large, Name: "typed code", RedeemType: "subscription", PlanID: large, PlanTitle: "保留订阅", State: "used", ClaimedBy: ptr(int64(large))}, &api.RedemptionCode{}},
		{"standard_pool", marketplace.Pool{Standard: marketplace.StandardPolicy{FirstPurchaseMinimumMicro: large}}, &api.MarketplacePool{}},
		{"channel_batch", channelmarket.BatchView{Items: []channelmarket.BatchItem{{BatchReceipt: channelmarket.BatchReceipt{AmountMicro: large}, GroupID: "public-channel"}}, CreatedAt: at, UpdatedAt: at}, &api.ChannelMarketBatchView{}},
		{"oauth_provider", identity.OAuthProviderInfo{Slug: "test", Name: "test", Icon: "icon"}, &api.OAuthProviderInfo{}},
		{"cash_box_money", commerce.CashBoxStatus{Money: json.Number("900719925474099.93")}, &api.CashBoxStatus{}},
		{"fuel_quote", commerce.FuelQuote{Credits: large, AmountMinor: large}, &api.FuelQuote{}},
		{"subscription_conversion", commerce.SubscriptionConversion{SourceCredits: large, TargetCredits: large}, &api.SubscriptionConversion{}},
		{"channel_view_verification", channelmarket.ChannelView{ChannelPolicy: channelmarket.ChannelPolicy{ModelConsistencyStatus: "consistent"}, VerificationView: channelmarket.VerificationView{Stage: "completed", DetectorVersion: "test", ModelResults: []channelmarket.ModelTest{}}, Prices: json.RawMessage(`{}`), CreatedAt: at, UpdatedAt: at}, &api.ChannelMarketChannelView{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := json.Marshal(tc.domain)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(before, tc.generated); err != nil {
				t.Fatal(err)
			}
			after, err := json.Marshal(tc.generated)
			if err != nil {
				t.Fatal(err)
			}
			decode := func(raw []byte) any {
				var result any
				d := json.NewDecoder(bytes.NewReader(raw))
				d.UseNumber()
				if err := d.Decode(&result); err != nil {
					t.Fatal(err)
				}
				return result
			}
			if !reflect.DeepEqual(decode(before), decode(after)) {
				t.Errorf("generated DTO lost domain JSON\nbefore: %s\nafter: %s", before, after)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
