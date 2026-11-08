package audio

import (
	"os"
	"path/filepath"
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
		{"closed and open", "lavfi.silence_start=-0.01\nlavfi.silence_end=1\nlavfi.silence_start=4\n", []timeline.Range{{Start: 0, End: 1}, {Start: 4, End: 9}}, ""},
		{"end without start", "lavfi.silence_end=2\n", nil, ""},
		{"bad end", "lavfi.silence_start=1\nlavfi.silence_end=later\n", nil, "silence end"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSilences([]byte(tc.output), 9)
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
	want := []timeline.Range{{Start: 1, End: 3}, {Start: 5, End: 8}}
	if got := complement(silences, 8); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := complement([]timeline.Range{{Start: 0, End: 8}}, 8); got != nil {
		t.Fatalf("all-silent audio kept %v", got)
	}
}

func TestToSampleSpans(t *testing.T) {
	ranges := []timeline.Range{
		{Start: 0.125, End: 1},    // padding runs before the start
		{Start: 1.375, End: 2},    // padding makes it touch the previous range
		{Start: 4.0004, End: 4.5}, // rounds outward to whole samples
		{Start: 9.875, End: 10.5}, // padding and range run past the end
		{Start: 12, End: 13},      // entirely past the end
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

func TestCutPCMReportsMissingFiles(t *testing.T) {
	dir := t.TempDir()
	if err := cutPCM(filepath.Join(dir, "missing.pcm"), filepath.Join(dir, "out.pcm"), nil, 2); err == nil || !strings.Contains(err.Error(), "opening decoded audio") {
		t.Fatalf("got %v", err)
	}
	src := filepath.Join(dir, "src.pcm")
	if err := os.WriteFile(src, countingPCM(10, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cutPCM(src, filepath.Join(dir, "missing", "out.pcm"), nil, 2); err == nil || !strings.Contains(err.Error(), "creating trimmed audio") {
		t.Fatalf("got %v", err)
	}
}
