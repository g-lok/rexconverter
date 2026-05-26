package rexengine

/*
#cgo CFLAGS: -I.
#include <stdlib.h>

// Matches the exact binary layout established in extractor.zig
typedef struct {
    int channels;
    int sample_rate;
    double tempo;
    double original_tempo;
    int time_sign_nom;
    int time_sign_denom;
    int bit_depth;
    int ppq_length;
} ZigMetadata;

typedef struct {
    int ppq_pos;
} ZigLoopSliceInfo;

typedef struct {
    ZigMetadata metadata;
    int tempo;
    int frame_length;
    int slice_count;
    ZigLoopSliceInfo* slice_info;
    float* pcm_data;
} ZigLoopRenderResult;

typedef struct {
    int slice_index;
    int ppq_pos;
    int frame_length;
    float* pcm_data; // Flat, interleaved PCM array from Zig
} ZigSlicePayload;

typedef struct {
    ZigMetadata metadata;
    int slice_count;
    ZigSlicePayload* slices;
} ZigRawExtraction;

// Zig engine lifecycle + extraction + diagnostics
int Zig_InitEngine(void);
void Zig_CloseEngine(void);
void Zig_Diagnostic(void);
void* Zig_ExtractRawData(const unsigned char* file_bytes, int byte_len, int target_sample_rate);
void Zig_FreeRawData(void* package_ptr);

// Loop preview render
void* Zig_RenderLoopPreview(const unsigned char* file_bytes, int byte_len, int target_sample_rate, int tempo_bpm);
void Zig_FreeLoopRenderResult(void* result);
*/
import "C"

import (
	"errors"
	"fmt"
	"reflect"
	"unsafe"
)

// PipelineConfig carries all CLI flags through the conversion pipeline.
type PipelineConfig struct {
	InputDir        string
	InputFiles      []string
	OutputDir       string
	OutputFile      string
	SampleRate      int
	BitRate         int
	Mono            bool
	Recursive       bool
	SliceLimit      int
	NormalizeSplits bool
	Tempo           int   // BPM override (0 = use original)
	Quiet           bool  // Suppress "Converting:" progress lines
	Preserve        bool  // Mirror input directory structure in output
	Verbose         bool
}

// InitEngine initializes the REX SDK framework. Called once at startup.
func InitEngine(verbose bool) error {
	if rc := C.Zig_InitEngine(); rc != 0 {
		return fmt.Errorf("Zig_InitEngine failed with code %d", rc)
	}
	if verbose {
		C.Zig_Diagnostic()
	}
	return nil
}

// CloseEngine shuts down the REX SDK framework. Called once at shutdown.
func CloseEngine() error {
	C.Zig_CloseEngine()
	return nil
}

// FIX: Expose ExecuteConversionPipeline as a direct alias pass to runner.go's pipeline orchestrator
func ExecuteConversionPipeline(cfg PipelineConfig) error {
	return runPipeline(cfg)
}

// ExtractRawData matches your runner.go caller naming expectation exactly
func ExtractRawData(fileData []byte, targetSampleRate int) (*RawExtraction, error) {
	if len(fileData) == 0 {
		return nil, errors.New("empty file data buffer sequence target")
	}

	// 1. Allocate the incoming file bytes onto the unmanaged C heap out of GC range
	cBytes := C.CBytes(fileData)
	defer C.free(cBytes)

	// 2. Call our underlying compiled Zig artifact wrapper pass
	opaquePtr := C.Zig_ExtractRawData((*C.uchar)(cBytes), C.int(len(fileData)), C.int(targetSampleRate))
	if opaquePtr == nil {
		return nil, errors.New("Zig extraction engine failed to parse REX container or headers")
	}
	defer C.Zig_FreeRawData(opaquePtr)

	// Cast the raw pointer to our C structure layout definition mapping block
	cPkg := (*C.ZigRawExtraction)(opaquePtr)

	// 3. Map global metadata directly into the existing model types.go expected layout type
	goMeta := RexMetadata{
		Channels:      int(cPkg.metadata.channels),
		SampleRate:    int(cPkg.metadata.sample_rate),
		Tempo:         float64(cPkg.metadata.tempo),
		OriginalTempo: float64(cPkg.metadata.original_tempo),
		TimeSignNom:   int(cPkg.metadata.time_sign_nom),
		TimeSignDenom: int(cPkg.metadata.time_sign_denom),
		BitDepth:      int(cPkg.metadata.bit_depth),
		PPQLength:     int(cPkg.metadata.ppq_length),
	}

	sliceCount := int(cPkg.slice_count)
	goSlices := make([]RexSlicePayload, sliceCount)

	// 4. Map the C array slice block out into a standard Go slice structure
	var cSlicesSlice []C.ZigSlicePayload
	sliceHeader := (*reflect.SliceHeader)(unsafe.Pointer(&cSlicesSlice))
	sliceHeader.Data = uintptr(unsafe.Pointer(cPkg.slices))
	sliceHeader.Len = sliceCount
	sliceHeader.Cap = sliceCount

	// 5. Loop over individual payloads and process slice sample arrays
	for i := 0; i < sliceCount; i++ {
		cSlice := cSlicesSlice[i]
		frameLen := int(cSlice.frame_length)

		// Calculate total floats by multiplying frames by active audio channels
		totalSamples := frameLen * goMeta.Channels

		var goPCM []float32
		if totalSamples > 0 && cSlice.pcm_data != nil {
			// Cast flat interleaved float* pointers into safe Go slices
			var cPCMSlice []C.float
			pcmHeader := (*reflect.SliceHeader)(unsafe.Pointer(&cPCMSlice))
			pcmHeader.Data = uintptr(unsafe.Pointer(cSlice.pcm_data))
			pcmHeader.Len = totalSamples
			pcmHeader.Cap = totalSamples

			// Clone data blocks cleanly to protect values against memory eviction faults
			goPCM = make([]float32, totalSamples)
			for s := 0; s < totalSamples; s++ {
				goPCM[s] = float32(cPCMSlice[s])
			}
		}

		goSlices[i] = RexSlicePayload{
			SliceIndex:  int(cSlice.slice_index),
			PPQPos:      int(cSlice.ppq_pos),
			FrameLength: frameLen,
			PCMData:     goPCM,
		}
	}

	// Return the parsed payload aligned precisely with the struct models in types.go
	return &RawExtraction{
		Metadata: goMeta,
		Slices:   goSlices,
	}, nil
}

