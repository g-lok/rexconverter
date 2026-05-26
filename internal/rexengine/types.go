package rexengine

// RexMetadata holds file-level attributes from the REX SDK.
type RexMetadata struct {
	Channels      int
	SampleRate    int
	Tempo         float64 // From SDK as BPM*1000 (e.g. 123456 → 123.456)
	OriginalTempo float64
	TimeSignNom   int
	TimeSignDenom int
	BitDepth      int
	PPQLength     int     // Loop length in PPQ ticks
	CreatorName   string
	Copyright     string
}

// WavCueMarker maps a slice boundary to a sample position in the output WAV.
type WavCueMarker struct {
	SliceID  int
	Position uint32 // Frame offset within the output file
	Label    string
}

// SliceExtraction holds rendered PCM data and metadata for one or more slices.
type SliceExtraction struct {
	Metadata    RexMetadata
	CuePoints   []WavCueMarker
	Interleaved []float32 // Channel-interleaved float32 PCM
	TotalFrames int       // Number of audio frames
}
