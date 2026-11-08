package audio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alexgorbatchev/godeps"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/timeline"
)

const (
	DefaultSilenceThreshold = "-30dB"
	DefaultSilenceDuration  = "2.0"

	// pcmBytesPerSample is the size of one signed 16-bit sample, the format
	// audio is decoded to before it is measured and cut.
	pcmBytesPerSample = 2
	// preparedName is the prepared upload. FLAC is lossless and, unlike AAC,
	// MP3 or Opus, adds no encoder priming delay, so the prepared audio holds
	// exactly the kept samples and the kept ranges stay exact on Deepgram's
	// side too.
	preparedName = "audio.flac"
)

// ErrNoSpeech reports that trimming found nothing worth keeping.
var ErrNoSpeech = errors.New("no speech was detected")

// PreprocessOptions configures how audio is prepared before upload.
type PreprocessOptions struct {
	Mono             bool                 // merge all channels into one
	Trim             TrimMode             // how to find the audio worth keeping; empty means TrimNone
	SilenceThreshold string               // TrimSilence: volume below which audio is silent, e.g. "-30dB"
	SilenceDuration  string               // TrimSilence: shortest silence removed, in seconds, e.g. "2.0"
	VAD              VADOptions           // TrimVAD: speech detection settings
	Padding          float64              // seconds kept on each side of the audio worth keeping
	Runner           godeps.CommandRunner // nil uses the real process runner
}

// Prepared is the result of preprocessing.
type Prepared struct {
	// Path is the audio to upload. It is the input path when nothing changed.
	Path string
	// Kept lists the ranges of the input, in seconds, that Path contains, end
	// to end. It is empty when nothing was cut.
	Kept []timeline.Range
}

