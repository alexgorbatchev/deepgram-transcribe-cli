package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/internal/cliout"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
)

// costPending is shown for a job whose real charge Deepgram has not published yet.
const costPending = "not billed yet"

// newJobCmd builds the job subject.
func newJobCmd(g *globalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "job",
		Short: "Review past transcriptions and what they cost",
	}

	cmd.AddCommand(
		newJobInspectCmd(g),
		newJobListCmd(g),
	)

	return cmd
}

// newJobListCmd builds the job list verb.
func newJobListCmd(g *globalOptions) *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List past transcriptions and total spending",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJobList(cmd, g, limit)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Show only this many of the most recent transcriptions")

	return cmd
}

// newJobInspectCmd builds the job inspect verb.
func newJobInspectCmd(g *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <audio-file|request-id>",
		Short: "Show the details and cost of one past transcription",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJobInspect(cmd, g, args[0])
		},
	}
}

func runJobList(cmd *cobra.Command, g *globalOptions, limit int) error {
	out := cliout.New(cmd.OutOrStdout(), cmd.ErrOrStderr())
	cacheDir := g.resolvedCacheDir()

	records, err := deepgram.ListJobRecords(cacheDir)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		out.Status("No transcriptions have been made yet.")
		return nil
	}

	if limit > 0 && limit < len(records) {
		records = records[:limit]
	}

	refreshActualCosts(cmd.Context(), out, g, cacheDir, records)

	rows := make([][]string, 0, len(records))
	var totalDuration, totalBilled float64
	billedCount := 0

	for _, record := range records {
		date := "unknown"
		if !record.Timestamp.IsZero() {
			date = record.Timestamp.Format("2006-01-02")
		}

		cost := costPending
		if record.CostIsActual {
			cost = record.CostUSD
			billedCount++
			totalBilled += parseCostUSD(record.CostUSD)
		}

		rows = append(rows, []string{
			date,
			record.Filename,
			deepgram.FormatSeconds(record.DurationSeconds),
			strconv.Itoa(record.Channels),
			record.Model,
			cost,
			record.RequestID,
		})

		totalDuration += record.DurationSeconds
	}

	headers := []string{"Date", "File", "Duration", "Channels", "Model", "Cost", "Request ID"}
	if err := out.Table(headers, rows); err != nil {
		return err
	}

	out.Blank()

	summary := out.Fields().
		Add("Transcriptions", "%d", len(records)).
		Add("Total duration", "%s", deepgram.FormatSeconds(totalDuration))

	switch {
	case billedCount == len(records):
		summary.Add("Total billed", "$%.3f", totalBilled)
	case billedCount > 0:
		summary.Add("Total billed", "$%.3f, covering %d of %d transcriptions", totalBilled, billedCount, len(records))
	default:
		summary.Add("Total billed", "%s", costPending)
	}

	summary.Render()

	return nil
}

func runJobInspect(cmd *cobra.Command, g *globalOptions, target string) error {
	out := cliout.New(cmd.OutOrStdout(), cmd.ErrOrStderr())
	cacheDir := g.resolvedCacheDir()

	record, err := deepgram.FindJobRecordByTarget(cacheDir, target)
	if err != nil {
		return cliout.WithHint(
			fmt.Errorf("looking up %q: %w", target, err),
			"Run `deepgram-transcribe job list` to see the recordings that have been transcribed.",
		)
	}

	found := []deepgram.JobRecord{*record}
	refreshActualCosts(cmd.Context(), out, g, cacheDir, found)
	record = &found[0]

	out.Heading("Transcription details")

	fields := out.Fields().Add("File name", "%s", record.Filename)
	if record.FilePath != "" {
		fields.Add("Original path", "%s", record.FilePath)
	}
	fields.Add("Request ID", "%s", record.RequestID)
	if !record.Timestamp.IsZero() {
		fields.Add("Transcribed at", "%s", record.Timestamp.Format("2006-01-02 15:04:05 MST"))
	}
	fields.
		Add("Duration", "%s", deepgram.FormatSeconds(record.DurationSeconds)).
		Add("Channels", "%d", record.Channels).
		Add("Made smaller first", "%t", record.Preprocessed).
		Add("Model", "%s", record.Model)

	if record.CostIsActual {
		fields.Add("Cost", "%s", record.CostUSD)
	} else {
		fields.Add("Cost", "%s, estimated at %s", costPending, record.CostUSD)
	}

	fields.Render()

	return nil
}

// refreshActualCosts fills in what Deepgram really charged for any record that
// still carries an estimate, and saves the answer so it is asked for only once.
//
// Requests run concurrently because each one is a separate round trip, but the
// results are applied on this goroutine so that nothing else writes to the cache
// while it is being read.
func refreshActualCosts(ctx context.Context, out *cliout.Printer, g *globalOptions, cacheDir string, records []deepgram.JobRecord) {
	client := g.newClient()
	if client == nil {
		return
	}

	type costResult struct {
		index int
		cost  string
		err   error
	}

	var wg sync.WaitGroup
	results := make(chan costResult, len(records))

	for i, record := range records {
		if record.CostIsActual || record.RequestID == "" {
			continue
		}

		wg.Add(1)
		go func(index int, requestID string) {
			defer wg.Done()
			cost, err := client.GetRequestCostFormatted(ctx, requestID)
			results <- costResult{index: index, cost: cost, err: err}
		}(i, record.RequestID)
	}

	wg.Wait()
	close(results)

	for result := range results {
		if result.err != nil {
			out.Detail("cost lookup failed for %s: %v", records[result.index].RequestID, result.err)
			continue
		}
		if result.cost == "" {
			continue
		}

		records[result.index].CostUSD = result.cost
		records[result.index].CostIsActual = true

		if err := deepgram.SaveJobRecord(cacheDir, records[result.index]); err != nil {
			out.Warn("The billed cost for %s could not be saved: %v", records[result.index].RequestID, err)
		}
	}
}

// parseCostUSD reads a stored cost such as "$0.043" back into a number. An
// unreadable value contributes nothing rather than breaking the total.
func parseCostUSD(cost string) float64 {
	value, err := strconv.ParseFloat(strings.TrimPrefix(strings.TrimSpace(cost), "$"), 64)
	if err != nil {
		return 0
	}
	return value
}
