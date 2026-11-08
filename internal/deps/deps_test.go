package deps

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/alexgorbatchev/godeps"
)

// stubRunner answers a version query with output, or fails with err so that the
// program looks missing. No test may run the real ffmpeg, because what is
// installed on the machine must not decide whether the suite passes.
func stubRunner(output string, err error) godeps.CommandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if err != nil {
			return nil, err
		}
		return []byte(strings.Replace(output, "ffmpeg version", name+" version", 1)), nil
	}
}

func TestDeclaredRequiresFFmpeg(t *testing.T) {
	declared := Declared()

	if len(declared) != 2 {
		t.Fatalf("expected two declared programs, got %d", len(declared))
	}

	ffmpeg := declared[0]
	if ffmpeg.Name != FFmpeg {
		t.Errorf("expected the declared program to be %q, got %q", FFmpeg, ffmpeg.Name)
	}
	if ffmpeg.Installer == nil {
		t.Error("expected ffmpeg to declare how it is installed")
	}
	if ffmpeg.InstallURL == "" {
		t.Error("expected ffmpeg to name where it can be downloaded")
	}
	// ffmpeg answers -version, not --version, so the default would fail.
	if len(ffmpeg.VersionArgs) != 1 || ffmpeg.VersionArgs[0] != "-version" {
		t.Errorf("expected ffmpeg to be asked with -version, got %v", ffmpeg.VersionArgs)
	}
}

func TestManagerVerifiesFFmpegVersion(t *testing.T) {
	notFound := errors.New("exec: \"ffmpeg\": executable file not found in $PATH")

	tests := []struct {
		name          string
		output        string
		runErr        error
		wantSatisfied bool
		wantInstalled bool
		wantStatus    string
	}{
		{
			name:          "current release",
			output:        "ffmpeg version 7.1.1 Copyright (c) 2000-2024 the FFmpeg developers\n",
			wantSatisfied: true,
			wantInstalled: true,
			wantStatus:    "ok",
		},
		{
			name:          "too old",
			output:        "ffmpeg version 3.4.8 Copyright (c) 2000-2020 the FFmpeg developers\n",
			wantSatisfied: false,
			wantInstalled: true,
			wantStatus:    "outdated",
		},
		{
			name:          "not on PATH",
			runErr:        notFound,
			wantSatisfied: false,
			wantInstalled: false,
			wantStatus:    "not installed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(stubRunner(tt.output, tt.runErr))

			reports, err := manager.Verify(context.Background())
			if tt.wantSatisfied && err != nil {
				t.Fatalf("expected a satisfied dependency, got error: %v", err)
			}
			if !tt.wantSatisfied && err == nil {
				t.Fatal("expected an unsatisfied dependency to report an error")
			}

			report, found := Find(reports, FFmpeg)
			if !found {
				t.Fatalf("expected a report for ffmpeg, got %+v", reports)
			}
			if report.Satisfied != tt.wantSatisfied {
				t.Errorf("Satisfied = %t, want %t", report.Satisfied, tt.wantSatisfied)
			}
			if report.Installed != tt.wantInstalled {
				t.Errorf("Installed = %t, want %t", report.Installed, tt.wantInstalled)
			}
			if got := Status(report); got != tt.wantStatus {
				t.Errorf("Status = %q, want %q", got, tt.wantStatus)
			}
		})
	}
}

func TestFindReportsOnlyTheNamedProgram(t *testing.T) {
	reports := []godeps.DependencyReport{
		{Name: "something-else", Satisfied: false},
		{Name: FFmpeg, Satisfied: true},
	}

	report, found := Find(reports, FFmpeg)
	if !found || !report.Satisfied {
		t.Errorf("expected the ffmpeg report, got %+v (found %t)", report, found)
	}

	if _, found := Find(reports, "absent"); found {
		t.Error("expected no report for a program that was never declared")
	}
	if _, found := Find(nil, FFmpeg); found {
		t.Error("expected no report when nothing was checked")
	}
}

