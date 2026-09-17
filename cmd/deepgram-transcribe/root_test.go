package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/internal/cliout"
)

// newTestCLI builds a root command wired to buffers and to a stub Deepgram
// endpoint, with the environment neutralised so that a developer's own API key,
// agent mode or terminal width cannot change the result.
func newTestCLI(t *testing.T, cacheDir, endpoint string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	t.Setenv(apiKeyEnvVar, "")
	t.Setenv("AGENT", "")
	t.Setenv("COLUMNS", "200")

	var out, errOut bytes.Buffer
	root := newRootCmd(&globalOptions{defaultCacheDir: cacheDir, endpoint: endpoint})
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
