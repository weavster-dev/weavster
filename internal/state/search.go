package state

import "strings"

// buildWhere builds the SQL WHERE clause and arguments for a Query.
func buildWhere(q Query) (string, []any) {
	var conds []string
	var args []any

	if q.IDFrom != "" {
		conds = append(conds, "id >= ?")
		args = append(args, q.IDFrom)
	}
	if q.IDTo != "" {
		conds = append(conds, "id <= ?")
		args = append(args, q.IDTo)
	}
	if !q.From.IsZero() {
		conds = append(conds, "received_at >= ?")
		args = append(args, q.From.UnixMilli())
	}
	if !q.To.IsZero() {
		conds = append(conds, "received_at <= ?")
		args = append(args, q.To.UnixMilli())
	}
	if q.Status != "" {
		conds = append(conds, "status = ?")
		args = append(args, string(q.Status))
	}
	if q.ContentType != "" {
		conds = append(conds, "content_type = ?")
		args = append(args, q.ContentType)
	}
	if q.MinAttempts > 0 || q.MaxAttempts > 0 {
		c := "EXISTS (SELECT 1 FROM message_attempts a WHERE a.message_id = messages.id AND 1=1"
		if q.MinAttempts > 0 {
			c += " AND a.attempts >= ?"
			args = append(args, q.MinAttempts)
		}
		if q.MaxAttempts > 0 {
			c += " AND a.attempts <= ?"
			args = append(args, q.MaxAttempts)
		}
		c += ")"
		conds = append(conds, c)
	}
	for k, v := range q.Metadata {
		conds = append(conds,
			"EXISTS (SELECT 1 FROM message_metadata md WHERE md.message_id = messages.id AND md.key = ? AND md.value = ?)")
		args = append(args, k, v)
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	return where, args
}

// buildOrderSort returns the ORDER BY clause for a sort spec.
func buildOrderSort(sortBy string) string {
	asc := !strings.HasPrefix(sortBy, "-")
	field := strings.TrimPrefix(sortBy, "-")
	if field == "" {
		field = "id"
	}
	dir := "ASC"
	if !asc {
		dir = "DESC"
	}
	return "ORDER BY " + field + " " + dir
}

// matches applies a Query predicate to a single message (in-memory search).
func matches(m Message, q Query) bool {
	if q.IDFrom != "" && m.ID < q.IDFrom {
		return false
	}
	if q.IDTo != "" && m.ID > q.IDTo {
		return false
	}
	if !q.From.IsZero() && m.ReceivedAt.Before(q.From) {
		return false
	}
	if !q.To.IsZero() && m.ReceivedAt.After(q.To) {
		return false
	}
	if q.Status != "" && m.Status != q.Status {
		return false
	}
	if q.ContentType != "" && m.ContentType != q.ContentType {
		return false
	}
	if q.MinAttempts > 0 || q.MaxAttempts > 0 {
		matched := false
		for _, a := range m.Attempts {
			if (q.MinAttempts == 0 || a.Attempts >= q.MinAttempts) &&
				(q.MaxAttempts == 0 || a.Attempts <= q.MaxAttempts) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for k, v := range q.Metadata {
		if m.Metadata[k] != v {
			return false
		}
	}
	return true
}
