package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v3"
	"github.com/spf13/cobra"
)

const skillAlert = "ALERT: Agents must read `AGENT=1 deepgram-transcribe skill` before using this tool.\n"

const skillInstructions = `Install the deepgram-transcribe skill using the Skills CLI:

  npx skills add alexgorbatchev/deepgram-transcribe-cli --skill deepgram-transcribe

Skill:

  https://github.com/alexgorbatchev/deepgram-transcribe-cli/tree/main/skills/deepgram-transcribe
`

// repositoryRoot is where the skills/ directory lives, relative to this package.
var repositoryRoot = filepath.Join("..", "..")

var skillPath = filepath.Join(repositoryRoot, "skills", "deepgram-transcribe", "SKILL.md")

func readSkill(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSkillPrintsInstallInstructionsOffline(t *testing.T) {
	for _, mode := range []string{"", "0", "1", "true", "yes"} {
		t.Run("AGENT="+mode, func(t *testing.T) {
			root, out, stderr := newTestCLI(t, t.TempDir(), "")
			t.Setenv("AGENT", mode)
			// No repository files are reachable from here, so the output cannot
			// depend on reading SKILL.md at runtime.
			t.Chdir(t.TempDir())
			root.SetArgs([]string{"skill"})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if out.String() != skillInstructions || stderr.Len() != 0 {
				t.Fatalf("stdout %q stderr %q", out, stderr)
			}
		})
	}
}

func TestSkillRejectsExtraArguments(t *testing.T) {
	for _, mode := range []string{"", "1"} {
		t.Run("AGENT="+mode, func(t *testing.T) {
			root, out, _ := newTestCLI(t, t.TempDir(), "")
			t.Setenv("AGENT", mode)
			root.SetArgs([]string{"skill", "extra"})
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), `unknown command "extra"`) {
				t.Fatalf("got %v, want extra argument rejected", err)
			}
			if strings.Contains(out.String(), "npx skills add") {
				t.Fatalf("rejected invocation printed instructions: %q", out)
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

func TestSkillPointerMatchesRepositorySkill(t *testing.T) {
	const prefix = "https://github.com/alexgorbatchev/deepgram-transcribe-cli/tree/main/"
	_, dir, found := strings.Cut(skillInstructions, prefix)
	if !found {
		t.Fatalf("skill URL missing from %q", skillInstructions)
	}
	printed := filepath.Join(repositoryRoot, strings.TrimSpace(dir), "SKILL.md")
	if printed != skillPath {
		t.Fatalf("printed skill path %s does not point at %s", printed, skillPath)
	}
	frontmatter, _, _ := strings.Cut(strings.TrimPrefix(readSkill(t), "---\n"), "\n---\n")
	if !strings.Contains("\n"+frontmatter+"\n", "\nname: "+rootCommandName+"\n") ||
		!strings.Contains(skillInstructions, "--skill "+rootCommandName+"\n") {
		t.Fatalf("SKILL.md frontmatter name does not match the printed --skill value:\n%s", frontmatter)
	}
}

func TestSkillDocumentsEveryCommandAndFlag(t *testing.T) {
	root, _, _ := newTestCLI(t, t.TempDir(), "")
	if missing := cobrahelptree.SkillOmissions(root, helpCatalog(), readSkill(t)); len(missing) > 0 {
		t.Fatalf("SKILL.md omits:\n%s", strings.Join(missing, "\n"))
	}
}

// SkillOmissions checks names, types and defaults but not the argument
// placeholders each command's Use declares, so the documented invocation is
// compared with the live tree here.
func TestSkillDocumentsEveryInvocation(t *testing.T) {
	doc := readSkill(t)
	root, _, _ := newTestCLI(t, t.TempDir(), "")
	for _, cmd := range liveCommands(root) {
		_, section, found := strings.Cut(doc, "### `"+cmd.CommandPath()+"`\n")
		if !found {
			t.Errorf("missing section for %s", cmd.CommandPath())
			continue
		}
		section, _, _ = strings.Cut(section, "\n### ")
		invocation := cmd.Use
		if parent := cmd.Parent(); parent != nil {
			invocation = parent.CommandPath() + " " + cmd.Use
		}
		if !strings.Contains(section, "Invocation: `"+invocation+"`") {
			t.Errorf("%s missing invocation %q", cmd.CommandPath(), invocation)
		}
	}
}

func TestEveryAgentHelpScreenOpensWithSkillAlert(t *testing.T) {
	root, _, _ := newTestCLI(t, t.TempDir(), "")
	commands := liveCommands(root)
	if len(commands) < 20 {
		t.Fatalf("walked only %d commands", len(commands))
	}
	for _, cmd := range commands {
		path := strings.Fields(cmd.CommandPath())[1:]
		for _, mode := range []string{"1", "true", "yes", "", "0"} {
			t.Run(strings.Join(append([]string{"AGENT=" + mode}, path...), "/"), func(t *testing.T) {
				root, out, _ := newTestCLI(t, t.TempDir(), "")
				t.Setenv("AGENT", mode)
				root.SetArgs(append(path, "--help"))
				if err := root.Execute(); err != nil {
					t.Fatal(err)
				}
				agent := mode != "" && mode != "0"
				if hasAlert := strings.HasPrefix(out.String(), skillAlert); hasAlert != agent {
					t.Fatalf("alert present = %v, want %v:\n%.300s", hasAlert, agent, out)
				}
			})
		}
	}
}

// Usage after an argument error is a diagnostic on the process's real stderr,
// which buffer writers would hide, so the streams are captured directly.
func TestAgentUsageAfterArgumentErrorOpensWithSkillAlert(t *testing.T) {
	t.Setenv(apiKeyEnvVar, "")
	t.Setenv("AGENT", "1")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("PATH", os.Getenv("PATH"))
	root := newRootCmd(&globalOptions{defaultCacheDir: t.TempDir()})
	root.SetArgs([]string{"transcript", "create"})

	var execErr error
	stdout, stderr := captureProcessStreams(t, func() { execErr = root.Execute() })
	if execErr == nil {
		t.Fatal("expected a missing-argument error")
	}
	if stdout != "" || !strings.HasPrefix(stderr, skillAlert) {
		t.Fatalf("stdout %q; stderr does not open with the alert:\n%.300s", stdout, stderr)
	}
}

// liveCommands returns every command the binary accepts, including cobra's
// generated help and completion commands, which only exist once the CLI runs.
func liveCommands(root *cobra.Command) []*cobra.Command {
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	var commands []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		commands = append(commands, cmd)
		for _, child := range cmd.Commands() {
			if !child.Hidden {
				walk(child)
			}
		}
	}
	walk(root)
	return commands
}

// A second setup would shadow the skill command already on the tree; that is a
// wiring mistake, so it must stop the program rather than pass silently.
func TestSetTreeHelpRejectsADuplicateSkillCommand(t *testing.T) {
	root, _, _ := newTestCLI(t, t.TempDir(), "")
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "already has a skill command") {
			t.Fatalf("got %v, want a panic naming the duplicate skill command", r)
		}
	}()
	setTreeHelp(root)
}
