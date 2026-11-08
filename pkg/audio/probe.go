package audio

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/alexgorbatchev/godeps"
)

// StreamInfo describes the first audio stream of a file.
type StreamInfo struct {
	Duration   float64 // seconds
	SampleRate int     // samples per second
	Channels   int
}

// Probe reads the first audio stream's duration, sample rate and channel count
// using ffprobe. Missing or invalid values are errors rather than evidence of
// shorter audio.
func Probe(ctx context.Context, path string, runner godeps.CommandRunner) (StreamInfo, error) {
	if runner == nil {
		runner = godeps.DefaultRunner
	}
	output, err := runner(ctx, "ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=duration,sample_rate,channels", "-of", "json", "-i", path)
	if err != nil {
		return StreamInfo{}, fmt.Errorf("probing audio stream: %w", err)
	}
	var probe struct {
		Streams []struct {
			Duration   string `json:"duration"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &probe); err != nil {
		return StreamInfo{}, fmt.Errorf("decoding audio stream details: %w", err)
	}
	if len(probe.Streams) != 1 {
		return StreamInfo{}, fmt.Errorf("audio stream details are unavailable for %q", path)
	}
	stream := probe.Streams[0]
	duration, err := strconv.ParseFloat(stream.Duration, 64)
	if err != nil || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return StreamInfo{}, fmt.Errorf("invalid audio duration %q for %q", stream.Duration, path)
	}
	sampleRate, err := strconv.Atoi(stream.SampleRate)
	if err != nil || sampleRate <= 0 {
		return StreamInfo{}, fmt.Errorf("invalid audio sample rate %q for %q", stream.SampleRate, path)
	}
	if stream.Channels <= 0 {
		return StreamInfo{}, fmt.Errorf("invalid audio channel count %d for %q", stream.Channels, path)
	}
	return StreamInfo{Duration: duration, SampleRate: sampleRate, Channels: stream.Channels}, nil
}
