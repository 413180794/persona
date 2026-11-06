-- A connector's secret (a Telegram session, a token), sealed with the hub's secret.key (like launchers' keys and
-- mail passwords): one row per connector, which has one account. Only that connector is handed it
-- (internal/connector Secrets). (Numbered 0211 to stay clear of other lanes' numbers.)
CREATE TABLE IF NOT EXISTS connector_secrets (
	connector text PRIMARY KEY,
	sealed text NOT NULL,
	updated_at timestamptz NOT NULL DEFAULT now()
);
