// Package timeline maps times on trimmed audio back to the recording it was cut
// from.
//
// Trimming keeps a list of ranges of the original recording and joins them end
// to end. A time on the trimmed audio therefore lies in exactly one kept range,
// found by walking the ranges and summing their lengths, and maps back to the
// start of that range plus the distance into it.
package timeline

import "math"

// precision is the number of mapped values per second that are kept. Mapping
// adds and subtracts offsets, so without rounding a word Deepgram placed at
// 0.48 s could come back as 9.620000000000001 s.
const precision = 1e6

// Range is a span of the original recording, in seconds from its start.
type Range struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// Length returns how many seconds the range spans.
func (r Range) Length() float64 { return r.End - r.Start }

// Timeline describes how uploaded audio relates to the original recording.
//
// The zero value is the identity: the upload is the recording itself.
type Timeline struct {
	// SourceDuration is the original recording's length in seconds, or 0 when
	// it was not measured.
	SourceDuration float64 `json:"source_duration,omitempty"`
	// Kept lists the ranges of the original recording that were uploaded, in
	// order and without overlap. Empty means nothing was cut.
	Kept []Range `json:"kept,omitempty"`
}

// Trimmed reports whether the upload omitted parts of the recording.
func (t Timeline) Trimmed() bool { return len(t.Kept) > 0 }

// Start maps the time a span begins on the trimmed audio to the original
// recording. A time exactly on a joint belongs to the range that begins there,
// because that is where the span's audio starts.
func (t Timeline) Start(at float64) float64 {
	if !t.Trimmed() {
		return round(at)
	}
	index, offset := t.locate(at, false)
	return t.place(index, at-offset)
}

// End maps the time a span finishes on the trimmed audio to the original
// recording. A time exactly on a joint belongs to the range that ends there,
// because that is where the span's audio stops.
func (t Timeline) End(at float64) float64 {
	if !t.Trimmed() {
		return round(at)
	}
	index, offset := t.locate(at, true)
	return t.place(index, at-offset)
}

// Span maps a word's start and end to the original recording.
//
// A word that crosses a joint on the trimmed audio has no single true position
// in the recording: the audio on either side of the joint was not contiguous.
// The word is placed in the kept range that holds the largest share of it and
// clipped to that range, so it never covers audio that was removed. Spans that
// stay inside one range map exactly.
func (t Timeline) Span(start, end float64) (float64, float64) {
	if !t.Trimmed() {
		return round(start), round(end)
	}

	first, _ := t.locate(start, false)
	last, _ := t.locate(end, true)
	switch {
	case first == last:
		return t.Start(start), t.End(end)
	case first > last:
		// A zero-length span sitting exactly on a joint.
		point := t.Start(start)
		return point, point
	}

	best, bestShare, bestOffset := first, -1.0, 0.0
	offset := 0.0
	for i, r := range t.Kept {
		rangeEnd := offset + r.Length()
		if i >= first && i <= last {
			share := math.Min(end, rangeEnd) - math.Max(start, offset)
			if share > bestShare {
				best, bestShare, bestOffset = i, share, offset
			}
		}
		offset = rangeEnd
	}

	clippedStart := math.Max(start, bestOffset)
	clippedEnd := math.Min(end, bestOffset+t.Kept[best].Length())
	return t.place(best, clippedStart-bestOffset), t.place(best, clippedEnd-bestOffset)
}

// locate returns the kept range that holds a trimmed-audio time and where that
// range begins on the trimmed audio. atEnd selects the earlier range for a time
// exactly on a joint. Times before the start or past the end of the trimmed
// audio resolve to the first or the last range.
func (t Timeline) locate(at float64, atEnd bool) (int, float64) {
	offset := 0.0
	for i, r := range t.Kept {
		rangeEnd := offset + r.Length()
		if at < rangeEnd || (atEnd && at == rangeEnd) {
			return i, offset
		}
		offset = rangeEnd
	}
	last := len(t.Kept) - 1
	return last, offset - t.Kept[last].Length()
}

// place returns the original-recording time that lies distance seconds into
// kept range index, clamped to that range.
func (t Timeline) place(index int, distance float64) float64 {
	r := t.Kept[index]
	return round(math.Min(math.Max(r.Start+distance, r.Start), r.End))
}

func round(seconds float64) float64 { return math.Round(seconds*precision) / precision }
