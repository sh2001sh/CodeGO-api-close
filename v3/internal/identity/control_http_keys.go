package identity

import (
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/legacy/identitydto"
)

func (c *Control) keyHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	k, err := scanKey(c.pool.QueryRow(r.Context(), `SELECT `+keyColumns+` FROM `+keyFrom+` WHERE k.user_id=$1 AND k.id=$2 AND k.deleted_at IS NULL`, u.ID, parseID(r.PathValue("id"))))
	c.reply(w, k, err)
}

func (c *Control) keysHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	if !identitydto.IsV3(r) {
		page, limit := legacyPagination(r)
		data, err := c.keyPage(r.Context(), u.ID, page, limit)
		c.reply(w, data, err)
		return
	}
	keys, err := c.ListKeys(r.Context(), u.ID, parseID(r.URL.Query().Get("before")), int(parseID(r.URL.Query().Get("page_size"))))
	c.reply(w, keys, err)
}
func (c *Control) createKeyHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	var in KeyRecord
	if err := decodeControl(w, r, &in); err != nil {
		c.reply(w, nil, err)
		return
	}
	k, raw, err := c.CreateKey(r.Context(), u.ID, in.KeyInput)
	c.reply(w, struct {
		KeyRecord
		Key string `json:"key"`
	}{k, raw}, err)
}
func (c *Control) updateKeyHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	var in KeyRecord
	if err := decodeControl(w, r, &in); err != nil {
		c.reply(w, nil, err)
		return
	}
	if r.URL.Query().Get("status_only") != "" {
		c.reply(w, nil, c.UpdateKeyStatus(r.Context(), u.ID, in.ID, in.Status))
		return
	}
	c.reply(w, nil, c.UpdateKey(r.Context(), u.ID, in.KeyInput))
}
func (c *Control) deleteKeyHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	c.reply(w, nil, c.DeleteKey(r.Context(), u.ID, parseID(r.PathValue("id"))))
}
func (c *Control) revealKeyHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	raw, err := c.RevealKey(r.Context(), u.ID, parseID(r.PathValue("id")))
	c.reply(w, struct {
		Key string `json:"key"`
	}{raw}, err)
}
