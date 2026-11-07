-- The plugin registry (docs/extensions/extensions.md, #1101): installed plugins and the switches and settings of built-in ones, their
-- secrets sealed with the hub's secret.key (like connector_secrets), and an audit of what changed them. hub.json's
-- plugins are never here. (Numbered 0291 to stay clear of other lanes' numbers.)
CREATE TABLE IF NOT EXISTS plugins (
	id text PRIMARY KEY,
	source text NOT NULL,
	origin text NOT NULL DEFAULT '',
	version text NOT NULL,
	previous_version text NOT NULL DEFAULT '',
	manifest jsonb NOT NULL,
	granted jsonb NOT NULL DEFAULT '{}',
	granted_digest text NOT NULL DEFAULT '',
	enabled boolean NOT NULL DEFAULT true,
	config jsonb NOT NULL DEFAULT '{}',
	remote jsonb,
	installed_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS plugin_secrets (
	plugin text NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
	name text NOT NULL,
	sealed text NOT NULL,
	updated_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (plugin, name)
);

-- Who installed, upgraded, rolled back, switched, configured or removed a plugin, and when. Never a secret's value
-- or a configuration's values.
CREATE TABLE IF NOT EXISTS plugin_audit (
	id bigserial PRIMARY KEY,
	plugin text NOT NULL,
	action text NOT NULL,
	actor text NOT NULL,
	detail jsonb NOT NULL DEFAULT '{}',
	at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS plugin_audit_plugin ON plugin_audit (plugin, at);
