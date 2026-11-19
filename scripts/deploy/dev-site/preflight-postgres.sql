BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '4s';
WITH dbs AS (SELECT oid,datname,datallowconn FROM pg_database WHERE datname IN ('persona_dev','persona_dev_manager')),
activity AS (SELECT datname, backend_type, state,
 CASE WHEN application_name IN ('persona-hub','persona-manager') THEN application_name ELSE 'unattributed' END AS application,
 count(*) AS connections FROM pg_stat_activity WHERE datname IN (SELECT datname FROM dbs) GROUP BY 1,2,3,4)
SELECT jsonb_build_object('observed_at',statement_timestamp(),'read_only',current_setting('transaction_read_only'),
 'cluster',(SELECT system_identifier::text FROM pg_control_system()),
 'databases',(SELECT jsonb_object_agg(datname, jsonb_build_object('oid',oid::text,'allows_connections',datallowconn)) FROM dbs),
 'activity',(SELECT coalesce(jsonb_agg(to_jsonb(activity)),'[]') FROM activity),
 'prepared_transactions',(SELECT count(*) FROM pg_prepared_xacts),
 'replication_slots',(SELECT count(*) FROM pg_replication_slots));
COMMIT;
