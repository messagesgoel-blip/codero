// Package redis is the single entrypoint for all Redis operations in codero.
// All key construction and command execution must go through this package.
// No raw Redis key strings may be constructed outside this package.
package redis

import (
	"errors"
	"fmt"
	"strings"
)

// Key format: codero:<repo>:<type>:<id>
// repo  — owner/repo slug (e.g. "acme/api")
// kType — key type  (e.g. "lease", "queue", "heartbeat")
// id    — identifier (e.g. branch name or event id)

// ErrInvalidKeyPart is returned when any key segment is empty or contains a colon.
var ErrInvalidKeyPart = errors.New("redis key part must be non-empty and must not contain ':'")

// BuildKey constructs a namespaced Redis key.
// All three parts must be non-empty and must not contain ":".
func BuildKey(repo, kType, id string) (string, error) {
	for _, part := range []string{repo, kType, id} {
		if err := validateKeyPart(part); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("codero:%s:%s:%s", repo, kType, id), nil
}

// MustBuildKey is like BuildKey but panics on invalid input.
// Use only in package-level var blocks or test helpers where input is constant.
func MustBuildKey(repo, kType, id string) string {
	k, err := BuildKey(repo, kType, id)
	if err != nil {
		panic(fmt.Sprintf("redis.MustBuildKey(%q, %q, %q): %v", repo, kType, id, err))
	}
	return k
}

func validateKeyPart(s string) error {
	if s == "" || strings.Contains(s, ":") {
		return fmt.Errorf("%w: got %q", ErrInvalidKeyPart, s)
	}
	return nil
}
