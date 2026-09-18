package main

import (
	"strings"
	"testing"
)

func TestDependencyListReportsEachProgram(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		runErr     error
		wantStatus string
		wantShown  string
	}{
		{
			name:       "ready",
			output:     ffmpegVersionOutput,
			wantStatus: "ok",
			wantShown:  "7.1.1",
		},
		{
			name:       "too old",
			output:     "ffmpeg version 3.4.8 Copyright (c) 2000-2020 the FFmpeg developers\n",
			wantStatus: "outdated",
			wantShown:  "3.4.8",
		},
		{
			name:       "not installed",
			runErr:     errFFmpegMissing,
			wantStatus: "not installed",
			wantShown:  "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, out, _ := newTestCLIWithRunner(t, t.TempDir(), "", stubRunner(tt.output, tt.runErr))
			root.SetArgs([]string{"dependency", "list"})

			// A program that is missing is what this command exists to show, so
			// it must not be reported as a command failure.
			if err := root.Execute(); err != nil {
				t.Fatalf("dependency list failed: %v", err)
			}

			listing := out.String()
			for _, want := range []string{"ffmpeg", tt.wantStatus, tt.wantShown} {
				if !strings.Contains(listing, want) {
					t.Errorf("expected %q in the listing, got:\n%s", want, listing)
				}
			}
		})
	}
}

func TestDependencyListIsTabSeparatedForAgents(t *testing.T) {
	root, out, _ := newTestCLIWithRunner(t, t.TempDir(), "", stubRunner(ffmpegVersionOutput, nil))
	t.Setenv("AGENT", "1")
	root.SetArgs([]string{"dependency", "list"})

	if err := root.Execute(); err != nil {
		t.Fatalf("dependency list failed: %v", err)
	}

	listing := out.String()
	if !strings.Contains(listing, "program\tneeds\tinstalled\tstatus") {
		t.Errorf("expected a tab separated header, got:\n%s", listing)
	}
	if strings.ContainsAny(listing, "│┌└├") {
		t.Errorf("expected no table borders in agent mode, got:\n%s", listing)
	}
}

// TestDependencyInstallSkipsWhatIsAlreadyThere covers the only branch of install
// that is safe to exercise. The package-manager installer shells out to brew or
// apt with a runner that cannot be replaced from outside godeps, so a test must
// never reach a branch that would actually install anything.
func TestDependencyInstallSkipsWhatIsAlreadyThere(t *testing.T) {
	root, _, errOut := newTestCLIWithRunner(t, t.TempDir(), "", stubRunner(ffmpegVersionOutput, nil))
	root.SetArgs([]string{"dependency", "install"})

	if err := root.Execute(); err != nil {
		t.Fatalf("dependency install failed: %v", err)
	}

	if !strings.Contains(errOut.String(), "already installed") {
		t.Errorf("expected install to report that nothing was needed, got: %s", errOut.String())
	}
}
