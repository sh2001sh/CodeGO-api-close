package audit

import "context"

func (s *Service) ListEvents(ctx context.Context, p Principal, query EventQuery) (EventPage, error) {
	q, err := scope(p, query.Query)
	if err != nil {
		return EventPage{}, err
	}
	if query.EventType != nil && (*query.EventType < 0 || *query.EventType > 6) {
		return EventPage{}, ErrInvalid
	}
	if s.pool == nil {
		return EventPage{}, ErrUnavailable
	}
	c, _ := decodeCursor(q.Cursor)
	args := append(queryArgs(q), timeArg(c.At), c.ID, q.Limit+1, query.EventType)
	rows, err := s.pool.Query(ctx, `SELECT id,user_id,key_id,created_at,event_type,content,model,amount,
 prompt_tokens,completion_tokens,duration_seconds,is_stream,channel_id,group_name,request_id
 FROM v3_audit.events `+usageWhere+`
 AND ($7::timestamptz IS NULL OR (created_at,id)<($7,$8))
 AND ($10::int IS NULL OR event_type=$10)
 ORDER BY created_at DESC,id DESC LIMIT $9`, args...)
	if err != nil {
		return EventPage{}, err
	}
	defer rows.Close()
	page := EventPage{Items: make([]Event, 0, q.Limit), PageSize: q.Limit}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.UserID, &e.KeyID, &e.CreatedAt, &e.EventType, &e.Content, &e.Model,
			&e.Amount, &e.PromptTokens, &e.CompletionTokens, &e.DurationSeconds, &e.IsStream,
			&e.ChannelID, &e.GroupName, &e.RequestID); err != nil {
			return EventPage{}, err
		}
		page.Items = append(page.Items, e)
	}
	if err := rows.Err(); err != nil {
		return EventPage{}, err
	}
	if len(page.Items) > q.Limit {
		page.Items = page.Items[:q.Limit]
		page.HasMore = true
		e := page.Items[len(page.Items)-1]
		page.NextCursor = encodeCursor(Usage{CreatedAt: e.CreatedAt, ID: e.ID})
	}
	return page, nil
}
