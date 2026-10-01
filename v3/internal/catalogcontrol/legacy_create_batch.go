package catalogcontrol

import (
	"fmt"
	"net/http"
)

func (s *Server) legacyCreateChannelBatch(w http.ResponseWriter, r *http.Request, template Channel, distinctNames bool) {
	if len(template.Credentials) == 0 || len(template.Credentials) > 10000 {
		fail(w, 400, "invalid_credentials", "Batch creation requires 1 to 10000 credentials")
		return
	}
	if s.enc == nil {
		fail(w, 503, "encryption_unavailable", "Credential encryption is unavailable")
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	ids := make([]int64, 0, len(template.Credentials))
	for i, credential := range template.Credentials {
		channel := template
		channel.ID = 0
		channel.Credentials = []CredentialInput{credential}
		// Credential prefixes must not appear in readable channel names.
		if distinctNames && len(template.Credentials) > 1 {
			channel.Name = fmt.Sprintf("%s %d", template.Name, i+1)
		}
		if err := channel.validate(); err != nil {
			fail(w, 400, "invalid_channel", err.Error())
			return
		}
		if err := s.writeChannel(r.Context(), tx, &channel); err != nil {
			s.dbError(w, err)
			return
		}
		ids = append(ids, channel.ID)
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, map[string]any{"ids": ids, "count": len(ids)})
}
