package audio

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/timeline"
)

// TrimMode selects how the parts of a recording worth uploading are found.
type TrimMode string

const (
	// TrimVAD keeps what a speech model hears as speech.
	TrimVAD TrimMode = "vad"
	// TrimSilence keeps everything louder than a fixed volume threshold.
	TrimSilence TrimMode = "silence"
	// TrimNone uploads the whole recording.
	TrimNone TrimMode = "none"
)

// Cuts reports whether the mode may remove audio. The zero value cuts nothing.
func (m TrimMode) Cuts() bool { return m != "" && m != TrimNone }

// TrimModes lists every mode, in the order help text presents them.
var TrimModes = []TrimMode{TrimVAD, TrimSilence, TrimNone}

// ParseTrimMode returns the mode a flag value names.
func ParseTrimMode(value string) (TrimMode, error) {
	mode := TrimMode(strings.ToLower(strings.TrimSpace(value)))
	if slices.Contains(TrimModes, mode) {
		return mode, nil
	}
	return "", fmt.Errorf("unknown trim mode %q; use vad, silence or none", value)
}

// DefaultTrimPadding is how many seconds of audio are kept on each side of a
// detected speech range, so the first and last syllables are not clipped.
const DefaultTrimPadding = 0.3

// These begin the metadata lines that ffmpeg's silencedetect filter writes
// through ametadata.
const (
	silenceStartPrefix = "lavfi.silence_start="
	silenceEndPrefix   = "lavfi.silence_end="
)

// parseSilences reads the silence ranges that silencedetect reported through
// ametadata. A silence still open when the audio ends has an infinite end.
func parseSilences(output []byte) ([]timeline.Range, error) {
	var silences []timeline.Range
	open := math.NaN()

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, silenceStartPrefix):
			value, err := strconv.ParseFloat(strings.TrimPrefix(line, silenceStartPrefix), 64)
			if err != nil {
				return nil, fmt.Errorf("reading silence start %q: %w", line, err)
			}
			open = math.Max(value, 0)
		case strings.HasPrefix(line, silenceEndPrefix):
			value, err := strconv.ParseFloat(strings.TrimPrefix(line, silenceEndPrefix), 64)
			if err != nil {
				return nil, fmt.Errorf("reading silence end %q: %w", line, err)
			}
			if !math.IsNaN(open) {
				silences = append(silences, timeline.Range{Start: open, End: value})
				open = math.NaN()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading silence detection output: %w", err)
	}
	if !math.IsNaN(open) {
		silences = append(silences, timeline.Range{Start: open, End: math.Inf(1)})
	}
	return silences, nil
}

// complement returns the parts of the audio that no silence covers. The last
// range is open-ended: it runs to the end of the decoded audio, whose length
// is only known once it has been decoded. ffprobe's duration is not used,
// because for containers without a duration index it is an estimate that can
// fall short of the real audio and would cut its tail.
func complement(silences []timeline.Range) []timeline.Range {
	var kept []timeline.Range
	cursor := 0.0
	for _, s := range silences {
		if s.Start > cursor {
			kept = append(kept, timeline.Range{Start: cursor, End: s.Start})
		}
		cursor = math.Max(cursor, s.End)
	}
	if !math.IsInf(cursor, 1) {
		kept = append(kept, timeline.Range{Start: cursor, End: math.Inf(1)})
	}
	return kept
}

// sampleSpan is a range of samples, end exclusive.
type sampleSpan struct{ start, end int64 }

// toSampleSpans widens each range by padding seconds on both sides, converts it
// to whole samples, clamps it to the audio and merges ranges that touch.
// Rounding outward means a span always contains the range it came from. Ends
// may be infinite; clamping happens before conversion to integers.
func toSampleSpans(ranges []timeline.Range, padding float64, sampleRate int, totalSamples int64) []sampleSpan {
	rate := float64(sampleRate)
	total := float64(totalSamples)
	var spans []sampleSpan
	for _, r := range ranges {
		start := int64(math.Max(math.Floor((r.Start-padding)*rate), 0))
		end := int64(math.Min(math.Ceil((r.End+padding)*rate), total))
		if end <= start {
			continue
		}
		if n := len(spans); n > 0 && start <= spans[n-1].end {
			spans[n-1].end = max(spans[n-1].end, end)
			continue
		}
		spans = append(spans, sampleSpan{start: start, end: end})
	}
	return spans
}

// spansToRanges converts sample spans back to seconds of the original audio.
func spansToRanges(spans []sampleSpan, sampleRate int) []timeline.Range {
	ranges := make([]timeline.Range, len(spans))
	for i, s := range spans {
		ranges[i] = timeline.Range{
			Start: float64(s.start) / float64(sampleRate),
			End:   float64(s.end) / float64(sampleRate),
		}
	}
	return ranges
}

// cutPCM copies the sample spans of raw interleaved PCM from src to dst, end to
// end. Working on decoded samples rather than on encoded packets is what makes
// the kept ranges exact.
func cutPCM(srcPath, dstPath string, spans []sampleSpan, frameBytes int64) (err error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening decoded audio: %w", err)
	}
	defer func() { _ = src.Close() }() // Read-only; a close error loses nothing.

	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("creating trimmed audio: %w", err)
	}
	defer func() {
		if closeErr := dst.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("closing trimmed audio: %w", closeErr)
		}
	}()

	for _, s := range spans {
		section := io.NewSectionReader(src, s.start*frameBytes, (s.end-s.start)*frameBytes)
		if _, err := io.Copy(dst, section); err != nil {
			return fmt.Errorf("copying kept audio: %w", err)
		}
	}
	return nil
}
