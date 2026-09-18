// Package cliout renders command output in the two shapes this CLI supports: a
// polished human mode, and a token-conservative agent mode selected by setting
// AGENT=1 in the environment.
//
// Human mode favours alignment, bordered tables and full-width rules. Agent mode
// drops every decoration that costs tokens without carrying meaning: no rules,
// no tables, no padding. Callers describe what they want to say and let the
// printer decide how it looks, so the two modes can never drift apart.
//
// Failures to write to a terminal are not actionable and are therefore not
// reported: if stdout is gone, there is nowhere left to complain to.
package cliout

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
	"github.com/olekukonko/tablewriter"
)

// fallbackWidth is used when stdout is not a terminal and $COLUMNS is unset,
// which is the normal case when output is piped into a file or another process.
const fallbackWidth = 100

// Printer writes command output in whichever mode the environment selected.
type Printer struct {
	out   io.Writer
	err   io.Writer
	agent bool
	width int
}

// New returns a Printer that writes results to out and progress to err.
//
// Mode and terminal width come from cobra-help-tree so that command output and
// the help screens it renders always agree on both.
func New(out, err io.Writer) *Printer {
	width := cobrahelptree.GetTerminalWidth()
	if width <= 0 {
		width = fallbackWidth
	}

	return &Printer{
		out:   out,
		err:   err,
		agent: cobrahelptree.IsAgentMode(),
		width: width,
	}
}

// IsAgent reports whether output is being written for an automated caller.
func (p *Printer) IsAgent() bool { return p.agent }

// Width returns the number of columns available for a line of output.
func (p *Printer) Width() int { return p.width }

// Out returns the writer that carries command results.
func (p *Printer) Out() io.Writer { return p.out }

// Line writes one line of results.
func (p *Printer) Line(format string, a ...any) {
	write(p.out, format+"\n", a...)
}

