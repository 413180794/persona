BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '4s';
SELECT jsonb_build_object('observed_at',statement_timestamp(),'read_only',current_setting('transaction_read_only'),
 'pending', (SELECT count(*) FROM client_ops WHERE state='pending'),
 'recent_client_ops', (SELECT coalesce(jsonb_object_agg(state,n),'{}') FROM
     (SELECT state,count(*) n FROM client_ops WHERE updated_at >= now()-interval '2 hours' GROUP BY state) x));
COMMIT;
