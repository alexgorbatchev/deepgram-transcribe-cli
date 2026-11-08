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
	Duration   float64 // seconds; zero when ffprobe cannot tell, as for some streamed containers
	SampleRate int     // samples per second
	Channels   int
}

// Probe reads the first audio stream's duration, sample rate and channel count
// using ffprobe. The duration is the stream's, or the container's when the
// stream does not state one, and zero when neither is known: trimming counts
// decoded samples and does not need it. A duration that is stated but invalid,
// a missing sample rate and a missing channel count are errors rather than
// evidence of shorter audio.
func Probe(ctx context.Context, path string, runner godeps.CommandRunner) (StreamInfo, error) {
	if runner == nil {
		runner = godeps.DefaultRunner
	}
	output, err := run(ctx, runner, "ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=duration,sample_rate,channels:format=duration", "-of", "json", "-i", path)
	if err != nil {
		return StreamInfo{}, fmt.Errorf("probing audio stream: %w", err)
	}
	var probe struct {
		Streams []struct {
			Duration   string `json:"duration"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &probe); err != nil {
		return StreamInfo{}, fmt.Errorf("decoding audio stream details: %w", err)
	}
	if len(probe.Streams) != 1 {
		return StreamInfo{}, fmt.Errorf("audio stream details are unavailable for %q", path)
	}
	stream := probe.Streams[0]
	duration, err := parseDuration(stream.Duration, probe.Format.Duration)
	if err != nil {
		return StreamInfo{}, fmt.Errorf("%w for %q", err, path)
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

// parseDuration returns the first duration ffprobe states, in seconds, or zero
// when it states none. ffprobe reports an unknown duration as "N/A" or leaves
// the field out.
func parseDuration(candidates ...string) (float64, error) {
	for _, value := range candidates {
		if value == "" || value == "N/A" {
			continue
		}
		duration, err := strconv.ParseFloat(value, 64)
		if err != nil || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
			return 0, fmt.Errorf("invalid audio duration %q", value)
		}
		return duration, nil
	}
	return 0, nil
}
