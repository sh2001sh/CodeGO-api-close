package identity

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CurrentPolicyVersion is the version of the published terms, privacy notice and supplier agreement.
const CurrentPolicyVersion = "2026-10-07"

type PolicyRequirement struct {
	Document string `json:"document"`
	Version  string `json:"version"`
	URL      string `json:"url"`
}

type CurrentPolicies struct {
	Version   string              `json:"version"`
	Documents []PolicyRequirement `json:"documents"`
}

type PolicyAcceptanceInput struct {
	Document string `json:"document"`
	Version  string `json:"version"`
	Locale   string `json:"locale"`
}

type PolicyAcceptance struct {
	Document   string    `json:"document"`
	Version    string    `json:"version"`
	Locale     string    `json:"locale"`
	AcceptedAt time.Time `json:"accepted_at"`
}

func validPolicyLocale(locale string) bool {
	switch locale {
	case "zh-HK", "zh-CN", "en", "ja", "ru", "ko", "fr", "de", "ar":
		return true
	default:
		return false
	}
}

func validatePolicyAcceptance(in PolicyAcceptanceInput) error {
	if in.Version != CurrentPolicyVersion || !validPolicyLocale(in.Locale) {
		return ErrInvalidInput
	}
	switch in.Document {
	case "terms", "privacy", "supplier":
		return nil
	default:
		return ErrInvalidInput
	}
}

func validateRegistrationPolicies(in RegisterInput, required bool) error {
	// Internal account creation without supplied agreements records no acceptance.
	// The public registration handler always sets required=true.
	if !required && in.AcceptedTermsVersion == "" && in.AcceptedPrivacyVersion == "" && in.AgreementLocale == "" {
		return nil
	}
	if in.AcceptedTermsVersion != CurrentPolicyVersion || in.AcceptedPrivacyVersion != CurrentPolicyVersion || !validPolicyLocale(in.AgreementLocale) {
		return ErrInvalidInput
	}
	return nil
}

func recordRegistrationPolicies(ctx context.Context, tx pgx.Tx, uid int64, in RegisterInput, now time.Time) error {
	if in.AcceptedTermsVersion == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO v3_identity.policy_acceptances(user_id,document,version,locale,accepted_at)
	 VALUES ($1,'terms',$2,$4,$5),($1,'privacy',$3,$4,$5)`, uid, in.AcceptedTermsVersion, in.AcceptedPrivacyVersion, in.AgreementLocale, now)
	return err
}

func (c *Control) PolicyAcceptances(ctx context.Context, uid int64) ([]PolicyAcceptance, error) {
	if uid <= 0 {
		return nil, ErrForbidden
	}
	rows, err := c.pool.Query(ctx, `SELECT document,version,locale,accepted_at FROM v3_identity.policy_acceptances WHERE user_id=$1 ORDER BY accepted_at DESC,document,version`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]PolicyAcceptance, 0)
	for rows.Next() {
		var acceptance PolicyAcceptance
		if err := rows.Scan(&acceptance.Document, &acceptance.Version, &acceptance.Locale, &acceptance.AcceptedAt); err != nil {
			return nil, err
		}
		result = append(result, acceptance)
	}
	return result, rows.Err()
}

func (c *Control) AcceptPolicy(ctx context.Context, uid int64, in PolicyAcceptanceInput) (PolicyAcceptance, error) {
	if uid <= 0 {
		return PolicyAcceptance{}, ErrForbidden
	}
	if err := validatePolicyAcceptance(in); err != nil {
		return PolicyAcceptance{}, err
	}
	// Do not change the original locale or timestamp on replay, including concurrent requests.
	_, err := c.pool.Exec(ctx, `INSERT INTO v3_identity.policy_acceptances(user_id,document,version,locale,accepted_at)
	 SELECT id,$2,$3,$4,$5 FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL
	 ON CONFLICT (user_id,document,version) DO NOTHING`, uid, in.Document, in.Version, in.Locale, c.cfg.Now())
	if err != nil {
		return PolicyAcceptance{}, err
	}
	var acceptance PolicyAcceptance
	err = c.pool.QueryRow(ctx, `SELECT document,version,locale,accepted_at FROM v3_identity.policy_acceptances
	 WHERE user_id=$1 AND document=$2 AND version=$3`, uid, in.Document, in.Version).
		Scan(&acceptance.Document, &acceptance.Version, &acceptance.Locale, &acceptance.AcceptedAt)
	return acceptance, controlDBError(err)
}

// HasAcceptedCurrentSupplier checks persisted acceptance; a database error must never allow publication.
func HasAcceptedCurrentSupplier(ctx context.Context, pool *pgxpool.Pool, uid int64) (bool, error) {
	if uid <= 0 {
		return false, ErrForbidden
	}
	var accepted bool
	err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_identity.policy_acceptances
	 WHERE user_id=$1 AND document='supplier' AND version=$2)`, uid, CurrentPolicyVersion).Scan(&accepted)
	return accepted, err
}

func (c *Control) registerPolicyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/policies/current", func(w http.ResponseWriter, r *http.Request) {
		c.reply(w, CurrentPolicies{Version: CurrentPolicyVersion, Documents: []PolicyRequirement{
			{Document: "terms", Version: CurrentPolicyVersion, URL: "/terms"},
			{Document: "privacy", Version: CurrentPolicyVersion, URL: "/privacy"},
			{Document: "supplier", Version: CurrentPolicyVersion, URL: "/supplier-agreement"},
		}}, nil)
	})
	mux.HandleFunc("GET /api/user/policy-acceptance", func(w http.ResponseWriter, r *http.Request) {
		u, ok := c.requireUser(w, r)
		if !ok {
			return
		}
		data, err := c.PolicyAcceptances(r.Context(), u.ID)
		c.reply(w, data, err)
	})
	mux.HandleFunc("POST /api/user/policy-acceptance", func(w http.ResponseWriter, r *http.Request) {
		u, ok := c.requireUser(w, r)
		if !ok {
			return
		}
		var in PolicyAcceptanceInput
		if err := decodeControl(w, r, &in); err != nil {
			c.reply(w, nil, err)
			return
		}
		data, err := c.AcceptPolicy(r.Context(), u.ID, in)
		c.reply(w, data, err)
	})
}
