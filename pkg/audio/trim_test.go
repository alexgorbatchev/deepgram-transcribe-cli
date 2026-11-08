package audio

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/timeline"
)

func TestParseTrimMode(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  TrimMode
	}{
		{"vad", TrimVAD}, {" Silence ", TrimSilence}, {"NONE", TrimNone},
	} {
		if got, err := ParseTrimMode(tc.value); err != nil || got != tc.want {
			t.Errorf("ParseTrimMode(%q) = %q, %v", tc.value, got, err)
		}
	}
	if _, err := ParseTrimMode("energy"); err == nil || !strings.Contains(err.Error(), "vad, silence or none") {
		t.Errorf("unknown mode accepted: %v", err)
	}
}

func TestParseSilences(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		want         []timeline.Range
		wantErr      string
	}{
		{"none", "", nil, ""},
		{"closed and open", "lavfi.silence_start=-0.01\nlavfi.silence_end=1\nlavfi.silence_start=4\n", []timeline.Range{{Start: 0, End: 1}, {Start: 4, End: math.Inf(1)}}, ""},
		{"end without start", "lavfi.silence_end=2\n", nil, ""},
		{"bad end", "lavfi.silence_start=1\nlavfi.silence_end=later\n", nil, "silence end"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSilences([]byte(tc.output))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got error %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestComplement(t *testing.T) {
	silences := []timeline.Range{{Start: 0, End: 1}, {Start: 3, End: 4}, {Start: 3.5, End: 5}}
	want := []timeline.Range{{Start: 1, End: 3}, {Start: 5, End: math.Inf(1)}}
	if got := complement(silences); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := complement([]timeline.Range{{Start: 0, End: math.Inf(1)}}); got != nil {
		t.Fatalf("all-silent audio kept %v", got)
	}
}

func TestToSampleSpans(t *testing.T) {
	ranges := []timeline.Range{
		{Start: 0.125, End: 1},           // padding runs before the start
		{Start: 1.375, End: 2},           // padding makes it touch the previous range
		{Start: 4.0004, End: 4.5},        // rounds outward to whole samples
		{Start: 9.875, End: math.Inf(1)}, // open-ended, clamped to the audio
		{Start: 12, End: 13},             // entirely past the end
	}
	got := toSampleSpans(ranges, 0.25, 1000, 10000)
	want := []sampleSpan{{start: 0, end: 2250}, {start: 3750, end: 4750}, {start: 9625, end: 10000}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if kept := spansToRanges(want[:1], 1000); !reflect.DeepEqual(kept, []timeline.Range{{Start: 0, End: 2.25}}) {
		t.Fatalf("spansToRanges = %v", kept)
	}
}

func TestClampSpansCutsAtTheDecodedEnd(t *testing.T) {
	spans := toSampleSpans([]timeline.Range{{Start: 1, End: 2}, {Start: 4, End: 6}, {Start: 20, End: math.Inf(1)}}, 0, 1000, unboundedSamples)
	if want := []sampleSpan{{1000, 2000}, {4000, 6000}, {20000, unboundedSamples}}; !reflect.DeepEqual(spans, want) {
		t.Fatalf("spans %v, want %v", spans, want)
	}
	for _, tc := range []struct {
		total int64
		want  []sampleSpan
	}{
		{30000, []sampleSpan{{1000, 2000}, {4000, 6000}, {20000, 30000}}},
		{5000, []sampleSpan{{1000, 2000}, {4000, 5000}}},
		{4000, []sampleSpan{{1000, 2000}}},
		{500, nil},
	} {
		if got := clampSpans(spans, tc.total); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("total %d: got %v, want %v", tc.total, got, tc.want)
		}
	}
}

func TestFingerprintNamesWhatChangesTheUpload(t *testing.T) {
	vad := PreprocessOptions{Trim: TrimVAD, Padding: 0.3, VAD: defaultVAD, SilenceThreshold: "-50dB"}
	silence := PreprocessOptions{Mono: true, Trim: TrimSilence, Padding: 0.5, VAD: defaultVAD}
	for _, tc := range []struct {
		name string
		opts PreprocessOptions
		want string
	}{
		{"vad", vad, "mono=false;trim=vad;padding=0.3;threshold=0.5;min_speech=0.25;min_silence=1"},
		{"silence with defaults", silence, "mono=true;trim=silence;padding=0.5;threshold=-30dB;duration=2.0"},
		{"silence", PreprocessOptions{Trim: TrimSilence, SilenceThreshold: "-40dB", SilenceDuration: "1.5"}, "mono=false;trim=silence;padding=0;threshold=-40dB;duration=1.5"},
		{"none ignores trim settings", PreprocessOptions{Mono: true, Trim: TrimNone, Padding: 2, VAD: defaultVAD}, "mono=true;trim=none"},
		{"zero value", PreprocessOptions{}, "mono=false;trim=none"},
	} {
		if got := tc.opts.Fingerprint(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	changed := vad
	changed.VAD.MinSilence = 2
	if changed.Fingerprint() == vad.Fingerprint() {
		t.Fatal("a different minimum silence left the fingerprint unchanged")
	}
}