// write sends one piece of output to w.
//
// This is the only place where a write error is dropped, and it is dropped
// deliberately: when the terminal a command is writing to has gone away, there is
// nowhere left to report that it has gone away.
func write(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

// Status reports progress. It goes to stderr so that redirecting stdout captures
// only the result, which is what makes `... > transcript.md` work.
func (p *Printer) Status(format string, a ...any) { p.tagged("INFO", format, a...) }

// Warn reports something that did not stop the command but changed its result.
func (p *Printer) Warn(format string, a ...any) { p.tagged("WARN", format, a...) }

// Success reports that a requested change was completed.
func (p *Printer) Success(format string, a ...any) { p.tagged("OK", format, a...) }

// Fail reports why a command could not finish.
func (p *Printer) Fail(format string, a ...any) { p.tagged(p.failTag(), format, a...) }

// Hint suggests how to resolve a failure. Both modes get it, because an agent
// acting on the error benefits from the same suggestion a person would.
func (p *Printer) Hint(format string, a ...any) { p.tagged("HINT", format, a...) }

// Detail reports technical information that only helps an automated caller, such
// as an internal key or an exact size. It is written in agent mode and dropped in
// human mode, where it would be noise.
func (p *Printer) Detail(format string, a ...any) {
	if !p.agent {
		return
	}
	p.tagged("INFO", format, a...)
}

// failTag keeps the error prefix short in agent mode, where it is one of the few
// tokens spent on every failure.
func (p *Printer) failTag() string {
	if p.agent {
		return "ERR"
	}
	return "ERROR"
}

// tagged writes a labelled line to stderr, bracketed for people and suffixed
// with a colon for machines.
func (p *Printer) tagged(tag, format string, a ...any) {
	message := fmt.Sprintf(format, a...)
	if p.agent {
		write(p.err, "%s: %s\n", tag, message)
		return
	}
	write(p.err, "[%s] %s\n", tag, message)
}

// Divider draws a horizontal rule the full width of the terminal. Agent mode has
// no rules, so nothing is written.
func (p *Printer) Divider() {
	if p.agent {
		return
	}
	write(p.out, "%s\n", strings.Repeat("-", p.width))
}

// Heading introduces a block of results. Agent mode drops it, because the field
// keys that follow already say what the values are.
func (p *Printer) Heading(title string) {
	if p.agent {
		return
	}
	write(p.out, "%s\n", title)
	p.Divider()
}

// Blank separates blocks of results in human mode only.
func (p *Printer) Blank() {
	if p.agent {
		return
	}
	write(p.out, "\n")
}

// FieldList collects label and value pairs so that they can be rendered as an
// aligned block for people and as flat key-value lines for machines.
type FieldList struct {
	printer *Printer
	labels  []string
	values  []string
}

// Fields starts a block of label and value pairs.
func (p *Printer) Fields() *FieldList {
	return &FieldList{printer: p}
}

// Add appends one label and its value. Labels are written for people; the
// machine-readable key is derived from the label so the two cannot disagree.
func (f *FieldList) Add(label, format string, a ...any) *FieldList {
	f.labels = append(f.labels, label)
	f.values = append(f.values, fmt.Sprintf(format, a...))
	return f
}

// Render writes the collected fields.
func (f *FieldList) Render() {
	if f.printer.agent {
		for i, label := range f.labels {
			write(f.printer.out, "%s: %s\n", fieldKey(label), f.values[i])
		}
		return
	}

	widest := 0
	for _, label := range f.labels {
		if len(label) > widest {
			widest = len(label)
		}
	}

	for i, label := range f.labels {
		padding := strings.Repeat(" ", widest-len(label))
		write(f.printer.out, "%s:%s  %s\n", label, padding, f.values[i])
	}
}

// Table writes rows under the given headers: a bordered table for people, and
// tab-separated lines for machines, which must never be given a table to parse.
func (p *Printer) Table(headers []string, rows [][]string) error {
	if p.agent {
		keys := make([]string, len(headers))
		for i, header := range headers {
			keys[i] = fieldKey(header)
		}
		write(p.out, "%s\n", strings.Join(keys, "\t"))
		for _, row := range rows {
			write(p.out, "%s\n", strings.Join(row, "\t"))
		}
		return nil
	}

	table := tablewriter.NewTable(p.out, tablewriter.WithMaxWidth(p.width))
	table.Header(headers)
	if err := table.Bulk(rows); err != nil {
		return fmt.Errorf("adding table rows: %w", err)
	}
	if err := table.Render(); err != nil {
		return fmt.Errorf("rendering table: %w", err)
	}
	return nil
}

// Bullets writes a short list of related items, indented under whatever was said
// last. Agent mode uses the same marker without the leading indent.
func (p *Printer) Bullets(items []string) {
	for _, item := range items {
		if p.agent {
			write(p.err, "- %s\n", item)
			continue
		}
		write(p.err, "  - %s\n", item)
	}
}

// Report writes a failed command's error, followed by its resolution hint when
// one was attached with WithHint.
func (p *Printer) Report(err error) {
	if err == nil {
		return
	}

	p.Fail("%v", err)

	var hinted *HintedError
	if errors.As(err, &hinted) && hinted.Hint != "" {
		p.Hint("%s", hinted.Hint)
	}
}

// HintedError pairs a failure with the action that would resolve it.
type HintedError struct {
	Err  error
	Hint string
}

// Error returns the underlying failure message.
func (e *HintedError) Error() string { return e.Err.Error() }

// Unwrap exposes the underlying failure to errors.Is and errors.As.
func (e *HintedError) Unwrap() error { return e.Err }

// WithHint attaches a resolution hint to err so that the command that reports it
// can tell the user what to do next.
func WithHint(err error, hint string) error {
	if err == nil {
		return nil
	}
	return &HintedError{Err: err, Hint: hint}
}

// fieldKey converts a human label such as "Request ID" into the machine key
// "request_id", so that agent output is predictable to parse.
func fieldKey(label string) string {
	var sb strings.Builder
	previousUnderscore := false

	for _, r := range strings.ToLower(strings.TrimSpace(label)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
			previousUnderscore = false
			continue
		}
		if !previousUnderscore && sb.Len() > 0 {
			sb.WriteRune('_')
			previousUnderscore = true
		}
	}

	return strings.TrimSuffix(sb.String(), "_")
}