// RenderLoopPreview renders the full REX loop at given tempo using SDK preview API.
// tempo: BPM * 1000 (e.g. 120000 for 120 BPM). Pass 0 to use original tempo.
func RenderLoopPreview(fileData []byte, targetSampleRate, tempo int) (*SliceExtraction, error) {
	if len(fileData) == 0 {
		return nil, errors.New("empty file data buffer")
	}

	cBytes := C.CBytes(fileData)
	defer C.free(cBytes)

	opaquePtr := C.Zig_RenderLoopPreview((*C.uchar)(cBytes), C.int(len(fileData)), C.int(targetSampleRate), C.int(tempo))
	if opaquePtr == nil {
		return nil, errors.New("Zig loop render failed")
	}
	defer C.Zig_FreeLoopRenderResult(opaquePtr)

	cRes := (*C.ZigLoopRenderResult)(opaquePtr)

	meta := RexMetadata{
		Channels:      int(cRes.metadata.channels),
		SampleRate:    int(cRes.metadata.sample_rate),
		Tempo:         float64(cRes.metadata.tempo),
		OriginalTempo: float64(cRes.metadata.original_tempo),
		TimeSignNom:   int(cRes.metadata.time_sign_nom),
		TimeSignDenom: int(cRes.metadata.time_sign_denom),
		BitDepth:      int(cRes.metadata.bit_depth),
		PPQLength:     int(cRes.metadata.ppq_length),
	}

	loopFrames := int(cRes.frame_length)
	sliceCount := int(cRes.slice_count)
	totalSamples := loopFrames * meta.Channels
	actualTempo := int(cRes.tempo)

	// Copy loop PCM
	interleaved := make([]float32, totalSamples)
	if totalSamples > 0 && cRes.pcm_data != nil {
		var cPCM []C.float
		pcmHeader := (*reflect.SliceHeader)(unsafe.Pointer(&cPCM))
		pcmHeader.Data = uintptr(unsafe.Pointer(cRes.pcm_data))
		pcmHeader.Len = totalSamples
		pcmHeader.Cap = totalSamples
		for s := 0; s < totalSamples; s++ {
			interleaved[s] = float32(cPCM[s])
		}
	}

	// Copy slice info (PPQ positions)
	var cSliceInfo []C.ZigLoopSliceInfo
	infoHeader := (*reflect.SliceHeader)(unsafe.Pointer(&cSliceInfo))
	infoHeader.Data = uintptr(unsafe.Pointer(cRes.slice_info))
	infoHeader.Len = sliceCount
	infoHeader.Cap = sliceCount

	// Calculate cue positions: framePos = sampleRate * 1000 * ppqPos / (tempo * 256)
	cuePoints := make([]WavCueMarker, sliceCount)
	for i := 0; i < sliceCount; i++ {
		ppqPos := int(cSliceInfo[i].ppq_pos)
		framePos := int(float64(meta.SampleRate) * 1000.0 * float64(ppqPos) / (float64(actualTempo) * 256.0))
		if framePos > loopFrames {
			framePos = loopFrames
		}
		cuePoints[i] = WavCueMarker{
			SliceID:  i,
			Position: uint32(framePos),
			Label:    fmt.Sprintf("Slice %02d", i+1),
		}
	}

	return &SliceExtraction{
		Metadata:    meta,
		CuePoints:   cuePoints,
		Interleaved: interleaved,
		TotalFrames: loopFrames,
	}, nil
}
