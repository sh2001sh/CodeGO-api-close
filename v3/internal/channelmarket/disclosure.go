package channelmarket

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/language"
)

// Declarations describe what the owner says, independently of connectivity tests.
type ChannelModelDisclosure struct {
	Model             string `json:"model"`
	Streaming         string `json:"streaming"`
	Tools             string `json:"tools"`
	StructuredOutputs string `json:"structured_outputs"`
	Vision            string `json:"vision"`
	ContextTokens     *int64 `json:"context_tokens,omitempty"`
	MaxOutputTokens   *int64 `json:"max_output_tokens,omitempty"`
}

type ChannelMarketDisclosureInput struct {
	SourceKind    string                   `json:"source_kind"`
	Regions       []string                 `json:"regions"`
	Retention     string                   `json:"retention"`
	RetentionDays *int                     `json:"retention_days,omitempty"`
	Training      string                   `json:"training"`
	PolicyURL     string                   `json:"policy_url,omitempty"`
	Models        []ChannelModelDisclosure `json:"models"`
}

type ChannelMarketDisclosure struct {
	ChannelMarketDisclosureInput
	UpdatedAt  time.Time `json:"updated_at"`
	Provenance string    `json:"provenance"`
}

func validDisclosureModel(model string) bool {
	return model != "" && len(model) <= 255 && utf8.ValidString(model) && strings.TrimSpace(model) == model &&
		!strings.ContainsFunc(model, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) })
}

func normalizeDisclosure(in ChannelMarketDisclosureInput) (ChannelMarketDisclosureInput, error) {
	if in.SourceKind == "" {
		in.SourceKind = "unknown"
	}
	if in.Retention == "" {
		in.Retention = "unknown"
	}
	if in.Training == "" {
		in.Training = "unknown"
	}
	if !oneOf(in.SourceKind, "unknown", "direct", "reseller", "self_hosted") ||
		!oneOf(in.Retention, "unknown", "none", "limited") || !oneOf(in.Training, "unknown", "no", "yes") ||
		len(in.Regions) > 30 || len(in.Models) > 1000 {
		return in, ErrInvalid
	}
	if in.RetentionDays != nil && (in.Retention != "limited" || *in.RetentionDays < 0 || *in.RetentionDays > 3650) {
		return in, ErrInvalid
	}
	regions := make(map[string]bool)
	for _, raw := range in.Regions {
		region := strings.ToUpper(strings.TrimSpace(raw))
		parsed, err := language.ParseRegion(region)
		if len(region) != 2 || err != nil || parsed.String() != region || !parsed.IsCountry() {
			return in, ErrInvalid
		}
		regions[region] = true
	}
	in.Regions = []string{}
	for region := range regions {
		in.Regions = append(in.Regions, region)
	}
	sort.Strings(in.Regions)
	in.PolicyURL = strings.TrimSpace(in.PolicyURL)
	if in.PolicyURL != "" && !validDisclosurePolicyURL(in.PolicyURL) {
		return in, ErrInvalid
	}
	seen := make(map[string]bool)
	if in.Models == nil {
		in.Models = []ChannelModelDisclosure{}
	}
	for i := range in.Models {
		m := &in.Models[i]
		if !validDisclosureModel(m.Model) || seen[m.Model] {
			return in, ErrInvalid
		}
		seen[m.Model] = true
		for _, field := range []*string{&m.Streaming, &m.Tools, &m.StructuredOutputs, &m.Vision} {
			if *field == "" {
				*field = "unknown"
			}
			if !oneOf(*field, "unknown", "supported", "unsupported") {
				return in, ErrInvalid
			}
		}
		for _, limit := range []*int64{m.ContextTokens, m.MaxOutputTokens} {
			if limit != nil && (*limit <= 0 || *limit > 1_000_000_000) {
				return in, ErrInvalid
			}
		}
		if m.ContextTokens != nil && m.MaxOutputTokens != nil && *m.MaxOutputTokens > *m.ContextTokens {
			return in, ErrInvalid
		}
	}
	sort.Slice(in.Models, func(i, j int) bool { return in.Models[i].Model < in.Models[j].Model })
	return in, nil
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

// Policy links may only link directly to a policy. Contact, tracking, referral,
// social media and generic storefront links cannot be used as public adverts.
// URLs are displayed only; this service never requests them.
func validDisclosurePolicyURL(value string) bool {
	if len(value) > 500 || strings.ContainsFunc(value, unicode.IsSpace) {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Port() != "" && u.Port() != "443") || net.ParseIP(u.Hostname()) != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if !strings.Contains(host, ".") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") {
		return false
	}
	for _, domain := range []string{"t.me", "telegram.me", "telegram.org", "discord.gg", "discord.com", "wa.me", "whatsapp.com", "weixin.qq.com", "qq.com", "wechat.com", "facebook.com", "instagram.com", "x.com", "twitter.com", "youtube.com", "line.me"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return false
		}
	}
	path := strings.ToLower(u.Path)
	for _, text := range []string{"contact", "join", "invite", "coupon", "promo", "discount", "shop", "affiliate", "referral"} {
		if strings.Contains(host+path, text) {
			return false
		}
	}
	for _, text := range []string{"privacy", "policy", "policies", "legal", "data-retention", "data-processing"} {
		if strings.Contains(path, text) {
			return true
		}
	}
	return false
}

func readDisclosureTx(ctx context.Context, tx pgx.Tx, channel int64) (*ChannelMarketDisclosure, error) {
	var raw []byte
	var result ChannelMarketDisclosure
	err := tx.QueryRow(ctx, `SELECT document,updated_at FROM v3_channelmarket.disclosures WHERE channel_id=$1`, channel).Scan(&raw, &result.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &result.ChannelMarketDisclosureInput); err != nil {
		return nil, err
	}
	result.Provenance = "owner_declared"
	return &result, nil
}

func (s *Service) Disclosure(ctx context.Context, a Actor, channel int64) (*ChannelMarketDisclosure, error) {
	var result *ChannelMarketDisclosure
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		if _, err := owned(ctx, tx, a, channel); err != nil {
			return err
		}
		var err error
		result, err = readDisclosureTx(ctx, tx, channel)
		return err
	})
	return result, err
}

func (s *Service) SaveDisclosure(ctx context.Context, a Actor, channel int64, input ChannelMarketDisclosureInput) (*ChannelMarketDisclosure, error) {
	in, err := normalizeDisclosure(input)
	if err != nil {
		return nil, err
	}
	var result *ChannelMarketDisclosure
	err = s.transaction(ctx, func(tx pgx.Tx) error {
		if _, err := owned(ctx, tx, a, channel); err != nil {
			return err
		}
		models := make([]string, len(in.Models))
		for i := range in.Models {
			models[i] = in.Models[i].Model
		}
		var missing int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM unnest($2::text[]) m WHERE NOT EXISTS(SELECT 1 FROM v3_catalog.channel_models c WHERE c.channel_id=$1 AND c.model=m)`, channel, models).Scan(&missing); err != nil {
			return err
		}
		if missing > 0 {
			return ErrInvalid
		}
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO v3_channelmarket.disclosures(channel_id,document,updated_at) VALUES($1,$2,$3) ON CONFLICT(channel_id) DO UPDATE SET document=EXCLUDED.document,updated_at=EXCLUDED.updated_at`, channel, raw, s.cfg.Now()); err != nil {
			return err
		}
		if err = securityTx(ctx, tx, a, channel, "channel_disclosure_updated", map[string]any{"provenance": "owner_declared"}); err != nil {
			return err
		}
		result, err = readDisclosureTx(ctx, tx, channel)
		return err
	})
	return result, err
}
