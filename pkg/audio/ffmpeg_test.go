package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/timeline"
)

// fakeFFmpeg answers the ffmpeg calls PreprocessAudio makes, so no test runs
// the real program. Its encoder copies the raw PCM piped into it, which lets a
// test read back exactly which samples were kept.
type fakeFFmpeg struct {
	t        *testing.T
	decoded  []byte // what a full decode produces
	speech   []byte // what the 16 kHz decode for voice detection produces
	silences string // what silencedetect prints
	failStep string // a call whose arguments contain this fails
	// workFiles lists the work directory's files when the encoder starts.
	workFiles []string

	mu    sync.Mutex
	calls [][]string
}

// fakeChunk is deliberately not a multiple of any frame size, so the decoded
// audio reaches the span filter split mid-frame.
const fakeChunk = 1001

func (f *fakeFFmpeg) record(name string, args []string) error {
	f.t.Helper()
	if name != "ffmpeg" {
		f.t.Errorf("unexpected program %s %v", name, args)
	}
	f.mu.Lock()
	f.calls = append(f.calls, args)
	f.mu.Unlock()
	if f.failStep != "" && strings.Contains(strings.Join(args, " "), f.failStep) {
		return errors.New("ffmpeg failed: ffmpeg diagnostic")
	}
	return nil
}

// run answers the calls whose output is captured or written to a file.
func (f *fakeFFmpeg) run(_ context.Context, name string, args ...string) ([]byte, error) {
	if err := f.record(name, args); err != nil {
		return []byte("ffmpeg diagnostic"), err
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "silencedetect=") {
		return []byte(f.silences), nil
	}
	if strings.Contains(joined, "pcm_s16le") {
		f.t.Errorf("decoded audio must be piped, not written: %v", args)
	}
	return nil, os.WriteFile(args[len(args)-1], []byte("downmixed"), 0o600)
}

// stream answers the decoders, which write PCM to stdout, and the encoder,
// which reads PCM from stdin.
func (f *fakeFFmpeg) stream(_ context.Context, stdin io.Reader, stdout io.Writer, name string, args ...string) error {
	failed := f.record(name, args)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "-i pipe:0") {
		output := args[len(args)-1]
		entries, err := os.ReadDir(filepath.Dir(output))
		if err != nil {
			f.t.Errorf("work directory missing: %v", err)
		}
		f.mu.Lock()
		for _, e := range entries {
			f.workFiles = append(f.workFiles, e.Name())
		}
		f.mu.Unlock()
		if failed != nil {
			return failed
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		return os.WriteFile(output, data, 0o600)
	}
	if failed != nil {
		return failed
	}
	if args[len(args)-1] != "pipe:1" || !strings.Contains(joined, "-f s16le") {
		f.t.Errorf("decode does not write raw PCM to stdout: %v", args)
	}
	pcm := f.decoded
	if strings.Contains(joined, fmt.Sprintf("-ac 1 -ar %d", vadSampleRate)) {
		pcm = f.speech
	}
	for len(pcm) > 0 {
		n := min(fakeChunk, len(pcm))
		if _, err := stdout.Write(pcm[:n]); err != nil {
			return err
		}
		pcm = pcm[n:]
	}
	return nil
}

// call returns the one call whose arguments contain marker.
func (f *fakeFFmpeg) call(marker string) string {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var found []string
	for _, args := range f.calls {
		if joined := strings.Join(args, " "); strings.Contains(joined, marker) {
			found = append(found, joined)
		}
	}
	if len(found) != 1 {
		f.t.Fatalf("calls containing %q: %v", marker, found)
	}
	return found[0]
}

// options returns opts running every program through the fake.
func (f *fakeFFmpeg) options(opts PreprocessOptions) PreprocessOptions {
	opts.Runner = f.run
	opts.Stream = f.stream
	return opts
}

// countingPCM returns frames of interleaved signed 16-bit samples whose value
// is the frame index, so a test can tell which frames survived a cut.
func countingPCM(frames, channels int) []byte {
	out := make([]byte, frames*channels*2)
	for i := range frames {
		for c := range channels {
			binary.LittleEndian.PutUint16(out[(i*channels+c)*2:], uint16(i))
		}
	}
	return out
}

