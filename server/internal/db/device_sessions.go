package db

import (
	"context"
	"time"
)

type DeviceSession struct {
	ID                int64      `json:"id"`
	DeviceID          string     `json:"device_id"`
	ConnectedAt       time.Time  `json:"connected_at"`
	DisconnectedAt    *time.Time `json:"disconnected_at"`
	Reason            string     `json:"reason"`
	Source            string     `json:"source"`
	HeartbeatInterval int        `json:"heartbeat_interval"`
}

type DeviceSessionSummary struct {
	Disconnects       int     `json:"disconnects"`
	LongestGapSeconds float64 `json:"longest_gap_seconds"`
}

type DeviceSessionHistory struct {
	Sessions []DeviceSession      `json:"sessions"`
	Summary  DeviceSessionSummary `json:"summary"`
}

func (d *DB) InsertDeviceSession(ctx context.Context, s DeviceSession) (int64, error) {
	var id int64
	err := d.pool.QueryRow(ctx, `INSERT INTO device_sessions
		(device_id, connected_at, source, heartbeat_interval) VALUES ($1, $2, $3, $4) RETURNING id`,
		s.DeviceID, s.ConnectedAt, s.Source, s.HeartbeatInterval).Scan(&id)
	return id, err
}

func (d *DB) UpdateDeviceSessionHeartbeat(ctx context.Context, id int64, interval int) error {
	_, err := d.pool.Exec(ctx, `UPDATE device_sessions SET heartbeat_interval = $2 WHERE id = $1 AND disconnected_at IS NULL`, id, interval)
	return err
}

func (d *DB) FinishDeviceSession(ctx context.Context, id int64, at time.Time, reason string, interval int) error {
	_, err := d.pool.Exec(ctx, `UPDATE device_sessions SET disconnected_at = GREATEST(connected_at, $2),
		reason = $3, heartbeat_interval = $4 WHERE id = $1 AND disconnected_at IS NULL`, id, at, reason, interval)
	return err
}

// StopDeviceSessions repairs interrupted runs; the actual crash time is unknown.
func (d *DB) StopDeviceSessions(ctx context.Context, at time.Time) error {
	_, err := d.pool.Exec(ctx, `UPDATE device_sessions SET disconnected_at = GREATEST(connected_at, $1),
		reason = 'hub_stopped' WHERE disconnected_at IS NULL`, at)
	return err
}

func (d *DB) PruneDeviceSessions(ctx context.Context, before time.Time) error {
	_, err := d.pool.Exec(ctx, `DELETE FROM device_sessions WHERE disconnected_at < $1`, before)
	return err
}

// DeviceSessions includes connections overlapping the window. One earlier end anchors a gap crossing since.
// Gaps use the union of connection intervals, so simultaneous connections do not create false outages.
func (d *DB) DeviceSessions(ctx context.Context, deviceID string, since, until time.Time) (DeviceSessionHistory, error) {
	out := DeviceSessionHistory{Sessions: []DeviceSession{}}
	rows, err := d.pool.Query(ctx, `SELECT id, device_id, connected_at, disconnected_at, COALESCE(reason, ''), source, heartbeat_interval
		FROM device_sessions WHERE device_id = $1 AND connected_at <= $3 AND
		(disconnected_at IS NULL OR disconnected_at >= $2 OR id = (
			SELECT id FROM device_sessions WHERE device_id = $1 AND disconnected_at < $2
			ORDER BY disconnected_at DESC, id DESC LIMIT 1)) ORDER BY connected_at, id`, deviceID, since, until)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	var end time.Time
	var endReason string
	gap := func(next time.Time) {
		if !end.IsZero() && endReason != "hub_stopped" {
			start := end
			if start.Before(since) {
				start = since
			}
			out.Summary.LongestGapSeconds = max(out.Summary.LongestGapSeconds, next.Sub(start).Seconds())
		}
	}
	for rows.Next() {
		var s DeviceSession
		if err := rows.Scan(&s.ID, &s.DeviceID, &s.ConnectedAt, &s.DisconnectedAt, &s.Reason, &s.Source, &s.HeartbeatInterval); err != nil {
			return out, err
		}
		if s.DisconnectedAt == nil || !s.DisconnectedAt.Before(since) {
			out.Sessions = append(out.Sessions, s)
		}
		if s.DisconnectedAt != nil && !s.DisconnectedAt.Before(since) && !s.DisconnectedAt.After(until) && s.Reason != "hub_stopped" {
			out.Summary.Disconnects++
		}
		if s.ConnectedAt.After(end) {
			gap(s.ConnectedAt)
		}
		nextEnd := until
		if s.DisconnectedAt != nil && s.DisconnectedAt.Before(until) {
			nextEnd = *s.DisconnectedAt
		}
		if nextEnd.After(end) || (nextEnd.Equal(end) && s.Reason == "hub_stopped") {
			end, endReason = nextEnd, s.Reason
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	gap(until)
	return out, nil
}
