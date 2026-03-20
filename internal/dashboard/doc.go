// Package dashboard provides HTTP handlers for the Codero web dashboard.
//
// All endpoints query real state from the SQLite database and Redis
// coordination layer. No synthetic or simulated data is used.
//
// Routes (all under /api/v1/dashboard/):
//
//	GET  /overview           - aggregate metrics for today
//	GET  /repos              - repo list with branch + gate summary
//	GET  /activity           - recent delivery events
//	GET  /block-reasons      - ranked findings/blocker sources
//	GET  /gate-health        - pass rates by provider
//	GET  /settings           - integrations + gate pipeline config
//	PUT  /settings           - validated settings update (persisted, audited)
//	POST /chat               - LiteLLM-backed review-process assistant
//	POST /manual-review-upload - file upload for manual review
//	GET  /events             - SSE stream of live dashboard events
package dashboard
