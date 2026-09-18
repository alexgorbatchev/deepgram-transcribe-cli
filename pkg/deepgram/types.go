package deepgram

import "strings"

// DiarizeModelLatest selects the newest generally available batch diarizer,
// which Deepgram currently resolves to v2. Deepgram documents `diarize_model`
// as the parameter new integrations should send; the older `diarize=true`
// boolean is deprecated, always routes to the v1 diarizer, and is rejected when
// combined with `diarize_model`.
const DiarizeModelLatest = "latest"

// DefaultModel is the model Deepgram is asked for when a request names none.
const DefaultModel = "nova-3"

// Options defines configuration parameters for Deepgram transcription requests.
type Options struct {
	Model           string   // e.g. "nova-3", "nova-2"
	Language        string   // e.g. "en", "en-US"
	DiarizeModel    string   // diarizer to run, e.g. DiarizeModelLatest or "v1"; empty disables speaker diarization
	SmartFormatting bool     // enable punctuation, formatting, dates, etc.
	Utterances      bool     // group output into speaker utterances
	Punctuate       bool     // enable explicit punctuation
	Terms           []string // keyterms / keywords to improve transcription accuracy
}

// EffectiveModel returns the model Deepgram will actually run, which is the one
// asked for or the default this client sends in its place. Both the request URL
// and the cost estimate have to agree on that answer.
func (o Options) EffectiveModel() string {
	if model := strings.TrimSpace(o.Model); model != "" {
		return model
	}
	return DefaultModel
}

// Diarized reports whether the request asks Deepgram to label who is speaking.
func (o Options) Diarized() bool { return o.DiarizeModel != "" }

// Keyterms returns the vocabulary actually worth sending: the terms with the
// surrounding whitespace removed and the empty ones dropped. It is the single
// answer to "does this request boost vocabulary", which decides both what goes
// on the wire and whether Deepgram's keyterm prompting charge applies.
func (o Options) Keyterms() []string {
	keyterms := make([]string, 0, len(o.Terms))
	for _, term := range o.Terms {
		if cleaned := strings.TrimSpace(term); cleaned != "" {
			keyterms = append(keyterms, cleaned)
		}
	}
	return keyterms
}

// PreRecordedResponse represents the top-level JSON response from Deepgram's v1/listen endpoint.
type PreRecordedResponse struct {
	Metadata Metadata `json:"metadata"`
	Results  Results  `json:"results"`
}

// Metadata contains metadata about the audio processing request.
type Metadata struct {
	RequestID string   `json:"request_id"`
	Duration  float64  `json:"duration"`
	Channels  int      `json:"channels"`
	Models    []string `json:"models"`
	Created   string   `json:"created"`
}

// Results contains the transcription channels and utterances.
type Results struct {
	Channels   []Channel   `json:"channels"`
	Utterances []Utterance `json:"utterances"`
}

// Channel contains the alternative transcriptions for a single channel.
type Channel struct {
	Alternatives []Alternative `json:"alternatives"`
}

// Alternative contains a transcribed string and individual word details.
type Alternative struct {
	Transcript string  `json:"transcript"`
	Confidence float64 `json:"confidence"`
	Words      []Word  `json:"words"`
}

// Utterance represents a single continuous speech segment attributed to a specific speaker.
//
// Confidence and SpeakerConfidence answer different questions: the first is how
// sure Deepgram is of the words, the second how sure it is that this speaker
// said them. Only the batch diarizer reports the second one; streaming omits it.
type Utterance struct {
	Start             float64 `json:"start"`
	End               float64 `json:"end"`
	Confidence        float64 `json:"confidence"`
	SpeakerConfidence float64 `json:"speaker_confidence"`
	Channel           int     `json:"channel"`
	Speaker           int     `json:"speaker"`
	Transcript        string  `json:"transcript"`
	Words             []Word  `json:"words"`
}

// Word contains details and timestamps for an individual recognized word.
type Word struct {
	Word              string  `json:"word"`
	Start             float64 `json:"start"`
	End               float64 `json:"end"`
	Confidence        float64 `json:"confidence"`
	Speaker           int     `json:"speaker"`
	SpeakerConfidence float64 `json:"speaker_confidence"`
	PunctuatedWord    string  `json:"punctuated_word"`
}
