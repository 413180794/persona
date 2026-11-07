-- The hub's own settings (docs/server/server.md "Migrations"): until now they shared the settings table with the default
-- manager's. Copied once from it on a database the two share; the rows stay there for a build from before (a switch
-- back), and a manager's settings table no longer has the hub's keys read from it.
CREATE TABLE IF NOT EXISTS hub_settings (
    key         text PRIMARY KEY,
    value       jsonb NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

DO $$
BEGIN
    IF to_regclass('settings') IS NOT NULL THEN
        INSERT INTO hub_settings (key, value, updated_at)
        SELECT key, value, updated_at FROM settings
        WHERE key IN ('hub_id', 'user_locale', 'look', 'hub_update', 'hub.event_log', 'hub.assistant_profile',
                      'hub.extensions_report', 'managers.default', 'managers.machines', 'sessions.deleted',
                      'persona_tiers', 'pins', 'mutes', 'mesh.epoch', 'mesh.records')
           OR key LIKE 'codex\_inventory/%'
        ON CONFLICT (key) DO NOTHING;
    END IF;
END $$;
