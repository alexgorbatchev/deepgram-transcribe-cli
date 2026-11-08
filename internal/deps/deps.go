// Package deps declares the external programs this CLI needs at runtime and
// manages them through godeps.
//
// ffmpeg prepares recordings and ffprobe measures their duration before upload.
// Transcription still works without them, so a missing or outdated
// ffmpeg is reported rather than treated as fatal.
package deps

import (
	"regexp"
	"strings"

	"github.com/alexgorbatchev/godeps"
)

const (
	// AppName names the managed directory that installed tools are kept in,
	// under $XDG_DATA_HOME or ~/.local/share.
	AppName = "deepgram-transcribe"

	// FFmpeg is the program that merges stereo to mono and cuts dead air.
	FFmpeg = "ffmpeg"
	// FFprobe measures audio duration before selecting a prepared upload.
	FFprobe = "ffprobe"

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
			VersionArgs:  []string{"-version"},
			ParseVersion: ParseFFmpegVersion,
			Installer:    godeps.SystemPackageManager(FFmpeg),
		},
		{
			Name:         FFprobe,
			MinVersion:   minFFmpegVersion,
			InstallURL:   ffmpegInstallURL,
			VersionArgs:  []string{"-version"},
			ParseVersion: ParseFFmpegVersion,
			Installer:    godeps.SystemPackageManager(FFmpeg),
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

var (
	// ffmpegBanner captures the version token of the first line that ffmpeg
	// and ffprobe print for -version.
	ffmpegBanner = regexp.MustCompile(`^\S+ version (\S+)`)
	// releaseVersion matches a release number at the start of a token:
	// "7.1.1", Debian's "7.1.1-1+b1", Ubuntu's "6.1.1-3ubuntu5", the
	// release-branch builds' "n9.0.2-47-gb204e94e90-20261106" and static
	// builds' "7.0-static".
	releaseVersion = regexp.MustCompile(`^n?(\d+\.\d+(?:\.\d+)?)(?:[^\d.]|$)`)
	// developmentBuild matches builds from the master branch, which carry no
	// release number: git-describe "N-118000-g5f1c9a2e7b", the older
	// "git-2020-08-31-4a0bd1b" and dated "2024-01-21-git-ef4d8b5b09-full_build".
	developmentBuild = regexp.MustCompile(`^(?:N-\d+-g[0-9a-f]+|git-\S+|\d{4}-\d{2}-\d{2}-git-\S+)`)
)

// ParseFFmpegVersion extracts the version from the -version banner of ffmpeg
// or ffprobe. It returns a release number such as "7.1.1", the token itself
// for a development build, which godeps accepts as current, or an empty string
// when the banner is unrecognizable, which godeps reports as an error.
func ParseFFmpegVersion(_, output string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(output), "\n")
	banner := ffmpegBanner.FindStringSubmatch(strings.TrimSpace(line))
	if banner == nil {
		return ""
	}
	token := banner[1]
	if release := releaseVersion.FindStringSubmatch(token); release != nil {
		return release[1]
	}
	if developmentBuild.MatchString(token) {
		return token
	}
	return ""
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
	case report.DetectedVersion == "":
		return "version unknown"
	default:
		return "outdated"
	}
}
