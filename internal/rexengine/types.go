package rexengine

// RexMetadata holds the file-level attributes recovered from REXInfo and REXCreatorInfo.
type RexMetadata struct {
	Channels      int
	SampleRate    int
	Tempo         float64 // Stored in C as 123456 -> mapped back to 123.456
	OriginalTempo float64
	TimeSignNom   int
	TimeSignDenom int
	BitDepth      int
	PPQLength     int     // fPPQLength from REXInfo — total PPQ ticks in loop
	CreatorName   string
	Copyright     string
}

// RexSlicePayload represents a completely standalone slice block extracted by C.
// This allows Go to mix, match, chop, or combine slices across files in parallel.
type RexSlicePayload struct {
	SliceIndex  int
	PPQPos      int       // The original timing position from ReCycle
	FrameLength int       // Number of audio frames in this slice
	PCMData     []float32 // Interleaved audio data for just this single slice
}

// RawExtraction represents the raw assets returned immediately by the single C thread.
type RawExtraction struct {
	Metadata RexMetadata
	Slices   []RexSlicePayload
}

// WavCueMarker holds the precise mathematical frame location for the Go encoder chunk.
// This is used right before writing the final stitched files to disk.
type WavCueMarker struct {
	SliceID  int    // Unique index identifier
	Position uint32 // The exact sample/frame offset within the final stitched file output
	Label    string // Slice name metadata (e.g., "Slice 01")
}

// SliceExtraction contains the complete analytical asset payload returned by a worker.
type SliceExtraction struct {
	Metadata    RexMetadata
	CuePoints   []WavCueMarker
	Interleaved []float32 // All slice audio data stitched sequentially into a single continuous float buffer
	TotalFrames int       // Cumulative audio frame length
}