func TestInitPathAddsTheManagedDirectory(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	// Registering PATH with the test framework restores it afterwards, since
	// godeps changes it on the running process.
	t.Setenv("PATH", os.Getenv("PATH"))

	if err := InitPath(); err != nil {
		t.Fatalf("InitPath failed: %v", err)
	}

	managed := strings.Join([]string{dataHome, AppName, "bin"}, string(os.PathSeparator))
	if !strings.Contains(os.Getenv("PATH"), managed) {
		t.Errorf("expected %q on PATH, got %q", managed, os.Getenv("PATH"))
	}

	// Running twice must not stack duplicate entries onto PATH.
	before := os.Getenv("PATH")
	if err := InitPath(); err != nil {
		t.Fatalf("second InitPath failed: %v", err)
	}
	if os.Getenv("PATH") != before {
		t.Errorf("expected PATH to be unchanged on a repeat call, got %q", os.Getenv("PATH"))
	}
}

func TestParseFFmpegVersion(t *testing.T) {
	tests := []struct {
		name, banner, want string
	}{
		{"release", "ffmpeg version 7.1.1 Copyright (c) 2000-2024 the FFmpeg developers", "7.1.1"},
		{"two-part release", "ffprobe version 4.4 Copyright (c) 2007-2021 the FFmpeg developers", "4.4"},
		{"release-branch build", "ffprobe version n9.0.2-47-gb204e94e90-20261106 Copyright (c) 2007-2026 the FFmpeg developers", "9.0.2"},
		{"Debian", "ffmpeg version 7.1.1-1+b1 Copyright (c) 2000-2025 the FFmpeg developers", "7.1.1"},
		{"Alpine", "ffmpeg version 6.1.2 Copyright (c) 2000-2024 the FFmpeg developers", "6.1.2"},
		{"Ubuntu", "ffmpeg version 4.4.2-0ubuntu0.22.04.1 Copyright (c) 2000-2021 the FFmpeg developers", "4.4.2"},
		{"static build", "ffmpeg version 7.0-static https://johnvansickle.com/ffmpeg/  Copyright (c) 2000-2024 the FFmpeg developers", "7.0"},
		{"git describe on master", "ffmpeg version N-118000-g5f1c9a2e7b-20250101 Copyright (c) 2000-2025 the FFmpeg developers", "N-118000-g5f1c9a2e7b-20250101"},
		{"dated git build", "ffmpeg version 2024-01-21-git-ef4d8b5b09-full_build-www.gyan.dev Copyright (c) 2000-2024 the FFmpeg developers", "2024-01-21-git-ef4d8b5b09-full_build-www.gyan.dev"},
		{"old git build", "ffmpeg version git-2020-08-31-4a0bd1b Copyright (c) 2000-2020 the FFmpeg developers", "git-2020-08-31-4a0bd1b"},
		{"only the first line counts", "\n  ffprobe version 6.1.1-3ubuntu5 Copyright\nbuilt with gcc 13 version 13.2", "6.1.1"},
		{"no version token", "ffmpeg version", ""},
		{"unrecognized token", "ffmpeg version custom-build Copyright (c) the FFmpeg developers", ""},
		{"not a banner", "command not found", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseFFmpegVersion("ffmpeg", tt.banner); got != tt.want {
				t.Errorf("ParseFFmpegVersion(%q) = %q, want %q", tt.banner, got, tt.want)
			}
		})
	}
}

func TestManagerAcceptsRealWorldBannersForBothPrograms(t *testing.T) {
	tests := []struct {
		name, version string
		wantStatus    string
		wantError     string
	}{
		{"release-branch build", "n9.0.2-47-gb204e94e90-20261106", "ok", ""},
		{"Debian", "7.1.1-1+b1", "ok", ""},
		{"git describe on master", "N-118000-g5f1c9a2e7b", "ok", ""},
		{"dated git build", "2024-01-21-git-ef4d8b5b09-full_build-www.gyan.dev", "ok", ""},
		{"too old Ubuntu", "4.2.7-0ubuntu0.1", "outdated", "outdated"},
		{"unrecognized", "custom-build", "version unknown", "could not parse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			banner := "ffmpeg version " + tt.version + " Copyright (c) 2000-2026 the FFmpeg developers\n"
			reports, _ := NewManager(stubRunner(banner, nil)).Verify(context.Background())
			for _, name := range []string{FFmpeg, FFprobe} {
				report, found := Find(reports, name)
				if !found {
					t.Fatalf("no report for %s", name)
				}
				if got := Status(report); got != tt.wantStatus {
					t.Errorf("%s status = %q, want %q (%+v)", name, got, tt.wantStatus, report)
				}
				if (tt.wantError == "") != (report.Error == "") || !strings.Contains(report.Error, tt.wantError) {
					t.Errorf("%s error = %q, want it to mention %q", name, report.Error, tt.wantError)
				}
			}
		})
	}
}