// framesOf reads back the frame indexes countingPCM wrote, taking the first
// channel of each frame.
func framesOf(t *testing.T, pcm []byte, channels int) []int {
	t.Helper()
	frameBytes := channels * 2
	frames := make([]int, len(pcm)/frameBytes)
	for i := range frames {
		frames[i] = int(binary.LittleEndian.Uint16(pcm[i*frameBytes:]))
	}
	return frames
}

func span(from, to int) []int {
	var out []int
	for i := from; i < to; i++ {
		out = append(out, i)
	}
	return out
}

func assertRemoved(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s remains: %v", path, err)
	}
}

func TestPreprocessAudioNoOp(t *testing.T) {
	for _, trim := range []TrimMode{"", TrimNone} {
		fake := &fakeFFmpeg{t: t}
		got, cleanup, err := PreprocessAudio(t.Context(), "test.mp3", StreamInfo{}, fake.options(PreprocessOptions{Trim: trim}))
		cleanup()
		if err != nil || got.Path != "test.mp3" || got.Kept != nil || len(fake.calls) != 0 {
			t.Fatalf("trim %q: got %+v, error %v, calls %v", trim, got, err, fake.calls)
		}
	}
}

func TestPreprocessAudioDownmixes(t *testing.T) {
	t.Chdir(t.TempDir())
	fake := &fakeFFmpeg{t: t}
	got, cleanup, err := PreprocessAudio(t.Context(), "recording", StreamInfo{}, fake.options(PreprocessOptions{Mono: true}))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-nostdin", "-v", "error", "-y", "-i", "recording", "-map", "0:a:0", "-ac", "1", "-c:a", "flac", "-sample_fmt", "s16", got.Path}
	if len(fake.calls) != 1 || !reflect.DeepEqual(fake.calls[0], want) {
		t.Fatalf("calls %v, want %v", fake.calls, want)
	}
	if filepath.Base(got.Path) != preparedName || !strings.Contains(got.Path, "transcribe-proc-") || got.Kept != nil {
		t.Fatalf("unexpected result %+v", got)
	}
	if !strings.HasPrefix(got.Path, os.TempDir()) {
		t.Fatalf("prepared file %q is outside the OS temp directory", got.Path)
	}
	if _, err := os.Stat(".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preprocessing touched working-directory .tmp: %v", err)
	}
	cleanup()
	cleanup()
	assertRemoved(t, filepath.Dir(got.Path))
}

func TestPreprocessAudioCutsSilenceAtExactSamples(t *testing.T) {
	const rate = 1000
	for _, channels := range []int{1, 2} {
		fake := &fakeFFmpeg{
			t:       t,
			decoded: countingPCM(10*rate, channels),
			// The second silence is still open when the audio ends.
			silences: "frame:1 pts:2 pts_time:2\nlavfi.silence_start=2\nframe:2 pts:4 pts_time:4\nlavfi.silence_end=4\nlavfi.silence_duration=2\nlavfi.silence_start=6\n",
		}
		source := StreamInfo{Duration: 10, SampleRate: rate, Channels: channels}
		got, cleanup, err := PreprocessAudio(t.Context(), "talk.wav", source, fake.options(PreprocessOptions{Trim: TrimSilence, SilenceThreshold: "-40dB", SilenceDuration: "1.5", Padding: 0.5}))
		if err != nil {
			t.Fatal(err)
		}

		wantKept := []timeline.Range{{Start: 0, End: 2.5}, {Start: 3.5, End: 6.5}}
		if !reflect.DeepEqual(got.Kept, wantKept) {
			t.Fatalf("kept %v, want %v", got.Kept, wantKept)
		}
		if got.SourceDuration != 10 || got.Duration != 5.5 {
			t.Fatalf("durations %v and %v, want 10 and 5.5 from the decoded samples", got.SourceDuration, got.Duration)
		}
		uploaded, err := os.ReadFile(got.Path)
		if err != nil {
			t.Fatal(err)
		}
		if want := append(span(0, 2500), span(3500, 6500)...); !slices.Equal(framesOf(t, uploaded, channels), want) {
			t.Fatalf("channels %d: uploaded the wrong samples", channels)
		}
		fake.call("silencedetect=noise=-40dB:d=1.5")
		fake.call(fmt.Sprintf("-ac %d -ar 1000 -f s16le -c:a pcm_s16le pipe:1", channels))
		encode := fake.call("-i pipe:0")
		// Only the prepared audio is ever written: decoded PCM goes through a pipe.
		if !slices.Equal(fake.workFiles, []string(nil)) && !slices.Equal(fake.workFiles, []string{preparedName}) {
			t.Fatalf("work directory held %v while encoding", fake.workFiles)
		}
		if filepath.Base(got.Path) != preparedName || !strings.Contains(encode, "-c:a flac -sample_fmt s16") ||
			!strings.Contains(encode, fmt.Sprintf("-f s16le -ar 1000 -ac %d", channels)) {
			t.Fatalf("prepared audio is not 16-bit FLAC in the source format: %s %s", got.Path, encode)
		}
		// The encoder receives exactly the kept samples, so the upload lasts
		// exactly as long as the kept ranges add up to.
		keptSamples := 0
		for _, r := range got.Kept {
			keptSamples += int(math.Round((r.End - r.Start) * rate))
		}
		if len(uploaded)/(2*channels) != keptSamples {
			t.Fatalf("upload holds %d samples, kept ranges add up to %d", len(uploaded)/(2*channels), keptSamples)
		}
		cleanup()
		assertRemoved(t, filepath.Dir(got.Path))
	}
}

