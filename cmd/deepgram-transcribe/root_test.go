package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/alexgorbatchev/godeps"
	"github.com/spf13/cobra"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/internal/cliout"
)

// ffmpegVersionOutput is what a healthy, current ffmpeg prints for -version.
const ffmpegVersionOutput = "ffmpeg version 7.1.1 Copyright (c) 2000-2024 the FFmpeg developers\nbuilt with clang\n"

// errFFmpegMissing is what running a program that is not on PATH returns.
var errFFmpegMissing = errors.New("exec: \"ffmpeg\": executable file not found in $PATH")

// stubRunner answers a version query with output, or fails with err so that the
// program looks missing. Tests must never run the real ffmpeg, because what is
// installed on the machine must not decide whether the suite passes.
func stubRunner(output string, err error) godeps.CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if err != nil {
			return nil, err
		}
		return []byte(output), nil
	}
}

// newTestCLI builds a root command wired to buffers and to a stub Deepgram
// endpoint, with a healthy ffmpeg reported.
func newTestCLI(t *testing.T, cacheDir, endpoint string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	return newTestCLIWithRunner(t, cacheDir, endpoint, stubRunner(ffmpegVersionOutput, nil))
}

// newTestCLIWithRunner is newTestCLI with control over what the external
// programs report, and with the environment neutralised so that a developer's
// own API key, agent mode, terminal width or installed tools cannot change the
// result.
func newTestCLIWithRunner(t *testing.T, cacheDir, endpoint string, runner godeps.CommandRunner) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	t.Setenv(apiKeyEnvVar, "")
	t.Setenv("AGENT", "")
	t.Setenv("COLUMNS", "200")
	// Keep the managed bin directory inside the test, and register PATH so the
	// test framework restores it after godeps prepends to it.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("PATH", os.Getenv("PATH"))

	var out, errOut bytes.Buffer
	root := newRootCmd(&globalOptions{
		defaultCacheDir: cacheDir,
		endpoint:        endpoint,
		depsRunner:      runner,
	})
	root.SetOut(&out)
	root.SetErr(&errOut)

	return root, &out, &errOut
}

// reportForTest renders err the way main does, so that the message and any hint
// attached to it can be asserted.
func reportForTest(root *cobra.Command, err error) {
	cliout.New(root.OutOrStdout(), root.ErrOrStderr()).Report(err)
}

// commandPaths collects every command path in the tree so that the structure can
// be asserted as a whole.
func commandPaths(cmd *cobra.Command) []string {
	var paths []string
	for _, child := range cmd.Commands() {
		if child.Name() == "help" || child.Name() == "completion" {
			continue
		}
		paths = append(paths, child.CommandPath())
		paths = append(paths, commandPaths(child)...)
	}
	return paths
}

func TestRootCommandUsesSubjectVerbStructure(t *testing.T) {
	root, _, _ := newTestCLI(t, t.TempDir(), "")

	got := strings.Join(commandPaths(root), "\n")
	want := []string{
		"deepgram-transcribe cache",
		"deepgram-transcribe cache clear",
		"deepgram-transcribe cache status",
		"deepgram-transcribe dependency",
		"deepgram-transcribe dependency install",
		"deepgram-transcribe dependency list",
		"deepgram-transcribe dependency update",
		"deepgram-transcribe job",
		"deepgram-transcribe job inspect",
		"deepgram-transcribe job list",
		"deepgram-transcribe transcript",
		"deepgram-transcribe transcript create",
	}

	for _, path := range want {
		if !strings.Contains(got, path+"\n") && !strings.HasSuffix(got, path) {
			t.Errorf("expected command %q in tree, got:\n%s", path, got)
		}
	}

	// Three levels is the documented maximum: subject, sub-subject, verb.
	for _, path := range commandPaths(root) {
		if depth := len(strings.Fields(path)); depth > 3 {
			t.Errorf("command %q is nested %d levels deep, maximum is 3", path, depth)
		}
	}
}

func TestVersionPrintsOnlyTheVersion(t *testing.T) {
	root, out, _ := newTestCLI(t, t.TempDir(), "")
	root.SetArgs([]string{"--version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("--version failed: %v", err)
	}

	if got := out.String(); got != version+"\n" {
		t.Errorf("--version printed %q, want %q", got, version+"\n")
	}
}

func TestCacheDirFlagIsAvailableToEverySubcommand(t *testing.T) {
	cacheDir := t.TempDir()

	for _, args := range [][]string{
		{"cache", "status"},
		{"cache", "clear"},
		{"job", "list"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root, _, _ := newTestCLI(t, t.TempDir(), "")
			root.SetArgs(append(args, "--cache-dir", cacheDir))

			if err := root.Execute(); err != nil {
				t.Fatalf("%v with --cache-dir failed: %v", args, err)
			}
		})
	}
}

