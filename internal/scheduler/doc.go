// Package scheduler implements the weighted-fair-queue dispatch loop.
// It picks branches from the Redis WFQ sorted set and issues leases.
//
// Components:
//   - Queue: manages pending branches with WFQ priority ordering
//   - LeaseManager: handles lease acquisition, release, and renewal
//   - Heartbeat: maintains lease through periodic renewal
//   - VirtualTime: tracks global virtual time for fair scheduling
//
// Redis Key Structure:
//   - codero:<repo>:queue:pending — ZSET of branch → priority (lower = higher priority)
//   - codero:<repo>:queue:vtime — STRING holding virtual time counter
//   - codero:<repo>:lease:<branch> — STRING holding holder ID with TTL
//
// WFQ Scheduling:
//
//	Priority = virtualTime + (1.0 / weight)
//	Higher weight = more service share = slower priority growth
//	Aging bonus prevents starvation of low-weight branches
package scheduler
