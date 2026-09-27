package state

import (
	"context"
	"time"
)

// TrendQuery selects messages received in [From, To) (of FlowID, when set)
// and groups them into buckets of Bucket length starting at From.
type TrendQuery struct {
	FlowID   string
	From, To time.Time
	Bucket   time.Duration
}

// TrendCounts maps a bucket index to the number of messages in each status
// (spec §5 message trends). Only buckets with messages appear.
type TrendCounts map[int]map[string]int

// MessageTrends counts the matching messages per bucket and status, in the
// database.
func (s *sqlStore) MessageTrends(ctx context.Context, q TrendQuery) (TrendCounts, error) {
	ctx = s.bind(ctx)
	from, bucket := q.From.UnixMilli(), q.Bucket.Milliseconds()
	query := `SELECT (received_at - ?) / ?, status, COUNT(*) FROM messages WHERE received_at >= ? AND received_at < ?`
	args := []any{from, bucket, from, q.To.UnixMilli()}
	if q.FlowID != "" {
		query += ` AND flow_id = ?`
		args = append(args, q.FlowID)
	}
	rows, err := s.db.QueryContext(ctx, query+` GROUP BY 1, 2`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := TrendCounts{}
	for rows.Next() {
		var i, n int
		var st string
		if err := rows.Scan(&i, &st, &n); err != nil {
			return nil, err
		}
		if out[i] == nil {
			out[i] = map[string]int{}
		}
		out[i][st] = n
	}
	return out, rows.Err()
}

// MessageTrends counts the matching messages per bucket and status.
func (s *MemStore) MessageTrends(_ context.Context, q TrendQuery) (TrendCounts, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := TrendCounts{}
	for _, m := range s.m {
		// Compare in milliseconds, as the SQL store stores them.
		at, from, to := m.ReceivedAt.UnixMilli(), q.From.UnixMilli(), q.To.UnixMilli()
		if at < from || at >= to || (q.FlowID != "" && m.FlowID != q.FlowID) {
			continue
		}
		i := int((at - from) / q.Bucket.Milliseconds())
		if out[i] == nil {
			out[i] = map[string]int{}
		}
		out[i][string(m.Status)]++
	}
	return out, nil
}