// captureProcessStreams runs fn with the process's own stdout and stderr
// replaced by pipes, and returns what each received.
//
// The buffers that the other tests install would hide the defect this guards
// against: cobra falls back to stderr only when no output writer is set, which
// is exactly the case for the real binary and never the case once SetOut has
// been called.
func captureProcessStreams(t *testing.T, fn func()) (string, string) {
	t.Helper()

	outReader, outWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating the stdout pipe: %v", err)
	}
	errReader, errWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating the stderr pipe: %v", err)
	}

	originalOut, originalErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outWriter, errWriter
	defer func() { os.Stdout, os.Stderr = originalOut, originalErr }()

	fn()

	// Close before reading, so the reads see EOF rather than blocking. The help
	// screen is far smaller than the pipe buffer, so writing cannot block first.
	if err := outWriter.Close(); err != nil {
		t.Fatalf("closing the stdout pipe: %v", err)
	}
	if err := errWriter.Close(); err != nil {
		t.Fatalf("closing the stderr pipe: %v", err)
	}

	capturedOut, err := io.ReadAll(outReader)
	if err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}
	capturedErr, err := io.ReadAll(errReader)
	if err != nil {
		t.Fatalf("reading captured stderr: %v", err)
	}

	return string(capturedOut), string(capturedErr)
}

// TestHelpGoesToStdout proves that help someone asked for can be piped or
// redirected. cobra's Print falls back to stderr, so rendering help through it
// leaves `--help | less` and `--help > file` empty.
func TestHelpGoesToStdout(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"cache", "--help"},
		{"transcript", "create", "--help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv(apiKeyEnvVar, "")
			t.Setenv("AGENT", "")
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			t.Setenv("PATH", os.Getenv("PATH"))

			// No SetOut or SetErr here: the fallback under test only happens
			// when the command has no writer of its own.
			root := newRootCmd(&globalOptions{defaultCacheDir: t.TempDir()})
			root.SetArgs(args)

			var execErr error
			stdout, stderr := captureProcessStreams(t, func() { execErr = root.Execute() })

			if execErr != nil {
				t.Fatalf("%v failed: %v", args, execErr)
			}
			if !strings.Contains(stdout, "Usage:") {
				t.Errorf("expected the help screen on stdout, got %q", stdout)
			}
			if stderr != "" {
				t.Errorf("expected nothing on stderr, got %q", stderr)
			}
		})
	}
}

func TestAgentHelpAlsoGoesToStdout(t *testing.T) {
	t.Setenv(apiKeyEnvVar, "")
	t.Setenv("AGENT", "1")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("PATH", os.Getenv("PATH"))

	root := newRootCmd(&globalOptions{defaultCacheDir: t.TempDir()})
	root.SetArgs([]string{"--help"})

	var execErr error
	stdout, stderr := captureProcessStreams(t, func() { execErr = root.Execute() })

	if execErr != nil {
		t.Fatalf("--help failed: %v", execErr)
	}
	if !strings.Contains(stdout, "command: deepgram-transcribe") {
		t.Errorf("expected the compact agent help on stdout, got %q", stdout)
	}
	if stderr != "" {
		t.Errorf("expected nothing on stderr, got %q", stderr)
	}
}

// TestUsageAfterBadArgumentsGoesToStderr is the other half of the contract:
// output nobody asked for must not pollute a redirect of the result.
func TestUsageAfterBadArgumentsGoesToStderr(t *testing.T) {
	t.Setenv(apiKeyEnvVar, "")
	t.Setenv("AGENT", "")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("PATH", os.Getenv("PATH"))

	root := newRootCmd(&globalOptions{defaultCacheDir: t.TempDir()})
	root.SetArgs([]string{"transcript", "create"})

	var execErr error
	stdout, stderr := captureProcessStreams(t, func() { execErr = root.Execute() })

	if execErr == nil {
		t.Fatal("expected an error when the audio file is missing")
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("expected the usage screen on stderr, got %q", stderr)
	}
	if stdout != "" {
		t.Errorf("expected nothing on stdout, got %q", stdout)
	}
}

