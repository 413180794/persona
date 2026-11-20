BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '4s';
WITH vals AS (
  SELECT coalesce((SELECT value FROM settings WHERE key='assistant.tasks'),'[]'::jsonb) AS tasks,
         coalesce((SELECT value FROM settings WHERE key='assistant.runs'),'[]'::jsonb) AS runs,
         coalesce((SELECT value FROM settings WHERE key='bots.feishu.state'),'{}'::jsonb) AS feishu,
         coalesce((SELECT value FROM settings WHERE key='channels.state'),'{}'::jsonb) AS channels
), tasks AS (
  SELECT t->>'id' AS id, nullif(t->>'last_run','')::timestamptz AS last_run
  FROM vals, jsonb_array_elements(tasks) AS t
), runs AS (
  SELECT r->>'ref_id' AS ref, r->>'status' AS status,
         nullif(r->>'at','')::timestamptz AS at, nullif(r->>'session_id','') IS NOT NULL AS has_session
  FROM vals, jsonb_array_elements(runs) AS r
), feishu_runs AS (
  SELECT r FROM vals, jsonb_array_elements(coalesce(channels->'runs','[]')) AS r
  WHERE r->>'channel' = 'feishu'
)
SELECT jsonb_build_object(
  'sampled_at', statement_timestamp(),
  'transaction_read_only', current_setting('transaction_read_only'),
  'node', jsonb_build_object(
      'machines_with_persisted_state', (SELECT count(DISTINCT machine_id) FROM node_sessions),
      'rows_updated_last_10min', (SELECT count(*) FROM node_sessions WHERE updated_at >= now() - interval '10 minutes'),
      'last_db_write', (SELECT max(updated_at) FROM node_sessions),
      'reliable_receipt_keys', (SELECT count(*) FROM settings WHERE key LIKE 'node_state_receipt.%')),
  'feishu', jsonb_build_object(
      'seen_ids_capped', (SELECT jsonb_array_length(coalesce(feishu->'seen','[]')) FROM vals),
      'state_updated_at', (SELECT updated_at FROM settings WHERE key='bots.feishu.state'),
      'active_followups', (SELECT count(*) FROM feishu_runs),
      'followups_marked_recalled', (SELECT count(*) FROM feishu_runs WHERE r->>'recalled' = 'true'),
      'followups_marked_held', (SELECT count(*) FROM feishu_runs WHERE r->>'held' = 'true')),
  'schedule', jsonb_build_object(
      'task_count', (SELECT count(*) FROM tasks),
      'ledger_entries_capped', (SELECT count(*) FROM runs),
      'started_last_2h', (SELECT count(*) FROM tasks WHERE last_run >= now() - interval '2 hours'),
      'recent_starts_without_nearby_run_candidate',
          (SELECT count(*) FROM tasks t WHERE t.last_run >= now() - interval '2 hours'
            AND NOT EXISTS (SELECT 1 FROM runs r WHERE r.ref=t.id
              AND r.at >= t.last_run - interval '5 seconds' AND r.at <= t.last_run + interval '5 minutes')),
      'recent_ok_with_session_not_terminal_proof',
          (SELECT count(*) FROM runs WHERE at >= now() - interval '2 hours' AND status='ok' AND has_session),
      'last_history', (SELECT max(at) FROM runs)),
  'manager_outbox', (SELECT coalesce(jsonb_object_agg(kind,n),'{}')
                       FROM (SELECT kind,count(*) AS n FROM manager_outbox GROUP BY kind) a));
COMMIT;
