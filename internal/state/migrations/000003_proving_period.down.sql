-- Rollback Sprint 6 proving period tables.

DROP INDEX IF EXISTS idx_proving_snapshots_date;
DROP TABLE IF EXISTS proving_snapshots;

DROP INDEX IF EXISTS idx_proving_events_repo_created;
DROP INDEX IF EXISTS idx_proving_events_type_created;
DROP TABLE IF EXISTS proving_events;

DROP INDEX IF EXISTS idx_precommit_reviews_repo_created;
DROP TABLE IF EXISTS precommit_reviews;