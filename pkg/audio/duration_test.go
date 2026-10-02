package audio

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestDuration(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		runErr       error
		want         float64
	}{
		{"valid", `{"streams":[{"duration":"12.345"}]}`, nil, 12.345},
		{"missing stream", `{"streams":[]}`, nil, 0},
		{"missing duration", `{"streams":[{}]}`, nil, 0},
		{"unavailable", `{"streams":[{"duration":"N/A"}]}`, nil, 0},
		{"zero", `{"streams":[{"duration":"0"}]}`, nil, 0},
		{"negative", `{"streams":[{"duration":"-1"}]}`, nil, 0},
		{"not finite", `{"streams":[{"duration":"NaN"}]}`, nil, 0},
		{"infinite", `{"streams":[{"duration":"+Inf"}]}`, nil, 0},
		{"invalid JSON", `{`, nil, 0},
		{"probe failure", "", errors.New("probe failed"), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := func(ctx context.Context, name string, args ...string) ([]byte, error) {
				wantArgs := []string{"-v", "error", "-select_streams", "a:0", "-show_entries", "stream=duration", "-of", "json", "-i", "-recording.opus"}
				if name != "ffprobe" || !reflect.DeepEqual(args, wantArgs) || ctx != t.Context() {
					t.Fatalf("unexpected probe: %s %v", name, args)
				}
				return []byte(tc.output), tc.runErr
			}
			got, err := Duration(t.Context(), "-recording.opus", runner)
			if got != tc.want || (err != nil) != (tc.want == 0) {
				t.Fatalf("duration %v, error %v", got, err)
			}
			if tc.runErr != nil && !errors.Is(err, tc.runErr) {
				t.Fatalf("lost probe error: %v", err)
			}
		})
	}
}
