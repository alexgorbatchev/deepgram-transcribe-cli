package audio

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/alexgorbatchev/godeps"
)

// Duration reads the first audio stream's duration in seconds using ffprobe.
// Missing or invalid durations are errors rather than evidence of shorter audio.
func Duration(ctx context.Context, path string, runner godeps.CommandRunner) (float64, error) {
	if runner == nil {
		runner = godeps.DefaultRunner
	}
	output, err := runner(ctx, "ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=duration", "-of", "json", "-i", path)
	if err != nil {
		return 0, fmt.Errorf("probing audio duration: %w", err)
	}
	var probe struct {
		Streams []struct {
			Duration string `json:"duration"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &probe); err != nil {
		return 0, fmt.Errorf("decoding audio duration: %w", err)
	}
	if len(probe.Streams) != 1 {
		return 0, fmt.Errorf("audio duration is unavailable for %q", path)
	}
	duration, err := strconv.ParseFloat(probe.Streams[0].Duration, 64)
	if err != nil || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return 0, fmt.Errorf("invalid audio duration %q for %q", probe.Streams[0].Duration, path)
	}
	return duration, nil
}
