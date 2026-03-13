// Package scheduler implements the weighted-fair-queue dispatch loop.
// It picks branches from the Redis WFQ sorted set and issues leases.
// Implementation begins in P1-S3.
package scheduler
