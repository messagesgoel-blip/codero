// Package precommit manages the two-loop local review gate.
// Loop 1: LiteLLM diff review. Loop 2: CodeRabbit CLI diff review.
// Rate-limiting and daily cap enforcement use internal/redis primitives.
// Implementation begins in P1-S5.5.
package precommit