// PreprocessAudio prepares audio for upload: it merges channels when Mono is
// set, and cuts what Trim finds not worth uploading.
//
// Prepared audio is 16-bit FLAC at the input's sample rate, with the input's
// channels or one channel after Mono. Cutting decodes the input to raw PCM,
// copies the kept sample ranges and encodes them losslessly, so the kept
// ranges are exact by construction. source
// describes the input's audio stream, as Probe reports it.
//
// The returned function removes every temporary file and is safe to call more
// than once.
func PreprocessAudio(ctx context.Context, inputPath string, source StreamInfo, opts PreprocessOptions) (Prepared, func(), error) {
	noCleanup := func() {}
	unchanged := Prepared{Path: inputPath}

	if !opts.Mono && !opts.Trim.Cuts() {
		return unchanged, noCleanup, nil
	}

	workDir, err := os.MkdirTemp("", "transcribe-proc-*")
	if err != nil {
		return Prepared{}, noCleanup, fmt.Errorf("creating temp audio directory: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(workDir) // Best-effort cleanup, including repeated calls.
	}

	p := preparation{
		ctx:     ctx,
		input:   inputPath,
		source:  source,
		opts:    opts,
		workDir: workDir,
		output:  filepath.Join(workDir, preparedName),
		runner:  opts.Runner,
	}
	if p.runner == nil {
		p.runner = godeps.DefaultRunner
	}

	prepared, err := p.run()
	if err != nil || prepared.Path == inputPath {
		cleanup()
		return prepared, noCleanup, err
	}
	return prepared, cleanup, nil
}

// preparation carries one PreprocessAudio call through its steps.
type preparation struct {
	ctx     context.Context
	input   string
	source  StreamInfo
	opts    PreprocessOptions
	workDir string
	output  string
	runner  godeps.CommandRunner
}

func (p *preparation) run() (Prepared, error) {
	unchanged := Prepared{Path: p.input}
	if !p.opts.Trim.Cuts() {
		return p.downmix()
	}

	ranges, err := p.detect()
	if err != nil {
		return Prepared{}, err
	}

	channels := p.source.Channels
	if p.opts.Mono {
		channels = 1
	}
	frameBytes := int64(channels * pcmBytesPerSample)

	decoded := filepath.Join(p.workDir, "decoded.pcm")
	if err := p.ffmpeg("decoding audio", p.decodeArgs(decoded, channels, p.source.SampleRate)...); err != nil {
		return Prepared{}, err
	}
	info, err := os.Stat(decoded)
	if err != nil {
		return Prepared{}, fmt.Errorf("measuring decoded audio: %w", err)
	}
	totalSamples := info.Size() / frameBytes

	spans := toSampleSpans(ranges, p.opts.Padding, p.source.SampleRate, totalSamples)
	if len(spans) == 0 {
		return Prepared{}, ErrNoSpeech
	}
	if len(spans) == 1 && spans[0].start == 0 && spans[0].end == totalSamples {
		if p.opts.Mono {
			return p.downmix()
		}
		return unchanged, nil
	}

	cut := filepath.Join(p.workDir, "cut.pcm")
	if err := cutPCM(decoded, cut, spans, frameBytes); err != nil {
		return Prepared{}, err
	}
	_ = os.Remove(decoded) // Frees disk early; cleanup removes it otherwise.

	encodeArgs := []string{
		"-nostdin", "-v", "error", "-y",
		"-f", "s16le", "-ar", strconv.Itoa(p.source.SampleRate), "-ac", strconv.Itoa(channels),
		"-i", cut, "-c:a", "flac", "-sample_fmt", "s16", p.output,
	}
	if err := p.ffmpeg("encoding trimmed audio", encodeArgs...); err != nil {
		return Prepared{}, err
	}

	return Prepared{Path: p.output, Kept: spansToRanges(spans, p.source.SampleRate)}, nil
}

// downmix merges every channel into one without cutting anything.
func (p *preparation) downmix() (Prepared, error) {
	args := []string{"-nostdin", "-v", "error", "-y", "-i", p.input, "-map", "0:a:0", "-ac", "1", "-c:a", "flac", "-sample_fmt", "s16", p.output}
	if err := p.ffmpeg("downmixing audio", args...); err != nil {
		return Prepared{}, err
	}
	return Prepared{Path: p.output}, nil
}

// detect returns the ranges of the input, in seconds, that the trim mode
// considers worth keeping, before padding.
func (p *preparation) detect() ([]timeline.Range, error) {
	if p.opts.Trim == TrimVAD {
		return p.detectSpeech()
	}
	return p.detectSound()
}

// detectSound keeps everything between silences that ffmpeg's silencedetect
// filter reports.
func (p *preparation) detectSound() ([]timeline.Range, error) {
	threshold := p.opts.SilenceThreshold
	if threshold == "" {
		threshold = DefaultSilenceThreshold
	}
	duration := p.opts.SilenceDuration
	if duration == "" {
		duration = DefaultSilenceDuration
	}

	filter := fmt.Sprintf("silencedetect=noise=%s:d=%s,ametadata=mode=print:file=-", threshold, duration)
	args := []string{"-nostdin", "-v", "error", "-i", p.input, "-map", "0:a:0", "-af", filter, "-f", "null", "-"}
	output, err := p.runner(p.ctx, "ffmpeg", args...)
	if err != nil {
		return nil, fmt.Errorf("ffmpeg silence detection failed: %w (output: %s)", err, strings.TrimSpace(string(output)))
	}

	silences, err := parseSilences(output)
	if err != nil {
		return nil, err
	}
	return complement(silences), nil
}

// detectSpeech decodes the input to the format the speech model reads and
// keeps what it hears as speech.
func (p *preparation) detectSpeech() ([]timeline.Range, error) {
	pcmPath := filepath.Join(p.workDir, "speech.pcm")
	if err := p.ffmpeg("decoding audio for voice detection", p.decodeArgs(pcmPath, 1, vadSampleRate)...); err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(pcmPath) }() // Frees disk early; cleanup removes it otherwise.

	pcm, err := os.Open(pcmPath)
	if err != nil {
		return nil, fmt.Errorf("opening audio for voice detection: %w", err)
	}
	defer func() { _ = pcm.Close() }() // Read-only; a close error loses nothing.

	return detectSpeech(pcm, p.opts.VAD)
}

// decodeArgs decodes the input's first audio stream to raw signed 16-bit PCM.
func (p *preparation) decodeArgs(outputPath string, channels, sampleRate int) []string {
	return []string{
		"-nostdin", "-v", "error", "-y", "-i", p.input, "-map", "0:a:0",
		"-ac", strconv.Itoa(channels), "-ar", strconv.Itoa(sampleRate),
		"-f", "s16le", "-c:a", "pcm_s16le", outputPath,
	}
}

func (p *preparation) ffmpeg(step string, args ...string) error {
	output, err := p.runner(p.ctx, "ffmpeg", args...)
	if err != nil {
		return fmt.Errorf("ffmpeg %s failed: %w (output: %s)", step, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// DetectMIMEType returns the MIME type corresponding to an audio file's extension.
func DetectMIMEType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/m4a"
	case ".mp4":
		return "audio/mp4"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	case ".aac":
		return "audio/aac"
	default:
		return "application/octet-stream"
	}
}