// TestPreprocessAudioKeepsTheTailBeyondAShortDurationEstimate covers containers
// without a duration index, such as ADTS AAC, where ffprobe estimates a
// duration shorter than the decoded audio. The audio after the estimate must
// still be uploaded.
func TestPreprocessAudioKeepsTheTailBeyondAShortDurationEstimate(t *testing.T) {
	const rate = 1000
	fake := &fakeFFmpeg{t: t, decoded: countingPCM(10*rate, 1), silences: "lavfi.silence_start=2\nlavfi.silence_end=4\n"}
	source := StreamInfo{Duration: 9.2, SampleRate: rate, Channels: 1}
	got, cleanup, err := PreprocessAudio(t.Context(), "talk.aac", source, fake.options(PreprocessOptions{Trim: TrimSilence}))
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if want := []timeline.Range{{Start: 0, End: 2}, {Start: 4, End: 10}}; !reflect.DeepEqual(got.Kept, want) {
		t.Fatalf("kept %v, want %v", got.Kept, want)
	}
	if got.SourceDuration != 10 || got.Duration != 8 {
		t.Fatalf("durations %v and %v, want the decoded 10 and 8, not the 9.2 s estimate", got.SourceDuration, got.Duration)
	}
	uploaded, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	if frames := framesOf(t, uploaded, 1); frames[len(frames)-1] != 10*rate-1 {
		t.Fatalf("upload ends at sample %d, want the last decoded sample %d", frames[len(frames)-1], 10*rate-1)
	}
}

func TestPreprocessAudioDownmixesWhileCutting(t *testing.T) {
	fake := &fakeFFmpeg{t: t, decoded: countingPCM(4000, 1), silences: "lavfi.silence_start=1\nlavfi.silence_end=3\n"}
	source := StreamInfo{Duration: 4, SampleRate: 1000, Channels: 2}
	got, cleanup, err := PreprocessAudio(t.Context(), "talk.m4a", source, fake.options(PreprocessOptions{Mono: true, Trim: TrimSilence}))
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if want := []timeline.Range{{Start: 0, End: 1}, {Start: 3, End: 4}}; !reflect.DeepEqual(got.Kept, want) {
		t.Fatalf("kept %v, want %v", got.Kept, want)
	}
	decode := fake.call("pipe:1")
	encode := fake.call("pipe:0")
	if !strings.Contains(decode, "-ac 1") || !strings.Contains(encode, "-ac 1") {
		t.Fatalf("downmix missing: %s | %s", decode, encode)
	}
}

