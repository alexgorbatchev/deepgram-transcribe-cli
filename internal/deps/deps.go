// Package deps declares the external programs this CLI needs at runtime and
// manages them through godeps.
//
// Only ffmpeg is required, and only for making a recording smaller before it is
// uploaded. Transcription still works without it, so a missing or outdated
// ffmpeg is reported rather than treated as fatal.
package deps

import (
	"github.com/alexgorbatchev/godeps"
)

const (
	// AppName names the managed directory that installed tools are kept in,
	// under $XDG_DATA_HOME or ~/.local/share.
	AppName = "deepgram-transcribe"

	// FFmpeg is the program that merges stereo to mono and cuts dead air.
	FFmpeg = "ffmpeg"

	// minFFmpegVersion is the oldest ffmpeg this project supports. 4.4 was
	// released in April 2021 and is a conservative floor: older builds may well
	// work, but nothing here checks them.
	minFFmpegVersion = "4.4"

	// ffmpegInstallURL is shown when ffmpeg cannot be installed automatically.
	ffmpegInstallURL = "https://ffmpeg.org/download.html"
)

// Declared returns the external programs this CLI depends on.
func Declared() []godeps.Dependency {
	return []godeps.Dependency{
		{
			Name:       FFmpeg,
			MinVersion: minFFmpegVersion,
			InstallURL: ffmpegInstallURL,
			// ffmpeg prints its banner for -version, not --version.
			VersionArgs: []string{"-version"},
			Installer:   godeps.SystemPackageManager(FFmpeg),
		},
	}
}

// NewManager returns the dependency manager for this CLI.
//
// runner may be nil, in which case godeps runs the real programs. Tests pass a
// stub so that no test depends on what happens to be installed on the machine.
//
// No cache is wired in deliberately. godeps caches version output without an
// expiry, so an ffmpeg upgraded outside this tool would keep being reported at
// its old version. Asking ffmpeg directly costs a few milliseconds.
func NewManager(runner godeps.CommandRunner) *godeps.Manager {
	return godeps.New(godeps.Config{
		AppName:      AppName,
		Runner:       runner,
		Dependencies: Declared(),
	})
}

// InitPath prepends the managed bin directory to this process's PATH, so that a
// tool installed by an earlier run is found by later lookups.
func InitPath() error {
	return godeps.InitManagedPath(AppName)
}

// Find returns the report for one named dependency. Callers use it to act on the
// program they actually need, rather than on whether every declared program is
// in order.
func Find(reports []godeps.DependencyReport, name string) (godeps.DependencyReport, bool) {
	for _, report := range reports {
		if report.Name == name {
			return report, true
		}
	}

	return godeps.DependencyReport{}, false
}

// Status describes a dependency in one word, for a listing column.
func Status(report godeps.DependencyReport) string {
	switch {
	case report.Satisfied:
		return "ok"
	case !report.Installed:
		return "not installed"
	default:
		return "outdated"
	}
}
