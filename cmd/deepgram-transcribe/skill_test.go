package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestSkillPrintsMaintainedReferenceOffline(t *testing.T) {
	want, err := os.ReadFile("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"", "0", "1", "true", "yes"} {
		t.Run("AGENT="+mode, func(t *testing.T) {
			root, out, stderr := newTestCLI(t, t.TempDir(), "")
			t.Setenv("AGENT", mode)
			t.Chdir(t.TempDir())
			root.SetArgs([]string{"skill"})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out.Bytes(), want) || stderr.Len() != 0 {
				t.Fatalf("skill output differs or wrote diagnostics: %s", stderr)
			}
		})
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestSkillReturnsWriteFailure(t *testing.T) {
	root, _, _ := newTestCLI(t, t.TempDir(), "")
	want := errors.New("output unavailable")
	root.SetOut(failingWriter{want})
	root.SetArgs([]string{"skill"})
	if err := root.Execute(); !errors.Is(err, want) {
		t.Fatalf("got %v, want write failure", err)
	}
}

func TestAgentHelpStartsWithSkillAlert(t *testing.T) {
	paths := [][]string{
		{}, {"cache"}, {"cache", "status"}, {"cache", "clear"},
		{"dependency"}, {"dependency", "list"}, {"dependency", "install"}, {"dependency", "update"},
		{"job"}, {"job", "list"}, {"job", "inspect"},
		{"transcript"}, {"transcript", "create"}, {"skill"}, {"help"},
		{"completion"}, {"completion", "bash"}, {"completion", "zsh"},
		{"completion", "fish"}, {"completion", "powershell"},
	}
	alert := "ALERT: Agents must read `AGENT=1 deepgram-transcribe skill` before using this tool.\n"
	for _, mode := range []string{"1", "true", "yes"} {
		for _, path := range paths {
			t.Run(mode+"/"+strings.Join(path, "/"), func(t *testing.T) {
				root, out, _ := newTestCLI(t, t.TempDir(), "")
				t.Setenv("AGENT", mode)
				root.SetArgs(append(path, "--help"))
				if err := root.Execute(); err != nil {
					t.Fatal(err)
				}
				if !strings.HasPrefix(out.String(), alert) {
					t.Fatalf("missing opening alert: %s", out)
				}
			})
		}
	}
}

// Compare documentation to the framework's initialized live interface so adding
// a command or changing flag type, alias or default cannot silently drift.
func TestSkillDocumentsLiveInterface(t *testing.T) {
	doc, err := os.ReadFile("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	root, _, _ := newTestCLI(t, t.TempDir(), "")
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		cmd.InitDefaultHelpFlag()
		cmd.InitDefaultVersionFlag()
		heading := "### `" + cmd.CommandPath() + "`\n"
		_, section, found := strings.Cut(string(doc), heading)
		if !found {
			t.Errorf("missing command %s", cmd.CommandPath())
		} else {
			section, _, _ = strings.Cut(section, "\n### ")
			invocation := cmd.Use
			if parent := cmd.Parent(); parent != nil {
				invocation = parent.CommandPath() + " " + cmd.Use
			}
			if !strings.Contains(section, "Invocation: `"+invocation+"`") {
				t.Errorf("%s missing argument signature %q", cmd.CommandPath(), invocation)
			}
			cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
				row := "| `--" + flag.Name + "` | `" + flag.Shorthand + "` | `" + flag.Value.Type() + "` | `" + flag.DefValue + "` |"
				if !strings.Contains(section, row) {
					t.Errorf("%s missing flag metadata %s", cmd.CommandPath(), row)
				}
			})
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
}
