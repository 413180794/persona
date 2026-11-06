-- ExtensionAudits without a plugin (GET /api/v1/extensions/-/audit, `personad extension audit`) lists everyone's rows
-- newest first. plugin_audit is append-only and plugin_audit_plugin leads with the plugin, so that listing read the
-- whole table and sorted it; this index lets it read only the newest rows.
CREATE INDEX IF NOT EXISTS plugin_audit_at ON plugin_audit (at DESC, id DESC);
