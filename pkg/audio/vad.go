package audio

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/zserge/govad"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/timeline"
)

const (
	// DefaultVADThreshold is the speech probability at or above which a frame
	// counts as speech. It is Silero VAD's own default.
	DefaultVADThreshold = 0.5
	// DefaultVADMinSpeech drops detected speech shorter than this many seconds,
	// such as clicks and coughs.
	DefaultVADMinSpeech = 0.25
	// DefaultVADMinSilence is how many seconds of non-speech end a speech range.
	// Shorter pauses stay inside the range, so sentences are not chopped apart.
	DefaultVADMinSilence = 1.0

	// vadSampleRate is the only rate the Silero model accepts.
	vadSampleRate = 16000
	// vadNegativeMargin is how far below the threshold a frame's probability
	// must fall before it counts as silence, as in Silero's reference
	// segmentation. The gap stops a probability hovering at the threshold from
	// flickering between speech and silence.
	vadNegativeMargin = 0.15
	// vadMinNegativeThreshold keeps the silence threshold above zero for very
	// low speech thresholds.
	vadMinNegativeThreshold = 0.01
	// pcmSampleScale converts signed 16-bit samples to the [-1, 1] range the
	// model expects.
	pcmSampleScale = 32768
)

// VADOptions tunes speech detection.
type VADOptions struct {
	Threshold  float64 // speech probability in (0, 1)
	MinSpeech  float64 // seconds
	MinSilence float64 // seconds
}

// Validate reports options that cannot describe a detection.
func (o VADOptions) Validate() error {
	if o.Threshold <= 0 || o.Threshold >= 1 || math.IsNaN(o.Threshold) {
		return fmt.Errorf("speech threshold %v is outside (0, 1)", o.Threshold)
	}
	if o.MinSpeech < 0 || math.IsNaN(o.MinSpeech) {
		return fmt.Errorf("minimum speech duration %v is negative", o.MinSpeech)
	}
	if o.MinSilence < 0 || math.IsNaN(o.MinSilence) {
		return fmt.Errorf("minimum silence duration %v is negative", o.MinSilence)
	}
	return nil
}

// detectSpeech runs the Silero VAD model over 16 kHz mono signed 16-bit
// little-endian PCM and returns the speech ranges, in seconds, before padding.
//
// The segmentation follows Silero's reference get_speech_timestamps: speech
// starts at the first frame at or above the threshold, and ends where frames
// drop below threshold minus vadNegativeMargin for at least MinSilence.
func detectSpeech(pcm io.Reader, opts VADOptions) ([]timeline.Range, error) {
	model, err := govad.New()
	if err != nil {
		return nil, fmt.Errorf("loading the voice activity model: %w", err)
	}

	seg := newSegmenter(opts)
	reader := bufio.NewReader(pcm)
	raw := make([]byte, govad.SamplesPerFrame*2)
	frame := make([]float32, govad.SamplesPerFrame)
	position := 0

	for {
		n, err := io.ReadFull(reader, raw)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("reading audio for voice detection: %w", err)
		}

		samples := n / 2
		for i := range frame {
			frame[i] = 0
			if i < samples {
				frame[i] = float32(int16(binary.LittleEndian.Uint16(raw[i*2:]))) / pcmSampleScale
			}
		}
		seg.add(position, model.Process(frame))
		position += samples
	}

	return seg.finish(position), nil
}

// segmenter turns per-frame speech probabilities into speech ranges.
type segmenter struct {
	threshold         float32
	negativeThreshold float32
	minSpeech         int // samples
	minSilence        int // samples

	speaking     bool
	speechStart  int
	silenceStart int // -1 while no silence is pending
	ranges       []timeline.Range
}

func newSegmenter(opts VADOptions) *segmenter {
	negative := math.Max(opts.Threshold-vadNegativeMargin, vadMinNegativeThreshold)
	return &segmenter{
		threshold:         float32(opts.Threshold),
		negativeThreshold: float32(negative),
		minSpeech:         int(math.Round(opts.MinSpeech * vadSampleRate)),
		minSilence:        int(math.Round(opts.MinSilence * vadSampleRate)),
		silenceStart:      -1,
	}
}

// add feeds the probability for the frame that starts at sample position.
func (s *segmenter) add(position int, probability float32) {
	if probability >= s.threshold {
		s.silenceStart = -1
		if !s.speaking {
			s.speaking = true
			s.speechStart = position
		}
		return
	}
	if !s.speaking || probability >= s.negativeThreshold {
		return
	}
	if s.silenceStart < 0 {
		s.silenceStart = position
	}
	if position-s.silenceStart >= s.minSilence {
		s.emit(s.silenceStart)
	}
}

// finish closes speech still open at the end of the audio and returns every
// range found.
func (s *segmenter) finish(total int) []timeline.Range {
	if s.speaking {
		s.emit(total)
	}
	return s.ranges
}

func (s *segmenter) emit(end int) {
	if end-s.speechStart >= s.minSpeech {
		s.ranges = append(s.ranges, timeline.Range{
			Start: float64(s.speechStart) / vadSampleRate,
			End:   float64(end) / vadSampleRate,
		})
	}
	s.speaking = false
	s.silenceStart = -1
}
