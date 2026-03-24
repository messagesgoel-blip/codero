-- 000012_tl_v2_closeout.down.sql
-- SQLite does not support DROP COLUMN before 3.35; recreate tables without the new columns.
-- For codero_github_links: last_synced_at
CREATE TABLE codero_github_links_backup AS
    SELECT link_id, task_id, repo_full_name, pr_number, issue_number,
           branch_name, head_sha, pr_state, last_ci_run_id
    FROM codero_github_links;
DROP TABLE codero_github_links;
ALTER TABLE codero_github_links_backup RENAME TO codero_github_links;
CREATE UNIQUE INDEX IF NOT EXISTS idx_codero_github_links_task_id ON codero_github_links (task_id);
CREATE INDEX IF NOT EXISTS idx_codero_github_links_repo_pr ON codero_github_links (repo_full_name, pr_number);
CREATE INDEX IF NOT EXISTS idx_codero_github_links_branch ON codero_github_links (repo_full_name, branch_name);

-- For task_feedback_cache: source_status
CREATE TABLE task_feedback_cache_backup AS
    SELECT cache_id, assignment_id, session_id, task_id,
           ci_snapshot, coderabbit_snapshot, human_review_snapshot,
           compliance_snapshot, context_block, snapshot_at, cache_hash
    FROM task_feedback_cache;
DROP TABLE task_feedback_cache;
ALTER TABLE task_feedback_cache_backup RENAME TO task_feedback_cache;
CREATE UNIQUE INDEX IF NOT EXISTS idx_task_feedback_cache_assignment ON task_feedback_cache (assignment_id);
CREATE INDEX IF NOT EXISTS idx_task_feedback_cache_session ON task_feedback_cache (session_id, snapshot_at DESC);
CREATE INDEX IF NOT EXISTS idx_task_feedback_cache_task ON task_feedback_cache (task_id, snapshot_at DESC);
