-- Best-effort rollback for SQLite (column drop not supported in older versions).
-- This is a no-op placeholder; SQLite 3.35+ supports ALTER TABLE DROP COLUMN.
SELECT 1;
