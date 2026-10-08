package channelmarket

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

func queueNameReviewTx(ctx context.Context, tx pgx.Tx, channel int64, candidate, current, candidateRemark, currentRemark string, nameSupplied, remarkSupplied bool) (bool, error) {
	var policyRaw []byte
	var published bool
	if err := tx.QueryRow(ctx, `SELECT coalesce(c.settings->'market','{}'),g.published_at IS NOT NULL FROM v3_catalog.channels c JOIN v3_channelmarket.groups g ON g.channel_id=c.id WHERE c.id=$1`, channel).Scan(&policyRaw, &published); err != nil {
		return false, err
	}
	var policy ChannelPolicy
	if err := json.Unmarshal(policyRaw, &policy); err != nil {
		return false, err
	}
	if policy.NameStatus == "pending" {
		if !nameSupplied && policy.SubmittedName != "" {
			candidate = policy.SubmittedName
		}
		if !remarkSupplied && policy.SubmittedRemark != nil {
			candidateRemark = *policy.SubmittedRemark
		}
	}
	status := "pending"
	if candidate == current && candidateRemark == currentRemark {
		status = "approved"
	}
	_, err := tx.Exec(ctx, `UPDATE v3_catalog.channels SET settings=jsonb_set(settings,'{market}',coalesce(settings->'market','{}')||jsonb_build_object('submitted_name',$2::text,'submitted_remark',$4::text,'name_status',$3::text,'name_review_reason','')) WHERE id=$1`, channel, candidate, status, candidateRemark)
	return published, err
}

// Pending names belong only to the owner/admin view. Public browsing and the
// model catalog keep the approved name, even for an authorized consumer.
func hideNameReview(c *ChannelView, a Actor) {
	if a.Admin || (a.UserID > 0 && a.UserID == c.OwnerUserID) {
		return
	}
	c.SubmittedName, c.NameStatus, c.NameReviewReason = "", "", ""
	c.SubmittedRemark = nil
}

func reviewNameTx(ctx context.Context, tx pgx.Tx, channel int64, action, candidate, candidateRemark, reason string) error {
	status := "rejected"
	if action == "approve" {
		status = "approved"
		var current, currentRemark string
		if err := tx.QueryRow(ctx, `SELECT name,coalesce(settings->'market'->>'remark','') FROM v3_catalog.channels WHERE id=$1`, channel).Scan(&current, &currentRemark); err != nil {
			return err
		}
		name := candidate
		var err error
		if candidate != current {
			name, err = normalizeGroupName(candidate)
			if err != nil || name == "" {
				return ErrInvalidName
			}
		}
		remark := candidateRemark
		if candidateRemark != currentRemark {
			remark, err = normalizeGroupRemark(candidateRemark)
			if err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_channelmarket.groups SET display_name=$2 WHERE channel_id=$1`, channel, name); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_catalog.channels SET name=$2,settings=jsonb_set(settings,'{market}',coalesce(settings->'market','{}')||jsonb_build_object('remark',$3::text)) WHERE id=$1`, channel, name, remark); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `UPDATE v3_catalog.channels SET settings=jsonb_set(settings,'{market}',coalesce(settings->'market','{}')||jsonb_build_object('name_status',$2::text,'name_review_reason',$3::text)) WHERE id=$1`, channel, status, reason)
	if err != nil {
		return err
	}
	return syncCommunity(ctx, tx, channel)
}
