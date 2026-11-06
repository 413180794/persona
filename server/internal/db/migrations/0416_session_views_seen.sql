-- SeenTimes loads only the sessions looked at in the last 30 days, but session_views keeps a row for every session ever
-- viewed (nothing prunes it), so without this the load scans the whole history.
CREATE INDEX IF NOT EXISTS session_views_seen ON session_views (seen_at);
