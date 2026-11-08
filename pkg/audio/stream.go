package audio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"

	"github.com/alexgorbatchev/godeps"
)

// StreamRunner runs a program with its standard input read from stdin and its
// standard output written to stdout; a nil stdin reads nothing and a nil
// stdout discards the output. The returned error carries the program's
// diagnostics.
type StreamRunner func(ctx context.Context, stdin io.Reader, stdout io.Writer, name string, args ...string) error

// DefaultStreamRunner runs the program with os/exec. A program stopped
// because ctx ended reports ctx's error, so cancellation stays recognizable.
func DefaultStreamRunner(ctx context.Context, stdin io.Reader, stdout io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return withCancellation(ctxErr, err)
		}
		if diagnostics := godeps.SanitizeStderr(stderr.String()); diagnostics != "" {
			return fmt.Errorf("%w: %s", err, diagnostics)
		}
		return err
	}
	return nil
}

// run runs a program whose output is captured. exec reports a program killed
// because ctx ended as "signal: killed", so the error then carries ctx's error
// too and cancellation stays recognizable.
func run(ctx context.Context, runner godeps.CommandRunner, name string, args ...string) ([]byte, error) {
	output, err := runner(ctx, name, args...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return output, withCancellation(ctxErr, err)
		}
	}
	return output, err
}

func withCancellation(ctxErr, err error) error {
	if errors.Is(err, ctxErr) {
		return err
	}
	return fmt.Errorf("%w: %w", ctxErr, err)
}

// spanFilter receives decoded interleaved PCM and passes on only the bytes
// inside its spans, end to end. It counts every byte it receives, so once the
// decoder has finished it knows the decoded length exactly.
type spanFilter struct {
	dst        io.Writer
	spans      []sampleSpan // in frames, sorted and disjoint; ends may lie past the audio
	frameBytes int64
	received   int64 // bytes seen so far
	next       int   // first span not yet fully passed on
	writeErr   error // why dst stopped accepting audio
}

func (f *spanFilter) Write(chunk []byte) (int, error) {
	start := f.received
	end := start + int64(len(chunk))
	f.received = end

	for f.next < len(f.spans) {
		s := f.spans[f.next]
		from := max(s.start*f.frameBytes, start)
		to := min(s.end*f.frameBytes, end)
		if from >= end {
			break
		}
		if from < to {
			if _, err := f.dst.Write(chunk[from-start : to-start]); err != nil {
				f.writeErr = err
				return 0, err
			}
		}
		if s.end*f.frameBytes > end {
			break
		}
		f.next++
	}
	return len(chunk), nil
}

// frames returns how many whole frames the filter has received.
func (f *spanFilter) frames() int64 { return f.received / f.frameBytes }
