package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"syscall"
)

type submitResult struct {
	ExitCode int
	Output   string
	Error    string
}

type submitArgs struct {
	Worktree string
	Repo     string
	Branch   string
	Title    string
	Body     string
}

// ExecSubmit runs `codero submit` with the given args and returns the result.
// coderoPath is the path to the codero binary (empty = search PATH).
func ExecSubmit(ctx context.Context, coderoPath, configPath string, args submitArgs) submitResult {
	if coderoPath == "" {
		coderoPath = "codero"
	}

	cmdArgs := []string{
		"submit",
		"--config", configPath,
		"--worktree", args.Worktree,
		"--repo", args.Repo,
		"--branch", args.Branch,
		"--title", args.Title,
	}
	if args.Body != "" {
		cmdArgs = append(cmdArgs, "--body", args.Body)
	}

	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.CommandContext(ctx, coderoPath, cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := submitResult{
		Output: strings.TrimSpace(stdout.String() + "\n" + stderr.String()),
	}

	if err != nil {
		result.Error = err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				result.ExitCode = status.ExitStatus()
			} else {
				result.ExitCode = 1
			}
		} else {
			result.ExitCode = -1
		}
	}

	return result
}