func TestPreprocessAudioTrimsWithVoiceDetection(t *testing.T) {
	speech := readSpeechFixture(t)
	speechSeconds := float64(len(speech)/2) / vadSampleRate
	var detected bytes.Buffer
	detected.Write(noisePCM(2, 4))
	detected.Write(speech)
	detected.Write(noisePCM(2, 5))

	const rate = 1000
	total := 4 + speechSeconds
	fake := &fakeFFmpeg{t: t, speech: detected.Bytes(), decoded: countingPCM(int(total*rate), 1)}
	source := StreamInfo{Duration: total, SampleRate: rate, Channels: 1}
	got, cleanup, err := PreprocessAudio(t.Context(), "talk.opus", source, fake.options(PreprocessOptions{Trim: TrimVAD, VAD: defaultVAD, Padding: 0.1}))
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Kept) != 1 {
		t.Fatalf("want one kept range, got %v", got.Kept)
	}
	if kept := got.Kept[0]; kept.Start < 1.5 || kept.Start > 2.3 || kept.End < 4.4 || kept.End > total-1.5 {
		t.Fatalf("kept %+v, want about {2 %.2f} plus padding", kept, 2+speechSeconds)
	}
	fake.call("-ac 1 -ar 16000 -f s16le -c:a pcm_s16le pipe:1")
	fake.call("-ac 1 -ar 1000 -f s16le -c:a pcm_s16le pipe:1")
	if !slices.Equal(fake.workFiles, []string(nil)) && !slices.Equal(fake.workFiles, []string{preparedName}) {
		t.Fatalf("work directory held %v while encoding", fake.workFiles)
	}
}

func TestPreprocessAudioReportsNoSpeech(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	fake := &fakeFFmpeg{t: t, speech: noisePCM(2, 6), decoded: countingPCM(2000, 1)}
	source := StreamInfo{Duration: 2, SampleRate: 1000, Channels: 1}
	got, cleanup, err := PreprocessAudio(t.Context(), "noise.wav", source, fake.options(PreprocessOptions{Trim: TrimVAD, VAD: defaultVAD}))
	cleanup()
	if !errors.Is(err, ErrNoSpeech) || got.Path != "noise.wav" || got.SourceDuration != 2 || got.Kept != nil {
		t.Fatalf("got %+v, error %v", got, err)
	}
	// Nothing is encoded; the decode only measures the recording.
	fake.call("-ac 1 -ar 1000 -f s16le")
	if len(fake.calls) != 2 {
		t.Fatalf("calls %v, want the two decodes only", fake.calls)
	}
	if left, _ := os.ReadDir(tempDir); len(left) != 0 {
		t.Fatalf("temporary files remain: %v", left)
	}
}

func TestPreprocessAudioKeepsUncutAudio(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mono     bool
		channels int
		merges   bool
	}{
		{"stereo kept as it is", false, 2, false},
		{"mono asked of mono audio", true, 1, false},
		{"stereo merged", true, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			t.Setenv("TMPDIR", tempDir)
			decodedChannels := tc.channels
			if tc.mono {
				decodedChannels = 1
			}
			fake := &fakeFFmpeg{t: t, decoded: countingPCM(3000, decodedChannels)}
			source := StreamInfo{Duration: 3, SampleRate: 1000, Channels: tc.channels}
			got, cleanup, err := PreprocessAudio(t.Context(), "talk.wav", source, fake.options(PreprocessOptions{Mono: tc.mono, Trim: TrimSilence}))
			if err != nil || got.Kept != nil || got.SourceDuration != 3 {
				t.Fatalf("got %+v, error %v", got, err)
			}
			if !tc.merges {
				// Nothing is encoded only to be discarded: the decode only
				// measures the recording.
				if got.Path != "talk.wav" || got.Channels != 0 || len(fake.calls) != 2 || fake.workFiles != nil {
					t.Fatalf("got %+v after calls %v", got, fake.calls)
				}
			} else if got.Path == "talk.wav" || got.Duration != 3 || got.Channels != 1 {
				t.Fatalf("got %+v, want the whole recording merged to one channel", got)
			}
			cleanup()
			if left, _ := os.ReadDir(tempDir); len(left) != 0 {
				t.Fatalf("temporary files remain: %v", left)
			}
		})
	}
}

