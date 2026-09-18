package main

import (
	"bytes"
	"context"
	"errors"
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
