// Package webhook receives and deduplicates incoming GitHub webhook events.
// Dedup is performed via Redis SET NX EX keyed on X-GitHub-Delivery.
// Implementation begins in P1-S5.
package webhook