// TestPreprocessAudioCutsATailInOnePass covers one range from the start of
// an unmerged recording. When detection proved that the recording runs past
// the range, it is cut in one decode; otherwise it is measured first, so a
// recording kept whole is never encoded, and decoded again only if its tail
// is cut after all.
func TestPreprocessAudioCutsATailInOnePass(t *testing.T) {
	// The silence from 4 s is still open at the end, so it lasted at least
	// the 2 s minimum: the recording is proven to run to 6 s.
	for _, tc := range []struct {
		name       string
		padding    float64
		minSilence string
		kept       []timeline.Range
		calls      int
	}{
		{"tail proven", 0, "2.0", []timeline.Range{{Start: 0, End: 4}}, 3},
		{"whole recording", 2.5, "2.0", nil, 2},
		{"tail found by measuring", 1.99, "2.0", []timeline.Range{{Start: 0, End: 5.99}}, 4},
		// A duration in a form only ffmpeg reads proves nothing past 4 s.
		{"unreadable minimum silence", 0, "00:00:02", []timeline.Range{{Start: 0, End: 4}}, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeFFmpeg{t: t, decoded: countingPCM(6000, 2), silences: "lavfi.silence_start=4\n"}
			source := StreamInfo{SampleRate: 1000, Channels: 2}
			got, cleanup, err := PreprocessAudio(t.Context(), "talk.wav", source, fake.options(PreprocessOptions{Trim: TrimSilence, Padding: tc.padding, SilenceDuration: tc.minSilence}))
			defer cleanup()
			if err != nil || !reflect.DeepEqual(got.Kept, tc.kept) || got.SourceDuration != 6 || (tc.kept == nil) != (got.Path == "talk.wav") {
				t.Fatalf("got %+v, error %v", got, err)
			}
			if len(fake.calls) != tc.calls {
				t.Fatalf("calls %v, want %d", fake.calls, tc.calls)
			}
		})
	}
}

func TestPreprocessAudioCutsASpeechTailInOnePass(t *testing.T) {
	speech := readSpeechFixture(t)
	detected := append(append([]byte{}, speech...), noisePCM(3, 7)...)
	const rate = 1000
	total := float64(len(detected)/2) / vadSampleRate
	fake := &fakeFFmpeg{t: t, speech: detected, decoded: countingPCM(int(total*rate), 1)}
	source := StreamInfo{SampleRate: rate, Channels: 1}
	got, cleanup, err := PreprocessAudio(t.Context(), "talk.opus", source, fake.options(PreprocessOptions{Trim: TrimVAD, VAD: defaultVAD, Padding: 0.1}))
	defer cleanup()
	if err != nil || len(got.Kept) != 1 || got.Kept[0].Start != 0 || got.Kept[0].End > total-2 {
		t.Fatalf("got %+v, error %v", got, err)
	}
	if len(fake.calls) != 3 {
		t.Fatalf("calls %v, want voice detection, one decode and an encode", fake.calls)
	}
}

func TestPreprocessAudioSkipsMergingMonoAudio(t *testing.T) {
	fake := &fakeFFmpeg{t: t}
	got, cleanup, err := PreprocessAudio(t.Context(), "talk.wav", StreamInfo{SampleRate: 1000, Channels: 1}, fake.options(PreprocessOptions{Mono: true}))
	cleanup()
	if err != nil || got.Path != "talk.wav" || len(fake.calls) != 0 {
		t.Fatalf("got %+v, error %v, calls %v", got, err, fake.calls)
	}
}

func TestPreprocessAudioReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	killed := func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("signal: killed") }
	for _, trim := range []TrimMode{TrimNone, TrimSilence} {
		_, cleanup, err := PreprocessAudio(ctx, "talk.wav", StreamInfo{SampleRate: 1000, Channels: 2}, PreprocessOptions{Mono: true, Trim: trim, Runner: killed})
		cleanup()
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "signal: killed") {
			t.Fatalf("trim %s: got %v", trim, err)
		}
	}
	if _, err := Probe(ctx, "talk.wav", killed); !errors.Is(err, context.Canceled) {
		t.Fatalf("probe: got %v", err)
	}
}

