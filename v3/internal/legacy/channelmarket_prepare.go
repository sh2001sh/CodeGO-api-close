package legacy

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Public-only channels have no legacy gateway ID. Their generated ID must
// survive new channels and gateway IDs appearing between online snapshots.
// Keep it below JavaScript's safe-integer limit; reject any source collision.
func cmGeneratedCatalogID(publicID string) int64 {
	digest := sha256.Sum256([]byte("codego.v3.marketplace.catalog:" + publicID))
	return 1<<46 | int64(binary.BigEndian.Uint64(digest[:8])&((1<<46)-1))
}

func (d *channelMarketData) prepare(sourceSecret string) {
	groupsByChannel := map[string][]cmRow{}
	for _, group := range d.rows["groups"] {
		id := group.text("channel_id")
		groupsByChannel[id] = append(groupsByChannel[id], group)
	}
	rows := append([]cmRow(nil), d.rows["channels"]...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].text("id") < rows[j].text("id") })
	used := map[int64]string{}
	for _, r := range rows {
		id := r.text("id")
		owner, err := r.integer("owner_user_id")
		if err == nil {
			err = d.user(owner)
		}
		if id == "" {
			err = errors.New("empty public channel ID")
		}
		if d.channels[id] != nil {
			err = errors.New("duplicate public channel ID")
		}
		catalogID, e := r.integer("internal_channel_id")
		if err == nil {
			err = e
		}
		if catalogID < 0 {
			err = errors.New("negative internal channel ID")
		}
		newCatalog := catalogID == 0
		archiveParent := false
		if newCatalog {
			catalogID = cmGeneratedCatalogID(id)
			if d.internal[catalogID] != nil {
				err = errors.New("generated catalog ID collides with a source gateway channel")
			}
		} else if d.internal[catalogID] == nil {
			groups := groupsByChannel[id]
			deleted, deleteErr := r.timestamp("deleted_at")
			if catalogID > 0 && deleted != nil && deleteErr == nil && len(groups) == 1 {
				groupDeleted, groupErr := groups[0].timestamp("deleted_at")
				archiveParent = groupDeleted != nil && groupErr == nil
			}
			if !archiveParent {
				err = errors.New("internal channel missing from source gateway channels")
			}
		}
		if existing := used[catalogID]; existing != "" {
			err = fmt.Errorf("internal channel %d belongs to both %s and %s", catalogID, existing, id)
		}
		c := &cmChannel{publicID: id, catalogID: catalogID, owner: owner, row: r, newCatalog: newCatalog, archiveParent: archiveParent}
		switch rProvider(r.text("provider_type")) {
		case "openai", "azure", "codex", "anthropic", "gemini":
		default:
			err = errors.New("unsupported source marketplace provider")
		}
		d.channels[id] = c
		used[catalogID] = id
		if newCatalog {
			c.url, e = cmSecret(r.text("base_url_ciphertext"), sourceSecret)
			if err == nil {
				err = e
			}
			c.credential, e = cmSecret(r.text("credential_ciphertext"), sourceSecret)
			if err == nil {
				err = e
			}
			u, parseErr := url.Parse(c.url)
			if parseErr != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				err = errors.New("invalid upstream URL")
			}
			if strings.TrimSpace(c.credential) == "" {
				err = errors.New("empty channel credential")
			}
			c.credentialKind, _, c.credentialExpiry, e = credentialProperties(c.credential, nil, 0)
			if err == nil {
				err = e
			}
		}
		models, e := r.structured("declared_models", "[]")
		if e == nil {
			e = json.Unmarshal(models, &c.models)
		}
		if err == nil {
			err = e
		}
		if newCatalog && len(c.models) == 0 {
			err = errors.New("channel has no declared models")
		}
		if err != nil {
			d.issue("channels", r, err)
		}
	}
	for _, r := range d.rows["groups"] {
		id := r.text("id")
		c, err := d.channel(r.text("channel_id"))
		if err == nil && c.owner != cmInt(r, "owner_user_id") {
			err = errors.New("group owner differs from channel owner")
		}
		if id == "" || r.text("public_slug") == "" || r.text("internal_group_name") == "" {
			err = errors.New("group ID, public slug and internal name are required")
		}
		if d.groups[id] != nil {
			err = errors.New("duplicate group ID")
		}
		d.groups[id] = r
		if err != nil {
			d.issue("groups", r, err)
			continue
		}
		if c.group != nil {
			d.issue("groups", r, errors.New("multiple groups for one market channel"))
			continue
		}
		c.group = r
	}
	for _, c := range d.channels {
		if c.group == nil {
			d.issue("channels", c.row, errors.New("market channel has no group"))
			continue
		}
		d.prepareChannel(c)
	}
	d.preparePermissions()
	d.preparePools()
	d.prepareIncome()
	d.prepareHistory()
	d.validateRecords()
}

func cmInt(r cmRow, key string) int64 { v, _ := r.integer(key); return v }
func cmLifecycle(value string) (string, error) {
	switch value {
	case "draft", "verifying", "active", "paused", "rejected", "deleted":
		return value, nil
	case "pending_review":
		return "verifying", nil
	case "degraded":
		return "active", nil
	case "disabled", "suspended":
		return "paused", nil
	default:
		return "", fmt.Errorf("unknown lifecycle %s", value)
	}
}
func cmVerification(value string) (string, error) {
	switch value {
	case "pending", "queued", "running", "passed", "failed", "paused":
		return value, nil
	case "never_run", "expired", "":
		return "pending", nil
	default:
		return "", fmt.Errorf("unknown verification %s", value)
	}
}

func rProvider(provider string) string {
	switch provider {
	case "openai_compatible":
		return "openai"
	case "azure_openai":
		return "azure"
	case "claude":
		return "anthropic"
	case "openai_max", "openaimax":
		return "openai_max"
	case "zhipu_v4":
		return "zhipu_4v"
	default:
		return provider
	}
}
