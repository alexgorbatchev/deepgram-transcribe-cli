package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	// unboundedSamples stands in for the end of audio whose length is not
	// known yet. It is decades of audio at any sample rate, and small enough
	// that its offset in bytes cannot overflow for any channel count ffmpeg
	// supports.
	unboundedSamples int64 = 1 << 50
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
	Stream           StreamRunner         // runs the decoders and encoder whose audio is piped; nil uses DefaultStreamRunner
}

// Fingerprint describes, in a stable form, every option that changes the audio
// PreprocessAudio prepares, so that a cache key can tell uploads apart.
// Options that the trim mode ignores are left out.
func (o PreprocessOptions) Fingerprint() string {
	trim := o.Trim
	if !trim.Cuts() {
		trim = TrimNone
	}
	fingerprint := fmt.Sprintf("mono=%t;trim=%s", o.Mono, trim)
	switch trim {
	case TrimVAD:
		fingerprint += fmt.Sprintf(";padding=%s;threshold=%s;min_speech=%s;min_silence=%s",
			formatOption(o.Padding), formatOption(o.VAD.Threshold), formatOption(o.VAD.MinSpeech), formatOption(o.VAD.MinSilence))
	case TrimSilence:
		threshold := o.SilenceThreshold
		if threshold == "" {
			threshold = DefaultSilenceThreshold
		}
		duration := o.SilenceDuration
		if duration == "" {
			duration = DefaultSilenceDuration
		}
		fingerprint += fmt.Sprintf(";padding=%s;threshold=%s;duration=%s", formatOption(o.Padding), threshold, duration)
	}
	return fingerprint
}

func formatOption(value float64) string { return strconv.FormatFloat(value, 'g', -1, 64) }

// Prepared is the result of preprocessing.
type Prepared struct {
	// Path is the audio to upload. It is the input path when nothing changed.
	Path string
	// Kept lists the ranges of the input, in seconds, that Path contains, end
	// to end. It is empty when nothing was cut.
	Kept []timeline.Range
	// SourceDuration is the input's length in seconds, counted from its
	// decoded samples, and Duration is Path's length counted the same way.
	// Both are exact, unlike the estimate ffprobe reports for containers
	// without a duration index. Both are zero when the input was not decoded,
	// which is the case for a downmix without trimming.
	SourceDuration float64
	Duration       float64
	// Channels is Path's channel count when it is prepared audio, and zero
	// when Path is the input.
	Channels int
}

// PreprocessAudio prepares audio for upload: it merges channels when Mono is
// set, and cuts what Trim finds not worth uploading.
//
// Prepared audio is 16-bit FLAC at the input's sample rate, with the input's
// channels or one channel after Mono. Cutting pipes the input's decoded PCM
// through this process, which passes only the kept sample ranges on to a FLAC
// encoder, so the kept ranges are exact by construction and the only file
// written is the prepared audio. source describes the input's audio stream, as
// Probe reports it; its duration is not needed.
//
// When trimming keeps nothing, the error is ErrNoSpeech and the returned
// Prepared still reports SourceDuration.
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
		stream:  opts.Stream,
	}
	if p.runner == nil {
		p.runner = godeps.DefaultRunner
	}
	if p.stream == nil {
		p.stream = DefaultStreamRunner
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
	stream  StreamRunner
}

