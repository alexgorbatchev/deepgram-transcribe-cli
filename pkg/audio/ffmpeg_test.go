package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/timeline"
)

// fakeFFmpeg answers the ffmpeg calls PreprocessAudio makes, so no test runs
// the real program. Its encoder copies the raw PCM it is given, which lets a
// test read back exactly which samples were kept.
type fakeFFmpeg struct {
	t        *testing.T
	decoded  []byte // what a full decode produces
	speech   []byte // what the 16 kHz decode for voice detection produces
	silences string // what silencedetect prints
	failStep string // a call whose arguments contain this fails
	calls    [][]string
}

func (f *fakeFFmpeg) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.t.Helper()
	if name != "ffmpeg" {
		f.t.Fatalf("unexpected program %s %v", name, args)
	}
	f.calls = append(f.calls, args)
	joined := strings.Join(args, " ")
	if f.failStep != "" && strings.Contains(joined, f.failStep) {
		return []byte("ffmpeg diagnostic"), errors.New("ffmpeg failed")
	}

	output := args[len(args)-1]
	switch {
	case strings.Contains(joined, "silencedetect="):
		return []byte(f.silences), nil
	case strings.Contains(joined, "pcm_s16le") && filepath.Base(output) == "speech.pcm":
		return nil, os.WriteFile(output, f.speech, 0o600)
	case strings.Contains(joined, "pcm_s16le"):
		return nil, os.WriteFile(output, f.decoded, 0o600)
	case slices.Contains(args, "s16le"):
		input := args[slices.Index(args, "-i")+1]
		data, err := os.ReadFile(input)
		if err != nil {
			f.t.Fatalf("encoder input missing: %v", err)
		}
		return nil, os.WriteFile(output, data, 0o600)
	default:
		return nil, os.WriteFile(output, []byte("downmixed"), 0o600)
	}
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
		got, cleanup, err := PreprocessAudio(t.Context(), "test.mp3", StreamInfo{}, PreprocessOptions{Trim: trim, Runner: fake.run})
		cleanup()
		if err != nil || got.Path != "test.mp3" || got.Kept != nil || len(fake.calls) != 0 {
			t.Fatalf("trim %q: got %+v, error %v, calls %v", trim, got, err, fake.calls)
		}
	}
}

func TestPreprocessAudioDownmixes(t *testing.T) {
	t.Chdir(t.TempDir())
	fake := &fakeFFmpeg{t: t}
	got, cleanup, err := PreprocessAudio(t.Context(), "recording", StreamInfo{}, PreprocessOptions{Mono: true, Runner: fake.run})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-nostdin", "-v", "error", "-y", "-i", "recording", "-map", "0:a:0", "-ac", "1", got.Path}
	if len(fake.calls) != 1 || !reflect.DeepEqual(fake.calls[0], want) {
		t.Fatalf("calls %v, want %v", fake.calls, want)
	}
	if !strings.HasSuffix(got.Path, defaultExtension) || !strings.Contains(got.Path, "transcribe-proc-") || got.Kept != nil {
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
		got, cleanup, err := PreprocessAudio(t.Context(), "talk.wav", source, PreprocessOptions{
			Trim: TrimSilence, SilenceThreshold: "-40dB", SilenceDuration: "1.5", Padding: 0.5, Runner: fake.run,
		})
		if err != nil {
			t.Fatal(err)
		}

		wantKept := []timeline.Range{{Start: 0, End: 2.5}, {Start: 3.5, End: 6.5}}
		if !reflect.DeepEqual(got.Kept, wantKept) {
			t.Fatalf("kept %v, want %v", got.Kept, wantKept)
		}
		uploaded, err := os.ReadFile(got.Path)
		if err != nil {
			t.Fatal(err)
		}
		if want := append(span(0, 2500), span(3500, 6500)...); !slices.Equal(framesOf(t, uploaded, channels), want) {
			t.Fatalf("channels %d: uploaded the wrong samples", channels)
		}
		if !strings.Contains(strings.Join(fake.calls[0], " "), "silencedetect=noise=-40dB:d=1.5") {
			t.Fatalf("silence settings not passed: %v", fake.calls[0])
		}
		if decode := strings.Join(fake.calls[1], " "); !strings.Contains(decode, fmt.Sprintf("-ac %d -ar 1000", channels)) {
			t.Fatalf("decode did not keep the source format: %v", decode)
		}
		if !strings.HasSuffix(got.Path, ".wav") {
			t.Fatalf("prepared audio changed container: %s", got.Path)
		}
		cleanup()
		assertRemoved(t, filepath.Dir(got.Path))
	}
}

