package rexengine

import (
	"fmt"
	"math"
)

// ProcessSlices takes raw extracted REX components and splits/stitches them into
// ready-to-encode SliceExtraction chunks based on a maximum slice limit.
func ProcessSlices(raw *RawExtraction, maxSlices int, normalize bool) ([]SliceExtraction, error) {
	if raw == nil || len(raw.Slices) == 0 {
		return nil, fmt.Errorf("no raw extraction data or slices found to process")
	}

	var results []SliceExtraction
	totalSlices := len(raw.Slices)

	// SCENARIO 1: If maxSlices is 0 or less, treat everything as a single continuous file
	if maxSlices <= 0 {
		return []SliceExtraction{stitchGroup(raw, raw.Slices)}, nil
	}

	// Array tracking exactly how many slices to put in each separate split file
	var partitionSizes []int

	// SCENARIO 2: If slice limits are breached and normalize is active, calculate equal sizes
	if normalize && maxSlices > 1 && totalSlices > maxSlices {
		// 1. Calculate how many total files are required
		numFiles := int(math.Ceil(float64(totalSlices) / float64(maxSlices)))

		// 2. Determine the uniform baseline size per file
		baseSize := totalSlices / numFiles

		// 3. Find out how many extra leftover slices need distribution
		remainder := totalSlices % numFiles

		// 4. Build our perfectly balanced file allocation blueprint
		for i := 0; i < numFiles; i++ {
			size := baseSize
			// Distribute remainder slices across the front files one by one
			if i < remainder {
				size++
			}
			partitionSizes = append(partitionSizes, size)
		}
	} else {
		// SCENARIO 3: Standard hard-cutoff clipping logic (e.g., 16, 16, 2)
		// This also implicitly handles individual slice file exports (maxSlices == 1)
		remaining := totalSlices
		for remaining > 0 {
			take := maxSlices
			if remaining < maxSlices {
				take = remaining
			}
			partitionSizes = append(partitionSizes, take)
			remaining -= take
		}
	}

	// 5. Execute structural slicing chunks using our partition allocations array
	currentIndex := 0
	for _, size := range partitionSizes {
		sliceGroup := raw.Slices[currentIndex : currentIndex+size]
		results = append(results, stitchGroup(raw, sliceGroup))
		currentIndex += size
	}

	return results, nil
}

func stitchGroup(raw *RawExtraction, sliceGroup []RexSlicePayload) SliceExtraction {
	var interleavedAudio []float32
	var cuePoints []WavCueMarker
	var currentFrameOffset uint32 = 0

	for subIndex, slice := range sliceGroup {
		// Absolute position inside THIS current chunk file resets to the local cumulative offset
		cuePoints = append(cuePoints, WavCueMarker{
			SliceID:  subIndex, // Reset locally per output file container for hardware trackers
			Position: currentFrameOffset,
			Label:    fmt.Sprintf("Slice %02d", slice.SliceIndex+1), // Retain original ReCycle index name tracking
		})

		interleavedAudio = append(interleavedAudio, slice.PCMData...)
		currentFrameOffset += uint32(slice.FrameLength)
	}

	return SliceExtraction{
		Metadata:    raw.Metadata,
		CuePoints:   cuePoints,
		Interleaved: interleavedAudio,
		TotalFrames: int(currentFrameOffset),
	}
}
