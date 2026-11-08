package audio

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
)

// helperProcessEnv makes the test binary act as the program
// DefaultStreamRunner runs, so the test needs no external program.
const helperProcessEnv = "AUDIO_STREAM_HELPER"

func TestMain(m *testing.M) {
	switch os.Getenv(helperProcessEnv) {
	case "":
		os.Exit(m.Run())
	case "echo":
		// Upper-cases stdin to stdout.
		data, _ := io.ReadAll(os.Stdin)
		_, _ = os.Stdout.Write(bytes.ToUpper(data))
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, "decoder exploded")
		os.Exit(3)
	}
}

func TestDefaultStreamRunnerPipesStdinToStdout(t *testing.T) {
	t.Setenv(helperProcessEnv, "echo")
	var out bytes.Buffer
	if err := DefaultStreamRunner(t.Context(), strings.NewReader("pcm"), &out, os.Args[0]); err != nil {
		t.Fatal(err)
	}
	if out.String() != "PCM" {
		t.Fatalf("stdout %q", out.String())
	}
}

func TestDefaultStreamRunnerReportsDiagnostics(t *testing.T) {
	t.Setenv(helperProcessEnv, "fail")
	err := DefaultStreamRunner(t.Context(), nil, nil, os.Args[0])
	if err == nil || !strings.Contains(err.Error(), "decoder exploded") || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("got %v", err)
	}
}

func TestSpanFilterPassesOnOnlyItsSpans(t *testing.T) {
	pcm := countingPCM(20, 2)
	var out bytes.Buffer
	filter := &spanFilter{dst: &out, spans: []sampleSpan{{2, 5}, {9, 10}, {15, unboundedSamples}}, frameBytes: 4}
	for chunk := range slices.Chunk(pcm, 3) {
		if n, err := filter.Write(chunk); err != nil || n != len(chunk) {
			t.Fatalf("wrote %d, error %v", n, err)
		}
	}
	want := append(append(span(2, 5), 9), span(15, 20)...)
	if got := framesOf(t, out.Bytes(), 2); !slices.Equal(got, want) || filter.frames() != 20 {
		t.Fatalf("frames %v of %d, want %v of 20", got, filter.frames(), want)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errEncoderStopped }

func TestSpanFilterRemembersWhyItsOutputStopped(t *testing.T) {
	filter := &spanFilter{dst: brokenWriter{}, spans: []sampleSpan{{0, 1}}, frameBytes: 2}
	if _, err := filter.Write([]byte{1, 2}); !errors.Is(err, errEncoderStopped) || !errors.Is(filter.writeErr, errEncoderStopped) {
		t.Fatalf("error %v, remembered %v", err, filter.writeErr)
	}
}
