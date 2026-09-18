package markdown

import (
	"fmt"
	"strings"
	"time"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
)

// MetaInfo contains contextual file and transcription metadata for the Markdown report header.
type MetaInfo struct {
	Filename  string
	FileSize  string
	Model     string
	Diarized  bool
	KeyTerms  []string
	Timestamp time.Time
}

// Format takes a Deepgram response and metadata and builds a Markdown string.
func Format(resp *deepgram.PreRecordedResponse, meta MetaInfo) string {
	var sb strings.Builder

	// Header
	fmt.Fprintf(&sb, "# Transcript: %s\n\n", meta.Filename)

	// Metadata Box / Table
	sb.WriteString("## Metadata\n\n")
	sb.WriteString("| Property | Value |\n")
	sb.WriteString("| :--- | :--- |\n")

	if !meta.Timestamp.IsZero() {
		fmt.Fprintf(&sb, "| **Date** | %s |\n", meta.Timestamp.Format("2006-01-02 15:04:05 MST"))
	}
	if meta.FileSize != "" {
		fmt.Fprintf(&sb, "| **File Size** | %s |\n", meta.FileSize)
	}
	if resp != nil && resp.Metadata.Duration > 0 {
		fmt.Fprintf(&sb, "| **Duration** | %s |\n", deepgram.FormatSeconds(resp.Metadata.Duration))
	}
	if meta.Model != "" {
		fmt.Fprintf(&sb, "| **Model** | `%s` |\n", meta.Model)
	}
	fmt.Fprintf(&sb, "| **Diarization** | `%t` |\n", meta.Diarized)

	if len(meta.KeyTerms) > 0 {
		var termsSummary string
		if len(meta.KeyTerms) > 15 {
			termsSummary = fmt.Sprintf("%s *(and %d more)*", strings.Join(meta.KeyTerms[:15], ", "), len(meta.KeyTerms)-15)
		} else {
			termsSummary = strings.Join(meta.KeyTerms, ", ")
		}
		fmt.Fprintf(&sb, "| **Key Terms** | %s |\n", termsSummary)
	}

	sb.WriteString("\n---\n\n")
	// "Transcript", not "Conversation": the audio may be a single speaker.
	sb.WriteString("## Transcript\n\n")

	if resp == nil {
		sb.WriteString("*(No transcript data available)*\n")
		return sb.String()
	}

	utterances := resp.Results.Utterances
	if len(utterances) > 0 {
		formatUtterances(&sb, utterances)
	} else if len(resp.Results.Channels) > 0 && len(resp.Results.Channels[0].Alternatives) > 0 {
		transcript := resp.Results.Channels[0].Alternatives[0].Transcript
		if strings.TrimSpace(transcript) != "" {
			sb.WriteString(transcript)
			sb.WriteString("\n")
		} else {
			sb.WriteString("*(Empty transcript)*\n")
		}
	} else {
		sb.WriteString("*(Empty transcript)*\n")
	}

	return sb.String()
}

// formatUtterances groups and formats speaker utterances into clean Markdown speaker blocks.
func formatUtterances(sb *strings.Builder, utterances []deepgram.Utterance) {
	if len(utterances) == 0 {
		return
	}

	type speakerBlock struct {
		speaker    int
		startTime  float64
		endTime    float64
		paragraphs []string
	}

	var blocks []speakerBlock
	var currentBlock *speakerBlock

	for _, u := range utterances {
		text := strings.TrimSpace(u.Transcript)
		if text == "" {
			continue
		}

		if currentBlock == nil || currentBlock.speaker != u.Speaker {
			if currentBlock != nil {
				blocks = append(blocks, *currentBlock)
			}
			currentBlock = &speakerBlock{
				speaker:    u.Speaker,
				startTime:  u.Start,
				endTime:    u.End,
				paragraphs: []string{text},
			}
		} else {
			currentBlock.endTime = u.End
			currentBlock.paragraphs = append(currentBlock.paragraphs, text)
		}
	}

	if currentBlock != nil {
		blocks = append(blocks, *currentBlock)
	}

	for _, b := range blocks {
		timeRange := fmt.Sprintf("%s - %s", deepgram.FormatSeconds(b.startTime), deepgram.FormatSeconds(b.endTime))
		fmt.Fprintf(sb, "### Speaker %d (%s)\n\n", b.speaker, timeRange)
		for _, para := range b.paragraphs {
			sb.WriteString(para)
			sb.WriteString("\n\n")
		}
	}
}