func TestPreprocessAudioCleansUpAfterFailure(t *testing.T) {
	for _, tc := range []struct {
		name, failStep, want string
		trim                 TrimMode
	}{
		{"downmix", "-ac 1", "downmixing audio", TrimNone},
		{"silence detection", "silencedetect", "silence detection", TrimSilence},
		{"voice detection decode", "16000", "decoding audio for voice detection", TrimVAD},
		{"decode", "-ar 1000 -f s16le", "decoding audio failed", TrimSilence},
		{"encode", "-i pipe:0", "encoding trimmed audio", TrimSilence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			t.Setenv("TMPDIR", tempDir)
			fake := &fakeFFmpeg{t: t, failStep: tc.failStep, decoded: countingPCM(3000, 1), silences: "lavfi.silence_start=1\nlavfi.silence_end=2\n"}
			source := StreamInfo{Duration: 3, SampleRate: 1000, Channels: 2}
			got, cleanup, err := PreprocessAudio(t.Context(), "talk.wav", source, fake.options(PreprocessOptions{Mono: true, Trim: tc.trim}))
			cleanup()
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "ffmpeg diagnostic") || got.Path != "" {
				t.Fatalf("got %+v, error %v", got, err)
			}
			if left, _ := os.ReadDir(tempDir); len(left) != 0 {
				t.Fatalf("temporary files remain: %v", left)
			}
		})
	}
}

func TestPreprocessAudioRejectsUnreadableSilenceReport(t *testing.T) {
	fake := &fakeFFmpeg{t: t, silences: "lavfi.silence_start=soon\n"}
	_, cleanup, err := PreprocessAudio(t.Context(), "talk.wav", StreamInfo{Duration: 3, SampleRate: 1000, Channels: 1}, fake.options(PreprocessOptions{Trim: TrimSilence}))
	cleanup()
	if err == nil || !strings.Contains(err.Error(), "silence start") {
		t.Fatalf("got error %v", err)
	}
}

func TestPreprocessRejectsBlockedTempDirectory(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("TMP", os.Getenv("TMPDIR"))
	t.Setenv("TEMP", os.Getenv("TMPDIR"))
	fake := &fakeFFmpeg{t: t}
	got, cleanup, err := PreprocessAudio(t.Context(), "recording.wav", StreamInfo{}, fake.options(PreprocessOptions{Mono: true}))
	cleanup()
	if got.Path != "" || err == nil || !strings.Contains(err.Error(), "creating temp audio directory") || len(fake.calls) != 0 {
		t.Fatalf("got %+v, error %v", got, err)
	}
}

func TestDetectMIMEType(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"audio.mp3", "audio/mpeg"},
		{"call.m4a", "audio/m4a"},
		{"recording.mp4", "audio/mp4"},
		{"voice.wav", "audio/wav"},
		{"recording.ogg", "audio/ogg"},
		{"recording.flac", "audio/flac"},
		{"audio.aac", "audio/aac"},
		{"unknown.xyz", "application/octet-stream"},
	}

	for _, tt := range tests {
		got := DetectMIMEType(tt.filename)
		if got != tt.want {
			t.Errorf("DetectMIMEType(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}
}

// TestPreprocessAudioMeasuresAudioWithoutADuration covers a container whose
// duration ffprobe cannot read: trimming counts the decoded samples instead.
func TestPreprocessAudioMeasuresAudioWithoutADuration(t *testing.T) {
	fake := &fakeFFmpeg{t: t, decoded: countingPCM(6000, 1), silences: "lavfi.silence_start=2\nlavfi.silence_end=5\n"}
	source := StreamInfo{SampleRate: 1000, Channels: 1}
	got, cleanup, err := PreprocessAudio(t.Context(), "live.webm", source, fake.options(PreprocessOptions{Trim: TrimSilence}))
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceDuration != 6 || got.Duration != 3 || !reflect.DeepEqual(got.Kept, []timeline.Range{{Start: 0, End: 2}, {Start: 5, End: 6}}) {
		t.Fatalf("got %+v", got)
	}
}
