package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"math/rand/v2"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/timeline"
)

// speechFixture is 2.53 s of English speech, 16 kHz mono signed 16-bit PCM.
// See testdata/speech-16k-mono.LICENSE for its source.
const speechFixture = "testdata/speech-16k-mono.s16le"

var defaultVAD = VADOptions{
	Threshold:  DefaultVADThreshold,
	MinSpeech:  DefaultVADMinSpeech,
	MinSilence: DefaultVADMinSilence,
}

// noisePCM returns seconds of loud white noise at 16 kHz. Its level, around
// -20 dBFS, is far above the silence threshold, so a volume-based trim would
// keep all of it.
func noisePCM(seconds float64, seed uint64) []byte {
	rng := rand.New(rand.NewPCG(seed, seed))
	samples := int(seconds * vadSampleRate)
	out := make([]byte, samples*2)
	for i := range samples {
		value := int16(rng.NormFloat64() * 0.1 * math.MaxInt16)
		binary.LittleEndian.PutUint16(out[i*2:], uint16(value))
	}
	return out
}

func readSpeechFixture(t *testing.T) []byte {
	t.Helper()
	speech, err := os.ReadFile(speechFixture)
	if err != nil {
		t.Fatalf("reading the speech fixture: %v", err)
	}
	return speech
}

func TestDetectSpeechKeepsSpeechAndDropsNoise(t *testing.T) {
	speech := readSpeechFixture(t)
	speechSeconds := float64(len(speech)/2) / vadSampleRate

	var pcm bytes.Buffer
	pcm.Write(noisePCM(2, 1))
	pcm.Write(speech)
	pcm.Write(noisePCM(2, 2))

	ranges, err := detectSpeech(&pcm, defaultVAD)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 1 {
		t.Fatalf("want one speech range, got %v", ranges)
	}

	// Silero works in 32 ms frames and the fixture starts and ends with a
	// breath, so allow a few frames either side of where the speech sits.
	const tolerance = 0.3
	speechEnd := 2 + speechSeconds
	got := ranges[0]
	if math.Abs(got.Start-2) > tolerance || math.Abs(got.End-speechEnd) > tolerance {
		t.Fatalf("speech range %+v, want about {2 %.2f}", got, speechEnd)
	}
}

func TestDetectSpeechFindsNothingInNoise(t *testing.T) {
	ranges, err := detectSpeech(bytes.NewReader(noisePCM(3, 3)), defaultVAD)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 0 {
		t.Fatalf("noise detected as speech: %v", ranges)
	}
}

func TestDetectSpeechReportsReadErrors(t *testing.T) {
	want := errors.New("disk gone")
	_, err := detectSpeech(iotest.ErrReader(want), defaultVAD)
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}

func TestDetectSpeechReadsAPartialLastFrame(t *testing.T) {
	speech := readSpeechFixture(t)
	// An odd byte count leaves a partial frame and a dangling byte.
	ranges, err := detectSpeech(bytes.NewReader(speech[:len(speech)-101]), defaultVAD)
	if err != nil || len(ranges) != 1 {
		t.Fatalf("ranges %v, error %v", ranges, err)
	}
}

func TestSegmenter(t *testing.T) {
	const frame = 512
	frameSeconds := float64(frame) / vadSampleRate
	at := func(frames int) float64 { return float64(frames) * frameSeconds }

	tests := []struct {
		name  string
		opts  VADOptions
		probs []float32
		want  []timeline.Range
	}{
		{
			name:  "speech closed by enough silence",
			opts:  VADOptions{Threshold: 0.5, MinSilence: at(2)},
			probs: []float32{0.1, 0.9, 0.9, 0.1, 0.1, 0.1, 0.1},
			want:  []timeline.Range{{Start: at(1), End: at(3)}},
		},
		{
			name:  "a short pause stays inside the range",
			opts:  VADOptions{Threshold: 0.5, MinSilence: at(3)},
			probs: []float32{0.9, 0.1, 0.1, 0.9, 0.1, 0.1, 0.1, 0.1},
			want:  []timeline.Range{{Start: 0, End: at(4)}},
		},
		{
			name:  "probabilities between the thresholds keep speech going",
			opts:  VADOptions{Threshold: 0.5, MinSilence: at(1)},
			probs: []float32{0.9, 0.4, 0.4, 0.4, 0.1, 0.1},
			want:  []timeline.Range{{Start: 0, End: at(4)}},
		},
		{
			name:  "speech shorter than the minimum is dropped",
			opts:  VADOptions{Threshold: 0.5, MinSpeech: at(2), MinSilence: at(1)},
			probs: []float32{0.9, 0.1, 0.1, 0.9, 0.9, 0.9, 0.1, 0.1},
			want:  []timeline.Range{{Start: at(3), End: at(6)}},
		},
		{
			name:  "speech still open at the end runs to the end",
			opts:  VADOptions{Threshold: 0.5, MinSilence: at(5)},
			probs: []float32{0.1, 0.9, 0.9, 0.1},
			want:  []timeline.Range{{Start: at(1), End: at(4)}},
		},
		{
			name:  "no speech",
			opts:  VADOptions{Threshold: 0.5},
			probs: []float32{0.1, 0.2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seg := newSegmenter(tt.opts)
			for i, p := range tt.probs {
				seg.add(i*frame, p)
			}
			got := seg.finish(len(tt.probs) * frame)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVADOptionsValidate(t *testing.T) {
	tests := []struct {
		name string
		opts VADOptions
		want string
	}{
		{"defaults", defaultVAD, ""},
		{"zero threshold", VADOptions{Threshold: 0}, "speech threshold"},
		{"threshold of one", VADOptions{Threshold: 1}, "speech threshold"},
		{"NaN threshold", VADOptions{Threshold: math.NaN()}, "speech threshold"},
		{"negative minimum speech", VADOptions{Threshold: 0.5, MinSpeech: -1}, "minimum speech"},
		{"negative minimum silence", VADOptions{Threshold: 0.5, MinSilence: -1}, "minimum silence"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want an error about %q", err, tt.want)
			}
		})
	}
}
