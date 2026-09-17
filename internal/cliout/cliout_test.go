package cliout

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// newTestPrinter returns a Printer in the requested mode along with the buffers
// it writes to.
func newTestPrinter(t *testing.T, agent bool) (*Printer, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	if agent {
		t.Setenv("AGENT", "1")
	} else {
		t.Setenv("AGENT", "")
	}
	t.Setenv("COLUMNS", "40")

	var out, errOut bytes.Buffer

	return New(&out, &errOut), &out, &errOut
}

func TestFieldKey(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  string
	}{
		{"single word", "Model", "model"},
		{"two words", "Request ID", "request_id"},
		{"punctuation", "SHA-256 Hash", "sha_256_hash"},
		{"padded", "  Total billed  ", "total_billed"},
		{"trailing punctuation", "Cost?", "cost"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fieldKey(tt.label); got != tt.want {
				t.Errorf("fieldKey(%q) = %q, want %q", tt.label, got, tt.want)
			}
		})
	}
}

func TestFieldsAlignForPeopleAndStayFlatForAgents(t *testing.T) {
	human, humanOut, _ := newTestPrinter(t, false)
	human.Fields().
		Add("File name", "call.m4a").
		Add("Request ID", "req-1").
		Render()

	want := "File name:   call.m4a\nRequest ID:  req-1\n"
	if got := humanOut.String(); got != want {
		t.Errorf("human fields = %q, want %q", got, want)
	}

	agent, agentOut, _ := newTestPrinter(t, true)
	agent.Fields().
		Add("File name", "call.m4a").
		Add("Request ID", "req-1").
		Render()

	want = "file_name: call.m4a\nrequest_id: req-1\n"
	if got := agentOut.String(); got != want {
		t.Errorf("agent fields = %q, want %q", got, want)
	}
}

func TestTableIsBorderedForPeopleAndTabSeparatedForAgents(t *testing.T) {
	headers := []string{"Date", "Request ID"}
	rows := [][]string{{"2026-03-31", "req-1"}}

	human, humanOut, _ := newTestPrinter(t, false)
	if err := human.Table(headers, rows); err != nil {
		t.Fatalf("rendering the human table: %v", err)
	}
	if !strings.ContainsAny(humanOut.String(), "│┌└├") {
		t.Errorf("expected a bordered table, got:\n%s", humanOut.String())
	}

	agent, agentOut, _ := newTestPrinter(t, true)
	if err := agent.Table(headers, rows); err != nil {
		t.Fatalf("rendering the agent table: %v", err)
	}

	want := "date\trequest_id\n2026-03-31\treq-1\n"
	if got := agentOut.String(); got != want {
		t.Errorf("agent table = %q, want %q", got, want)
	}
}

func TestDecorationIsHumanModeOnly(t *testing.T) {
	human, humanOut, _ := newTestPrinter(t, false)
	human.Heading("Details")
	human.Blank()

	if !strings.Contains(humanOut.String(), "Details\n"+strings.Repeat("-", 40)) {
		t.Errorf("expected a heading followed by a full width rule, got:\n%s", humanOut.String())
	}

	agent, agentOut, _ := newTestPrinter(t, true)
	agent.Heading("Details")
	agent.Divider()
	agent.Blank()

	if agentOut.String() != "" {
		t.Errorf("expected no decoration in agent mode, got:\n%s", agentOut.String())
	}
}

func TestStatusTagsDifferByMode(t *testing.T) {
	human, _, humanErr := newTestPrinter(t, false)
	human.Status("working")
	human.Warn("careful")
	human.Success("done")
	human.Fail("broken")
	human.Detail("internal key abc")

	want := "[INFO] working\n[WARN] careful\n[OK] done\n[ERROR] broken\n"
	if got := humanErr.String(); got != want {
		t.Errorf("human statuses = %q, want %q", got, want)
	}

	agent, _, agentErr := newTestPrinter(t, true)
	agent.Status("working")
	agent.Fail("broken")
	agent.Detail("internal key abc")

	want = "INFO: working\nERR: broken\nINFO: internal key abc\n"
	if got := agentErr.String(); got != want {
		t.Errorf("agent statuses = %q, want %q", got, want)
	}
}

func TestReportIncludesTheAttachedHint(t *testing.T) {
	printer, _, errOut := newTestPrinter(t, false)

	base := errors.New("no Deepgram API key was given")
	printer.Report(WithHint(base, "Set DEEPGRAM_API_KEY."))

	got := errOut.String()
	if !strings.Contains(got, "[ERROR] no Deepgram API key was given") {
		t.Errorf("expected the failure to be reported, got: %s", got)
	}
	if !strings.Contains(got, "[HINT] Set DEEPGRAM_API_KEY.") {
		t.Errorf("expected the hint to be reported, got: %s", got)
	}
}

func TestHintedErrorUnwrapsToItsCause(t *testing.T) {
	base := errors.New("underlying failure")
	wrapped := WithHint(base, "try again")

	if !errors.Is(wrapped, base) {
		t.Error("expected the hinted error to unwrap to its cause")
	}
	if wrapped.Error() != base.Error() {
		t.Errorf("expected the cause's message, got %q", wrapped.Error())
	}
	if WithHint(nil, "ignored") != nil {
		t.Error("expected no error when there is nothing to hint about")
	}
}

func TestBulletsIndentForPeopleOnly(t *testing.T) {
	human, _, humanErr := newTestPrinter(t, false)
	human.Bullets([]string{"one", "two"})

	if got := humanErr.String(); got != "  - one\n  - two\n" {
		t.Errorf("human bullets = %q", got)
	}

	agent, _, agentErr := newTestPrinter(t, true)
	agent.Bullets([]string{"one"})

	if got := agentErr.String(); got != "- one\n" {
		t.Errorf("agent bullets = %q", got)
	}
}
