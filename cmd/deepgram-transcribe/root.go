package main

import (
	"os"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree"
	"github.com/alexgorbatchev/godeps"
	"github.com/spf13/cobra"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/internal/deps"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
)

// version is replaced at build time by GoReleaser (-X main.version={{.Version}}).
var version = "dev"

// apiKeyEnvVar is the environment variable consulted when --api-key is not given.
const apiKeyEnvVar = "DEEPGRAM_API_KEY"

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
// cache clear.
func newRootCmd(g *globalOptions) *cobra.Command {
	root := &cobra.Command{
		Use:   "deepgram-transcribe",
		Short: "Turn call recordings into readable transcripts",
		Long: `deepgram-transcribe turns a recorded call into a readable Markdown transcript,
labelled with who is speaking and when they spoke.

It keeps a copy of every transcript it makes, so asking for the same recording
twice is free, and it can tell you what each recording cost to transcribe.`,
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
	flags.StringVar(&g.cacheDir, "cache-dir", "", "Folder that saved transcripts are kept in")
	flags.StringVar(&g.apiKey, "api-key", "", "Deepgram API key (defaults to the DEEPGRAM_API_KEY environment variable)")

	root.AddCommand(
		newCacheCmd(g),
		newDependencyCmd(g),
		newJobCmd(g),
		newTranscriptCmd(g),
	)

	// Render every help screen as an aligned command tree, trimmed to the
	// terminal width, and as compact key-value text when AGENT=1.
	cobrahelptree.Setup(root)

	return root
}