// TestHumanHelpHidesGeneratedCommandsButAgentHelpKeepsThem pins the one option
// this CLI passes to the help renderer. Cobra's generated completion command
// brings four shell children, which would push the first real command five lines
// down a tree whose whole purpose is readability. Agent mode still lists it,
// because there the contract is a full description of what the binary accepts.
func TestHumanHelpHidesGeneratedCommandsButAgentHelpKeepsThem(t *testing.T) {
	human, humanOut, _ := newTestCLI(t, t.TempDir(), "")
	human.SetArgs([]string{"--help"})
	if err := human.Execute(); err != nil {
		t.Fatalf("--help failed: %v", err)
	}

	if !strings.Contains(humanOut.String(), "├─ cache") {
		t.Errorf("expected the command tree, got:\n%s", humanOut.String())
	}
	if strings.Contains(humanOut.String(), "completion") {
		t.Errorf("expected no generated command in the tree, got:\n%s", humanOut.String())
	}

	agent, agentOut, _ := newTestCLI(t, t.TempDir(), "")
	t.Setenv("AGENT", "1")
	agent.SetArgs([]string{"--help"})
	if err := agent.Execute(); err != nil {
		t.Fatalf("--help failed: %v", err)
	}

	if !strings.Contains(agentOut.String(), "completion") {
		t.Errorf("expected agent help to list every accepted command, got:\n%s", agentOut.String())
	}
}

// TestPositionalArgumentsAreDocumented covers the help catalog. Cobra has no
// field for describing a positional argument, so without the catalog a reader
// sees the bare placeholder from Use and has to guess what it accepts.
func TestPositionalArgumentsAreDocumented(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		arg     string
		mention string
	}{
		{
			name:    "transcript create",
			args:    []string{"transcript", "create", "--help"},
			arg:     "<audio-file>",
			mention: "m4a",
		},
		{
			name:    "job inspect",
			args:    []string{"job", "inspect", "--help"},
			arg:     "<audio-file|request-id>",
			mention: "request ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			human, humanOut, _ := newTestCLI(t, t.TempDir(), "")
			human.SetArgs(tt.args)
			if err := human.Execute(); err != nil {
				t.Fatalf("%v failed: %v", tt.args, err)
			}

			screen := humanOut.String()
			if !strings.Contains(screen, "Arguments:") {
				t.Errorf("expected an arguments block, got:\n%s", screen)
			}
			if !strings.Contains(screen, tt.arg) || !strings.Contains(screen, tt.mention) {
				t.Errorf("expected %q described with %q, got:\n%s", tt.arg, tt.mention, screen)
			}

			agent, agentOut, _ := newTestCLI(t, t.TempDir(), "")
			t.Setenv("AGENT", "1")
			agent.SetArgs(tt.args)
			if err := agent.Execute(); err != nil {
				t.Fatalf("%v in agent mode failed: %v", tt.args, err)
			}

			if !strings.Contains(agentOut.String(), "args:") {
				t.Errorf("expected agent mode to describe the arguments, got:\n%s", agentOut.String())
			}
		})
	}
}

// TestHelpCatalogKeysMatchRealCommands guards the catalog against drift. Its keys
// are command paths as strings, so a renamed command would silently stop being
// documented rather than fail to compile.
func TestHelpCatalogKeysMatchRealCommands(t *testing.T) {
	root, _, _ := newTestCLI(t, t.TempDir(), "")

	known := map[string]bool{root.CommandPath(): true}
	for _, path := range commandPaths(root) {
		known[path] = true
	}

	for path := range helpCatalog() {
		if !known[path] {
			t.Errorf("help catalog documents %q, which is not a command in the tree", path)
		}
	}
}

func TestRuntimeFailureDoesNotPrintUsage(t *testing.T) {
	root, out, errOut := newTestCLI(t, t.TempDir(), "")
	root.SetArgs([]string{"transcript", "create", "/does/not/exist.mp3"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for a missing audio file")
	}

	combined := out.String() + errOut.String()
	if strings.Contains(combined, "Usage:") {
		t.Errorf("expected no usage screen for a runtime failure, got:\n%s", combined)
	}
}

func TestArgumentFailureStillPrintsUsage(t *testing.T) {
	root, out, errOut := newTestCLI(t, t.TempDir(), "")
	root.SetArgs([]string{"transcript", "create"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected an error when the audio file is missing")
	}

	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "Usage:") {
		t.Errorf("expected a usage screen for a bad invocation, got:\n%s", combined)
	}
}
