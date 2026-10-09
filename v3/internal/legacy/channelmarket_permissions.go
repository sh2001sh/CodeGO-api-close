package legacy

import (
	"errors"
	"fmt"
)

func (d *channelMarketData) preparePermissions() {
	invites := map[int64]string{}
	for _, r := range d.rows["group_invites"] {
		b := cmBuild()
		id := b.integer(r, "id", "id")
		group := r.text("group_id")
		b.put("group_id", group)
		actor := b.integer(r, "created_by", "created_by")
		if _, err := d.group(group); err != nil {
			b.err = err
		}
		if err := d.user(actor); err != nil {
			b.err = err
		}
		if id <= 0 {
			b.err = errors.New("invite ID must be positive")
		}
		invites[id] = group
		hash, err := cmInviteHash(r.text("token_hash"))
		if err != nil {
			b.err = err
		}
		b.put("token_hash", hash)
		b.times(r, "expires_at", "revoked_at", "created_at")
		b.put("expires_at", nil)
		if expires, err := r.timestamp("expires_at"); err != nil {
			b.err = err
		} else {
			b.put("expires_at", expires)
		}
		d.record("v3_channelmarket.group_invites", []string{"id"}, b, r)
	}
	for _, table := range []string{"group_access", "channel_user_blocks", "user_multipliers", "multiplier_notices", "community_channel_ratings", "channel_feedback"} {
		for _, r := range d.rows[table] {
			b := cmBuild()
			user := b.integer(r, "user_id", "user_id")
			if err := d.user(user); err != nil {
				b.err = err
			}
			var keys []string
			target := "v3_channelmarket." + table
			if table == "group_access" {
				group := r.text("group_id")
				b.put("group_id", group)
				if _, err := d.group(group); err != nil {
					b.err = err
				}
				id := cmInt(r, "granted_by_invite")
				b.put("invite_id", nil)
				if id != 0 {
					b.put("invite_id", id)
					if invites[id] != group {
						b.err = errors.New("access invite is missing or belongs to another group")
					}
				}
				b.integer(r, "id", "legacy_id")
				keys = []string{"group_id", "user_id"}
				b.times(r, "created_at")
			} else {
				c, err := d.channel(r.text("channel_id"))
				if err != nil {
					b.err = err
				} else {
					b.put("channel_id", c.catalogID)
				}
				keys = []string{"channel_id", "user_id"}
				switch table {
				case "channel_user_blocks":
					b.integer(r, "id", "legacy_id")
					b.times(r, "created_at")
				case "user_multipliers":
					b.integer(r, "id", "legacy_id")
					b.exactFactor(r, "multiplier", "multiplier_ppm", false)
					b.times(r, "updated_at")
				case "multiplier_notices":
					b.integer(r, "id", "id")
					keys = []string{"id"}
					b.exactFactor(r, "previous_multiplier", "previous_ppm", true)
					b.exactFactor(r, "multiplier", "multiplier_ppm", true)
					b.put("cleared", r.text("cleared") == "true")
					b.texts(r, "source")
					b.times(r, "created_at", "read_at")
				case "community_channel_ratings":
					target = "v3_community.channel_ratings"
					b.put("channel_id", r.text("channel_id"))
					b.integer(r, "id", "legacy_id")
					stars := b.integer(r, "stars", "stars")
					if stars < 1 || stars > 5 {
						b.err = errors.New("stars must be 1 through 5")
					}
					if c != nil && c.owner == user {
						b.err = errors.New("owner self-rating is not permitted")
					}
					b.times(r, "created_at", "updated_at")
				case "channel_feedback":
					b.integer(r, "id", "id")
					keys = []string{"id"}
					b.texts(r, "status")
					b.times(r, "created_at", "updated_at")
				}
			}
			d.record(target, keys, b, r)
		}
	}
	timeRangeKeys := map[string]bool{}
	for _, r := range d.rows["time_range_multipliers"] {
		b := cmBuild()
		b.texts(r, "id", "label")
		id := r.text("id")
		if id == "" || timeRangeKeys[id] {
			b.err = errors.New("time range ID must be nonempty and unique")
		}
		timeRangeKeys[id] = true
		c, err := d.channel(r.text("channel_id"))
		if err != nil {
			b.err = err
		} else {
			b.put("channel_id", c.catalogID)
		}
		start, e := r.integer("start_timestamp")
		if e != nil {
			b.err = e
		}
		end, e := r.integer("end_timestamp")
		if e != nil {
			b.err = e
		}
		b.put("starts_at", cmUnix(start))
		b.put("ends_at", cmUnix(end))
		b.exactFactor(r, "multiplier", "multiplier_ppm", false)
		if end <= start && b.err == nil {
			// V2 accepted these rows without a range check and never applied
			// them in pricing. Preserve their full source rows in channel
			// metadata; do not fabricate an active interval for the V3 table.
			continue
		}
		d.record("v3_channelmarket.time_range_multipliers", []string{"id"}, b, r)
	}
	for _, r := range d.rows["bargain_requests"] {
		b := cmBuild()
		b.texts(r, "id", "group_id", "status", "reason")
		b.put("resolution_note", r.text("admin_note"))
		b.exactFactor(r, "proposed_multiplier", "proposed_ppm", false)
		user := b.integer(r, "user_id", "user_id")
		if err := d.user(user); err != nil {
			b.err = err
		}
		if _, err := d.group(r.text("group_id")); err != nil {
			b.err = err
		}
		status := r.text("status")
		if status == "approved" {
			status = "accepted"
		}
		b.put("status", status)
		if status != "pending" && status != "accepted" && status != "rejected" {
			b.err = fmt.Errorf("unknown bargain status %s", status)
		}
		b.times(r, "created_at", "resolved_at")
		d.record("v3_channelmarket.bargain_requests", []string{"id"}, b, r)
	}
}
