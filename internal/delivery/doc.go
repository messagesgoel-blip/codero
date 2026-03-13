// Package delivery handles append-only inbox.log writes and seq-number assignment.
// Seq numbers are sourced from Redis INCR for atomicity.
// Implementation begins in P1-S4.
package delivery
