DROP INDEX IF EXISTS idx_findings_run_id;
DROP INDEX IF EXISTS idx_findings_repo_branch;
DROP TABLE IF EXISTS findings;

DROP INDEX IF EXISTS idx_review_runs_repo_branch;
DROP TABLE IF EXISTS review_runs;

DROP TABLE IF EXISTS webhook_deliveries;

DROP INDEX IF EXISTS idx_delivery_events_repo_branch_seq;
DROP TABLE IF EXISTS delivery_events;
