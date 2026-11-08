// Package transcriptjson renders a transcript as a versioned JSON document for
// programs to read.
//
// Every time in the document is in seconds on the original recording's
// timeline, whatever was cut before upload. The README and the agent skill
// document the schema; change SchemaVersion whenever a field is removed,
// renamed or changes meaning.
package transcriptjson

import (
	"encoding/json"
	"fmt"

	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/deepgram"
	"github.com/alexgorbatchev/deepgram-transcribe-cli/pkg/timeline"
)

// SchemaVersion identifies the document layout.
const SchemaVersion = 1

// Document is the JSON transcript.
type Document struct {
	Schema              int              `json:"schema"`
	Source              Source           `json:"source"`
	RequestID           string           `json:"request_id"`
	Model               string           `json:"model"`
	Language            string           `json:"language"`
	Diarized            bool             `json:"diarized"`
	Duration            float64          `json:"duration"`
	TranscribedDuration float64          `json:"transcribed_duration"`
	CacheHit            bool             `json:"cache_hit"`
	CostEstimateUSD     *float64         `json:"cost_estimate_usd"`
	KeptRanges          []timeline.Range `json:"kept_ranges"`
	Transcript          string           `json:"transcript"`
	Words               []Word           `json:"words"`
	Utterances          []Utterance      `json:"utterances"`
}

// Source names the transcribed file.
type Source struct {
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"size_bytes"`
}

// Word is one recognized word.
type Word struct {
	Word           string  `json:"word"`
	PunctuatedWord string  `json:"punctuated_word"`
	Start          float64 `json:"start"`
	End            float64 `json:"end"`
	Confidence     float64 `json:"confidence"`
	Speaker        *int    `json:"speaker"`
}

// Utterance is one continuous stretch of speech by one speaker.
type Utterance struct {
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Confidence float64 `json:"confidence"`
	Speaker    *int    `json:"speaker"`
	Transcript string  `json:"transcript"`
}

// Meta carries what the document reports beyond Deepgram's response.
type Meta struct {
	Filename string
	FileSize int64
	Model    string
	Language string
	Diarized bool
	CacheHit bool
	// CostUSD is the estimated cost, or nil when it cannot be estimated.
	CostUSD *float64
	// Timeline is how the uploaded audio relates to the original recording.
	Timeline timeline.Timeline
}

// Format renders resp, whose times must already be on the original timeline
// (see deepgram.PreRecordedResponse.OnTimeline), as an indented JSON document
// followed by a newline.
func Format(resp deepgram.PreRecordedResponse, meta Meta) ([]byte, error) {
	doc := Document{
		Schema:              SchemaVersion,
		Source:              Source{Filename: meta.Filename, SizeBytes: meta.FileSize},
		RequestID:           resp.Metadata.RequestID,
		Model:               meta.Model,
		Language:            meta.Language,
		Diarized:            meta.Diarized,
		Duration:            resp.Metadata.Duration,
		TranscribedDuration: resp.Metadata.Duration,
		CacheHit:            meta.CacheHit,
		CostEstimateUSD:     meta.CostUSD,
		KeptRanges:          meta.Timeline.Kept,
		Words:               []Word{},
		Utterances:          []Utterance{},
	}
	if meta.Timeline.SourceDuration > 0 {
		doc.Duration = meta.Timeline.SourceDuration
	}

	speaker := func(id int) *int {
		if !meta.Diarized {
			return nil
		}
		return &id
	}

	if len(resp.Results.Channels) > 0 && len(resp.Results.Channels[0].Alternatives) > 0 {
		best := resp.Results.Channels[0].Alternatives[0]
		doc.Transcript = best.Transcript
		for _, w := range best.Words {
			doc.Words = append(doc.Words, Word{
				Word:           w.Word,
				PunctuatedWord: w.PunctuatedWord,
				Start:          w.Start,
				End:            w.End,
				Confidence:     w.Confidence,
				Speaker:        speaker(w.Speaker),
			})
		}
	}

	for _, u := range resp.Results.Utterances {
		doc.Utterances = append(doc.Utterances, Utterance{
			Start:      u.Start,
			End:        u.End,
			Confidence: u.Confidence,
			Speaker:    speaker(u.Speaker),
			Transcript: u.Transcript,
		})
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding the JSON transcript: %w", err)
	}
	return append(data, '\n'), nil
}
