package main

import (
	"fmt"
	"os"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v3"
	"github.com/alexgorbatchev/godeps"
	"github.com/spf13/cobra"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/internal/deps"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
)

// version is replaced at build time by GoReleaser (-X main.version={{.Version}}).
var version = "dev"

const (
	// apiKeyEnvVar is the environment variable consulted when --api-key is not given.
	apiKeyEnvVar = "DEEPGRAM_API_KEY"

	// rootCommandName is the binary's name. The help catalog is keyed on full
	// command paths, which start with it, so both come from here and cannot drift
	// apart.
	rootCommandName = "deepgram-transcribe"
)

// skillSource is where the agent skill is published: skills/deepgram-transcribe/SKILL.md
// on the repository's default branch. cobra-help-tree turns it into the `skill`
// command and the alert that opens every agent help screen.
var skillSource = cobrahelptree.SkillSource{Repository: "alexgorbatchev/deepgram-transcribe-cli", Branch: "main"}

// globalOptions holds the settings that every subcommand shares. They are bound
// to persistent flags on the root command so that a transcript saved to a custom
// folder can also be inspected, listed and cleared from that same folder.
type globalOptions struct {
	cacheDir string
	apiKey   string

	// defaultCacheDir is used when --cache-dir is not given.
	defaultCacheDir string

	// endpoint redirects Deepgram calls to a stub server. It is set only by
	// tests, which must never reach the real service.
	endpoint string

	// depsRunner stands in for running the external programs this CLI depends
	// on. It is set only by tests, so that no test depends on what happens to be
	// installed on the machine running it.
	depsRunner godeps.CommandRunner
}

// newDependencyManager returns the manager for the external programs this CLI
// needs, such as ffmpeg.
func (g *globalOptions) newDependencyManager() *godeps.Manager {
	return deps.NewManager(g.depsRunner)
}

// resolvedCacheDir returns the folder that transcripts are read from and saved to.
func (g *globalOptions) resolvedCacheDir() string {
	if g.cacheDir != "" {
		return g.cacheDir
	}
	return g.defaultCacheDir
}

// resolvedAPIKey returns the API key from the flag, falling back to the
// environment.
func (g *globalOptions) resolvedAPIKey() string {
	if g.apiKey != "" {
		return g.apiKey
	}
	return os.Getenv(apiKeyEnvVar)
}

// newClient builds a Deepgram client, or returns nil when no API key is
// available. Listing and inspecting past work still succeeds without a key, so
// callers treat a nil client as "cannot ask Deepgram" rather than as an error.
func (g *globalOptions) newClient() *deepgram.Client {
	key := g.resolvedAPIKey()
	if key == "" {
		return nil
	}

	var opts []deepgram.ClientOption
	if g.endpoint != "" {
		opts = append(opts, deepgram.WithEndpoint(g.endpoint))
	}

	return deepgram.NewClient(key, opts...)
}

// newRootCmd assembles the command tree. Subjects sit at the top level and their
// verbs nest underneath: transcript create, job list, job inspect, cache status,
// cache clear. The top-level skill command comes from setTreeHelp.
func newRootCmd(g *globalOptions) *cobra.Command {
	root := &cobra.Command{
		Use:   rootCommandName,
		Short: "Transcribe speech in an audio file to Markdown",
		Long: `deepgram-transcribe turns any audio containing speech into a readable Markdown
transcript, labelled with who is speaking and when they spoke.

Responses are cached locally and reused on repeated requests. Use --force for a
fresh transcription, which may be billed again. Job commands report request costs.`,
		Version:       version,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Arguments and flags have already been validated by this point, so
			// anything that fails from here on is a runtime problem that the
			// usage screen would only bury.
			cmd.SilenceUsage = true

			// Put the managed bin directory on PATH so that a program this tool
			// installed earlier is found. Failing to do so only means the
			// managed copy is not visible, which every command reports in its
			// own terms, so it is not worth stopping for here.
			_ = deps.InitPath()
		},
	}

	// Scripts read --version, so it prints the bare version and nothing else.
	root.SetVersionTemplate("{{.Version}}\n")

	flags := root.PersistentFlags()
	flags.StringVar(&g.cacheDir, "cache-dir", "", "Directory for the local response cache")
	flags.StringVar(&g.apiKey, "api-key", "", "Deepgram API key (defaults to the DEEPGRAM_API_KEY environment variable)")

	root.AddCommand(
		newCacheCmd(g),
		newDependencyCmd(g),
		newJobCmd(g),
		newTranscriptCmd(g),
	)

	setTreeHelp(root)

	return root
}

// setTreeHelp renders every help and usage screen as an aligned command tree,
// trimmed to the terminal width, and as compact key-value text when AGENT=1. It
// also adds the skill command, which prints how to install the agent skill, and
// opens every agent help screen with an alert pointing at it.
//
// Generated commands are hidden because the tree exists to be read: cobra's
// completion command arrives with four shell children, so keeping it would cost
// five lines above the first command this CLI actually defines. The skill command
// counts as generated, since agents are sent to it by the alert. Agent mode still
// lists both, since there the contract is a full description of the interface.
func setTreeHelp(root *cobra.Command) {
	err := cobrahelptree.SetupWithOptions(root, cobrahelptree.HelpOptions{
		Catalog: helpCatalog(),
		Skill:   skillSource,
		Tree:    cobrahelptree.TreeOptions{HideGeneratedCommands: true},
	})
	if err != nil {
		// The command is the freshly built root and the options are static, so a
		// failure is a programming error.
		panic(fmt.Sprintf("configuring help: %v", err))
	}
}

// helpCatalog describes what each positional argument means.
//
// Cobra has nowhere to put this: Use carries argument names as free text and
// ValidArgs is an enum of accepted values, so without a catalog a reader sees
// <audio-file|request-id> and has to guess what either half accepts. Both help
// modes render these, under "Arguments:" and "args:" respectively.
//
// Only commands that take positional arguments appear here. Everything else the
// renderers need, the summary and the description, they already read from the
// command's own Short and Long.
func helpCatalog() cobrahelptree.TechCatalog {
	return cobrahelptree.TechCatalog{
		rootCommandName + " transcript create": {
			Args: []cobrahelptree.ArgSpec{{
				Name:        "<audio-file>",
				Description: "Audio to transcribe: mp3, m4a, mp4, wav, flac, ogg or aac",
			}},
		},
		rootCommandName + " job inspect": {
			Args: []cobrahelptree.ArgSpec{{
				Name:        "<audio-file|request-id>",
				Description: "An audio file, matched by its contents, or a request ID from `job list`",
			}},
		},
	}
}
