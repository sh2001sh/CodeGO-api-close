package http

import (
	"testing"

	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceapp "github.com/sh2001sh/new-api/internal/marketplace/app"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	"github.com/stretchr/testify/require"
)

func TestTokenConnectivityBillingOptionsUsesTokenOwnerForOfficialGroup(t *testing.T) {
	options := tokenConnectivityBillingOptions(
		&identityschema.Token{UserId: 7596},
		&marketplaceapp.RoutingBinding{
			InternalGroup: "official-pro",
			SourceType:    marketplacedomain.SourceTypeOfficial,
		},
	)

	require.Equal(t, 7596, options.UserID)
	require.Equal(t, "official-pro", options.InternalGroup)
	require.Empty(t, options.MarketplaceGroupID)
	require.Zero(t, options.MarketplaceOwnerID)
}

func TestTokenConnectivityBillingOptionsPreservesMarketplaceSettlementIdentity(t *testing.T) {
	options := tokenConnectivityBillingOptions(
		&identityschema.Token{UserId: 42},
		&marketplaceapp.RoutingBinding{
			GroupID:          "market-group",
			InternalGroup:    "market-internal",
			OwnerUserID:      7,
			SourceType:       marketplacedomain.SourceTypeMarketplaceUser,
			CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly,
			Multiplier:       0.8,
			ModelPrices:      map[string]marketplacedomain.ChannelModelPrice{"gpt-test": {InputPricePerMillion: 1}},
		},
	)

	require.Equal(t, 42, options.UserID)
	require.Equal(t, "market-group", options.MarketplaceGroupID)
	require.Equal(t, "market-internal", options.InternalGroup)
	require.Equal(t, 7, options.MarketplaceOwnerID)
	require.Equal(t, marketplacedomain.CreditPolicyUniversalOnly, options.CreditPoolPolicy)
	require.Equal(t, 0.8, options.Multiplier)
	require.Contains(t, options.ModelPrices, "gpt-test")
}
