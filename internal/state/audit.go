package state

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// AuditRecord is one stored audit entry (who did what, to what, when).
type AuditRecord struct {
	ID       int64
	At       time.Time
	Actor    string
	Action   string
	Resource string
	Detail   map[string]string
}

// AuditQuery narrows an audit search; zero fields are open. Results come in
// id order (oldest first) after AfterID, at most Limit (default 100).
// ExcludeAction leaves out entries with that action.
type AuditQuery struct {
	Actor, Action, Resource string
	ExcludeAction           string
	From, To                time.Time // inclusive, to the millisecond
	AfterID                 int64
	Limit                   int
}

// maxMemAudit bounds the memory store's audit entries (the oldest go).
const maxMemAudit = 100000

func auditLimit(n int) int {
	if n <= 0 {
		return 100
	}
	return n
}

// auditMatches applies q's filters to r (the memory store).
func auditMatches(r AuditRecord, q AuditQuery) bool {
	ms := r.At.UnixMilli() // as the SQL store compares
	return r.ID > q.AfterID && (q.Actor == "" || r.Actor == q.Actor) && (q.Action == "" || r.Action == q.Action) &&
		(q.Resource == "" || r.Resource == q.Resource) && (q.ExcludeAction == "" || r.Action != q.ExcludeAction) &&
		(q.From.IsZero() || ms >= q.From.UnixMilli()) && (q.To.IsZero() || ms <= q.To.UnixMilli())
}

// auditDetail is detail as stored: never nil.
func auditDetail(d map[string]string) map[string]string {
	out := make(map[string]string, len(d))
	for k, v := range d {
		out[k] = v
	}
	return out
}

// AppendAudit stores r and returns its id.
func (s *sqlStore) AppendAudit(ctx context.Context, r AuditRecord) (int64, error) {
	ctx = s.bind(ctx)
	detail, err := json.Marshal(auditDetail(r.Detail))
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO audit_log (at, actor, action, resource, detail) VALUES (?, ?, ?, ?, ?) RETURNING id`,
		r.At.UnixMilli(), textValue(r.Actor), textValue(r.Action), textValue(r.Resource), string(detail)).Scan(&id)
	return id, err
}

// SearchAudit returns the entries q selects, oldest first.
func (s *sqlStore) SearchAudit(ctx context.Context, q AuditQuery) ([]AuditRecord, error) {
	ctx = s.bind(ctx)
	where, args := "WHERE id > ?", []any{q.AfterID}
	for _, f := range []struct {
		col, val string
	}{{"actor", q.Actor}, {"action", q.Action}, {"resource", q.Resource}} {
		if f.val != "" {
			where += " AND " + f.col + " = ?"
			args = append(args, f.val)
		}
	}
	if q.ExcludeAction != "" {
		where += " AND action <> ?"
		args = append(args, q.ExcludeAction)
	}
	if !q.From.IsZero() {
		where += " AND at >= ?"
		args = append(args, q.From.UnixMilli())
	}
	if !q.To.IsZero() {
		where += " AND at <= ?"
		args = append(args, q.To.UnixMilli())
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, actor, action, resource, detail FROM audit_log `+where+` ORDER BY id LIMIT ?`, append(args, auditLimit(q.Limit))...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []AuditRecord{}
	for rows.Next() {
		var r AuditRecord
		var at int64
		var detail string
		if err := rows.Scan(&r.ID, &at, &r.Actor, &r.Action, &r.Resource, &detail); err != nil {
			return nil, err
		}
		r.At = time.UnixMilli(at).UTC()
		if err := json.Unmarshal([]byte(detail), &r.Detail); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AppendAudit keeps r in memory (the newest maxMemAudit entries).
func (s *MemStore) AppendAudit(_ context.Context, r AuditRecord) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auditSeq++
	r.ID = s.auditSeq
	r.At = time.UnixMilli(r.At.UnixMilli()).UTC() // as the SQL store keeps it
	r.Actor, r.Action, r.Resource = textValue(r.Actor), textValue(r.Action), textValue(r.Resource)
	r.Detail = auditDetail(r.Detail)
	s.audit = append(s.audit, r)
	if len(s.audit) > maxMemAudit {
		s.audit = s.audit[len(s.audit)-maxMemAudit:]
	}
	return r.ID, nil
}

// SearchAudit returns the entries q selects, oldest first.
func (s *MemStore) SearchAudit(_ context.Context, q AuditQuery) ([]AuditRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []AuditRecord{}
	// Ids ascend: start after the cursor.
	for _, r := range s.audit[sort.Search(len(s.audit), func(i int) bool { return s.audit[i].ID > q.AfterID }):] {
		if len(out) == auditLimit(q.Limit) {
			break
		}
		if auditMatches(r, q) {
			out = append(out, r)
		}
	}
	return out, nil
}

// DeleteAuditBefore removes the entries older than t and says how many.
func (s *sqlStore) DeleteAuditBefore(ctx context.Context, t time.Time) (int, error) {
	ctx = s.bind(ctx)
	res, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE at < ?`, t.UnixMilli())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// DeleteAuditBefore removes the entries older than t and says how many.
func (s *MemStore) DeleteAuditBefore(_ context.Context, t time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.audit[:0]
	for _, r := range s.audit {
		if r.At.UnixMilli() >= t.UnixMilli() {
			kept = append(kept, r)
		}
	}
	n := len(s.audit) - len(kept)
	s.audit = kept
	return n, nil
}
