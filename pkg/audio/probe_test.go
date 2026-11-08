package audio

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestProbe(t *testing.T) {
	valid := StreamInfo{Duration: 12.345, SampleRate: 48000, Channels: 2}
	for _, tc := range []struct {
		name, output string
		runErr       error
		want         StreamInfo
	}{
		{"valid", `{"streams":[{"duration":"12.345","sample_rate":"48000","channels":2}]}`, nil, valid},
		{"missing stream", `{"streams":[]}`, nil, StreamInfo{}},
		{"missing duration", `{"streams":[{"sample_rate":"48000","channels":2}]}`, nil, StreamInfo{}},
		{"unavailable", `{"streams":[{"duration":"N/A","sample_rate":"48000","channels":2}]}`, nil, StreamInfo{}},
		{"zero", `{"streams":[{"duration":"0","sample_rate":"48000","channels":2}]}`, nil, StreamInfo{}},
		{"negative", `{"streams":[{"duration":"-1","sample_rate":"48000","channels":2}]}`, nil, StreamInfo{}},
		{"not finite", `{"streams":[{"duration":"NaN","sample_rate":"48000","channels":2}]}`, nil, StreamInfo{}},
		{"infinite", `{"streams":[{"duration":"+Inf","sample_rate":"48000","channels":2}]}`, nil, StreamInfo{}},
		{"missing sample rate", `{"streams":[{"duration":"1","channels":2}]}`, nil, StreamInfo{}},
		{"missing channels", `{"streams":[{"duration":"1","sample_rate":"48000"}]}`, nil, StreamInfo{}},
		{"invalid JSON", `{`, nil, StreamInfo{}},
		{"probe failure", "", errors.New("probe failed"), StreamInfo{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
				wantArgs := []string{"-v", "error", "-select_streams", "a:0", "-show_entries", "stream=duration,sample_rate,channels", "-of", "json", "-i", "-recording.opus"}
				if name != "ffprobe" || !reflect.DeepEqual(args, wantArgs) || ctx != t.Context() {
					t.Fatalf("unexpected probe: %s %v", name, args)
				}
				return []byte(tc.output), tc.runErr
			}
			got, err := Probe(t.Context(), "-recording.opus", runner)
			if got != tc.want || (err != nil) != (tc.want == StreamInfo{}) {
				t.Fatalf("stream %+v, error %v", got, err)
			}
			if tc.runErr != nil && !errors.Is(err, tc.runErr) {
				t.Fatalf("lost probe error: %v", err)
			}
		})
	}
}
