package tui

import "os"

// IsInteractiveTTY returns true when both stdin and stdout are connected to a
// real terminal. Use this to gate interactive prompts, actions, and alt-screen
// mode so that CI environments, piped scripts, and headless contexts get clean
// non-blocking output.
func IsInteractiveTTY() bool {
	return isTTY(os.Stdin) && isTTY(os.Stdout)
}

// isTTY reports whether f is a character device (i.e. a terminal).
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