func (p *preparation) run() (Prepared, error) {
	unchanged := Prepared{Path: p.input}
	// A recording that is already mono has nothing to merge.
	downmixes := p.opts.Mono && p.source.Channels != 1
	if !p.opts.Trim.Cuts() {
		if !downmixes {
			return unchanged, nil
		}
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
	rate := p.source.SampleRate

	spans := toSampleSpans(ranges, p.opts.Padding, rate, unboundedSamples)
	if !downmixes && len(spans) == 1 && spans[0].start == 0 {
		// One span from the start keeps the whole recording unless the
		// recording runs past it, and a whole recording that is not merged
		// is uploaded as it is. Measuring first avoids encoding audio only
		// to discard it, at the cost of decoding twice when the tail is cut.
		totalSamples, err := p.cut(nil, channels)
		if err != nil {
			return Prepared{}, err
		}
		if spans[0].end >= totalSamples {
			unchanged.SourceDuration = samplesToSeconds(totalSamples, rate)
			return unchanged, nil
		}
	}

	totalSamples, err := p.cut(spans, channels)
	if err != nil {
		return Prepared{}, err
	}
	measured := samplesToSeconds(totalSamples, rate)
	unchanged.SourceDuration = measured

	spans = clampSpans(spans, totalSamples)
	if len(spans) == 0 {
		return unchanged, ErrNoSpeech
	}
	if len(spans) == 1 && spans[0].start == 0 && spans[0].end == totalSamples {
		if !downmixes {
			return unchanged, nil
		}
		// Everything was kept, so the prepared audio is a plain downmix.
		return Prepared{Path: p.output, SourceDuration: measured, Duration: measured, Channels: 1}, nil
	}

	var keptSamples int64
	for _, s := range spans {
		keptSamples += s.end - s.start
	}
	return Prepared{
		Path:           p.output,
		Kept:           spansToRanges(spans, rate),
		SourceDuration: measured,
		Duration:       samplesToSeconds(keptSamples, rate),
		Channels:       channels,
	}, nil
}

// cut decodes the input and encodes the samples inside spans, end to end, to
// the prepared output, and returns how many samples per channel the input
// decoded to. Decoded audio is piped from the decoder through a spanFilter
// into the encoder and never written to disk. Without spans nothing is
// encoded and the input is only measured.
func (p *preparation) cut(spans []sampleSpan, channels int) (int64, error) {
	ctx, cancel := context.WithCancel(p.ctx)
	defer cancel()

	filter := &spanFilter{spans: spans, frameBytes: int64(channels * pcmBytesPerSample)}
	encoded := make(chan error, 1)
	var pipe *io.PipeWriter
	if len(spans) > 0 {
		var reader *io.PipeReader
		reader, pipe = io.Pipe()
		filter.dst = pipe
		encodeArgs := []string{
			"-nostdin", "-v", "error", "-y",
			"-f", "s16le", "-ar", strconv.Itoa(p.source.SampleRate), "-ac", strconv.Itoa(channels),
			"-i", "pipe:0", "-c:a", "flac", "-sample_fmt", "s16", p.output,
		}
		go func() {
			err := p.stream(ctx, reader, nil, "ffmpeg", encodeArgs...)
			// Unblocks the filter if the encoder stops reading early.
			_ = reader.CloseWithError(errEncoderStopped)
			encoded <- err
		}()
	} else {
		encoded <- nil
	}

	decodeErr := p.stream(ctx, nil, filter, "ffmpeg", p.decodeArgs(channels, p.source.SampleRate)...)
	if pipe != nil {
		// A failed decode must not leave a truncated FLAC looking complete.
		_ = pipe.CloseWithError(decodeErr)
	}
	if decodeErr != nil {
		cancel()
	}
	encodeErr := <-encoded

	// An encoder that stops reading also breaks the decoder's output, and a
	// failed decode stops the encoder, so the side that failed first is the
	// cause.
	if filter.writeErr != nil || (decodeErr == nil && encodeErr != nil) {
		return 0, fmt.Errorf("ffmpeg encoding trimmed audio failed: %w", errors.Join(encodeErr, filter.writeErr))
	}
	if decodeErr != nil {
		return 0, fmt.Errorf("ffmpeg decoding audio failed: %w", decodeErr)
	}
	return filter.frames(), nil
}

// errEncoderStopped is what the filter sees when the encoder has exited.
var errEncoderStopped = errors.New("the encoder stopped reading")

func samplesToSeconds(samples int64, sampleRate int) float64 {
	return float64(samples) / float64(sampleRate)
}

// downmix merges every channel into one without cutting anything.
func (p *preparation) downmix() (Prepared, error) {
	args := []string{"-nostdin", "-v", "error", "-y", "-i", p.input, "-map", "0:a:0", "-ac", "1", "-c:a", "flac", "-sample_fmt", "s16", p.output}
	if err := p.ffmpeg("downmixing audio", args...); err != nil {
		return Prepared{}, err
	}
	return Prepared{Path: p.output, Channels: 1}, nil
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
	output, err := run(p.ctx, p.runner, "ffmpeg", args...)
	if err != nil {
		return nil, fmt.Errorf("ffmpeg silence detection failed: %w (output: %s)", err, strings.TrimSpace(string(output)))
	}

	silences, err := parseSilences(output)
	if err != nil {
		return nil, err
	}
	return complement(silences), nil
}

// detectSpeech pipes the input, decoded to the format the speech model reads,
// into the model and keeps what it hears as speech.
func (p *preparation) detectSpeech() ([]timeline.Range, error) {
	ctx, cancel := context.WithCancel(p.ctx)
	defer cancel()

	reader, writer := io.Pipe()
	decoded := make(chan error, 1)
	go func() {
		err := p.stream(ctx, nil, writer, "ffmpeg", p.decodeArgs(1, vadSampleRate)...)
		_ = writer.CloseWithError(err) // nil ends the model's input cleanly
		decoded <- err
	}()

	ranges, detectErr := detectSpeech(reader, p.opts.VAD)
	if detectErr != nil {
		// Stops the decoder, which is otherwise left writing to nobody.
		_ = reader.CloseWithError(detectErr)
		cancel()
	}
	if err := <-decoded; err != nil {
		return nil, fmt.Errorf("ffmpeg decoding audio for voice detection failed: %w", err)
	}
	if detectErr != nil {
		return nil, detectErr
	}
	return ranges, nil
}

// decodeArgs decodes the input's first audio stream to raw signed 16-bit PCM
// on standard output.
func (p *preparation) decodeArgs(channels, sampleRate int) []string {
	return []string{
		"-nostdin", "-v", "error", "-i", p.input, "-map", "0:a:0",
		"-ac", strconv.Itoa(channels), "-ar", strconv.Itoa(sampleRate),
		"-f", "s16le", "-c:a", "pcm_s16le", "pipe:1",
	}
}

func (p *preparation) ffmpeg(step string, args ...string) error {
	output, err := run(p.ctx, p.runner, "ffmpeg", args...)
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
