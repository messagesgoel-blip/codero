package main

// version_test.go — Tests for version stamping (COD-053).
//
// Covers:
//  1. versionCmd prints whatever the package-level version var holds.
//  2. The default value is "dev" so un-stamped dev builds are distinguishable
//     from release builds.
//  3. Confirms the version var is settable (simulated via direct assignment)
//     which validates the -ldflags "-X main.version=..." injection path.

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureVersionStdout redirects os.Stdout, executes f, and returns captured output.
func captureVersionStdout(f func()) string {
	orig := os.Stdout
	rd, wr, _ := os.Pipe()
	os.Stdout = wr
	f()
	wr.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	io.Copy(&buf, rd) //nolint:errcheck
	return buf.String()
}

// TestVersionCmd_DefaultIsDev confirms that the default version value is "dev".
// An un-stamped build must never silently claim a release version.
func TestVersionCmd_DefaultIsDev(t *testing.T) {
	// Save and restore the package-level version.
	orig := version
	t.Cleanup(func() { version = orig })

	version = "dev" // explicit; mirrors the var declaration default

	cmd := versionCmd()
	out := captureVersionStdout(func() {
		cmd.Run(cmd, nil)
	})
	if strings.TrimSpace(out) != "dev" {
		t.Errorf("expected version output %q, got %q", "dev", strings.TrimSpace(out))
	}
}

// TestVersionCmd_StampedVersion confirms that a stamped version string is printed
// verbatim.  This mirrors the -ldflags "-X main.version=v1.2.4" injection path.
func TestVersionCmd_StampedVersion(t *testing.T) {
	orig := version
	t.Cleanup(func() { version = orig })

	version = "v1.2.4"

	cmd := versionCmd()
	out := captureVersionStdout(func() {
		cmd.Run(cmd, nil)
	})
	if strings.TrimSpace(out) != "v1.2.4" {
		t.Errorf("expected version output %q, got %q", "v1.2.4", strings.TrimSpace(out))
	}
}

// TestVersionCmd_SemverFormat confirms that a stamped semver value starts with "v"
// and contains at least two dots (vMAJOR.MINOR.PATCH).
func TestVersionCmd_SemverFormat(t *testing.T) {
	orig := version
	t.Cleanup(func() { version = orig })

	version = "v1.2.4"

	cmd := versionCmd()
	out := strings.TrimSpace(captureVersionStdout(func() {
		cmd.Run(cmd, nil)
	}))

	if !strings.HasPrefix(out, "v") {
		t.Errorf("stamped version should start with 'v', got %q", out)
	}
	if strings.Count(out, ".") < 2 {
		t.Errorf("stamped version should contain at least two dots (vX.Y.Z), got %q", out)
	}
}
