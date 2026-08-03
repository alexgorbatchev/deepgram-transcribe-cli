package deepgram

// Options defines configuration parameters for Deepgram transcription requests.
type Options struct {
	Model           string   // e.g. "nova-3", "nova-2"
	Language        string   // e.g. "en", "en-US"
	Diarize         bool     // enable speaker diarization
	SmartFormatting bool     // enable punctuation, formatting, dates, etc.
	Utterances      bool     // group output into speaker utterances
	Punctuate       bool     // enable explicit punctuation
	Terms           []string // keyterms / keywords to improve transcription accuracy
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
	Transcript string `json:"transcript"`
	Confidence float64`json:"confidence"`
	Words      []Word `json:"words"`
}

// Utterance represents a single continuous speech segment attributed to a specific speaker.
type Utterance struct {
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Confidence float64 `json:"confidence"`
	Channel    int     `json:"channel"`
	Speaker    int     `json:"speaker"`
	Transcript string  `json:"transcript"`
	Words      []Word  `json:"words"`
}

// Word contains details and timestamps for an individual recognized word.
type Word struct {
	Word           string  `json:"word"`
	Start          float64 `json:"start"`
	End            float64 `json:"end"`
	Confidence     float64 `json:"confidence"`
	Speaker        int     `json:"speaker"`
	PunctuatedWord string  `json:"punctuated_word"`
}