func TestPreprocessAudioDownmixesWhileCutting(t *testing.T) {
	fake := &fakeFFmpeg{t: t, decoded: countingPCM(4000, 1), silences: "lavfi.silence_start=1\nlavfi.silence_end=3\n"}
	source := StreamInfo{Duration: 4, SampleRate: 1000, Channels: 2}
	got, cleanup, err := PreprocessAudio(t.Context(), "talk.m4a", source, PreprocessOptions{Mono: true, Trim: TrimSilence, Runner: fake.run})
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if want := []timeline.Range{{Start: 0, End: 1}, {Start: 3, End: 4}}; !reflect.DeepEqual(got.Kept, want) {
		t.Fatalf("kept %v, want %v", got.Kept, want)
	}
	decode := strings.Join(fake.calls[1], " ")
	encode := strings.Join(fake.calls[2], " ")
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
	got, cleanup, err := PreprocessAudio(t.Context(), "talk.opus", source, PreprocessOptions{
		Trim: TrimVAD, VAD: defaultVAD, Padding: 0.1, Runner: fake.run,
	})
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
	vadDecode := strings.Join(fake.calls[0], " ")
	if !strings.Contains(vadDecode, "-ac 1 -ar 16000") {
		t.Fatalf("voice detection decode is not 16 kHz mono: %s", vadDecode)
	}
	assertRemoved(t, filepath.Join(filepath.Dir(got.Path), "speech.pcm"))
	assertRemoved(t, filepath.Join(filepath.Dir(got.Path), "decoded.pcm"))
}

func TestPreprocessAudioReportsNoSpeech(t *testing.T) {
	fake := &fakeFFmpeg{t: t, speech: noisePCM(2, 6), decoded: countingPCM(2000, 1)}
	source := StreamInfo{Duration: 2, SampleRate: 1000, Channels: 1}
	got, cleanup, err := PreprocessAudio(t.Context(), "noise.wav", source, PreprocessOptions{Trim: TrimVAD, VAD: defaultVAD, Runner: fake.run})
	cleanup()
	if !errors.Is(err, ErrNoSpeech) || got.Path != "" {
		t.Fatalf("got %+v, error %v", got, err)
	}
	assertRemoved(t, filepath.Dir(fake.calls[0][len(fake.calls[0])-1]))
}

func TestPreprocessAudioKeepsUncutAudio(t *testing.T) {
	for _, mono := range []bool{false, true} {
		fake := &fakeFFmpeg{t: t, decoded: countingPCM(3000, 1)}
		source := StreamInfo{Duration: 3, SampleRate: 1000, Channels: 1}
		got, cleanup, err := PreprocessAudio(t.Context(), "talk.wav", source, PreprocessOptions{Mono: mono, Trim: TrimSilence, Runner: fake.run})
		if err != nil || got.Kept != nil {
			t.Fatalf("got %+v, error %v", got, err)
		}
		if (got.Path == "talk.wav") == mono {
			t.Fatalf("mono %t: unexpected upload %s", mono, got.Path)
		}
		workDir := filepath.Dir(fake.calls[1][len(fake.calls[1])-1])
		cleanup()
		assertRemoved(t, workDir)
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
		{"decode", "decoded.pcm", "decoding audio", TrimSilence},
		{"encode", "cut.pcm", "encoding trimmed audio", TrimSilence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			t.Setenv("TMPDIR", tempDir)
			fake := &fakeFFmpeg{t: t, failStep: tc.failStep, decoded: countingPCM(3000, 1), silences: "lavfi.silence_start=1\nlavfi.silence_end=2\n"}
			source := StreamInfo{Duration: 3, SampleRate: 1000, Channels: 1}
			got, cleanup, err := PreprocessAudio(t.Context(), "talk.wav", source, PreprocessOptions{Mono: true, Trim: tc.trim, Runner: fake.run})
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
	_, cleanup, err := PreprocessAudio(t.Context(), "talk.wav", StreamInfo{Duration: 3, SampleRate: 1000, Channels: 1}, PreprocessOptions{Trim: TrimSilence, Runner: fake.run})
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
	got, cleanup, err := PreprocessAudio(t.Context(), "recording.wav", StreamInfo{}, PreprocessOptions{Mono: true, Runner: fake.run})
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
