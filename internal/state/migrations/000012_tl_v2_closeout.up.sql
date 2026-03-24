-- 000012_tl_v2_closeout.up.sql
-- Add last_synced_at to codero_github_links (per spec §12.1)
ALTER TABLE codero_github_links ADD COLUMN last_synced_at DATETIME;

-- Add source_status JSON column to task_feedback_cache (per spec §12.2)
-- Stores {"ci":"available","coderabbit":"pending","human":"not_configured","compliance":"available"}
ALTER TABLE task_feedback_cache ADD COLUMN source_status TEXT NOT NULL DEFAULT '{}';
