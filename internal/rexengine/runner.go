package rexengine

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// runPipeline acts as the central router orchestrating the data flow
func runPipeline(cfg PipelineConfig) error {
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, os.Stdin); err != nil {
			return fmt.Errorf("failed reading stream from stdin buffer: %w", err)
		}
		return processFileBuffer(buf.Bytes(), "stdin", cfg)
	}

	var targets []string
	if cfg.InputDir != "" {
		discovered, err := scanDirectory(cfg.InputDir, cfg.Recursive)
		if err != nil {
			return err
		}
		targets = discovered
	} else {
		targets = cfg.InputFiles
	}

	for _, target := range targets {
		data, err := os.ReadFile(target)
		if err != nil {
			fmt.Printf("Warning: Skipping unreadable target %s: %v\n", target, err)
			continue
		}
		if err := processFileBuffer(data, target, cfg); err != nil {
			fmt.Printf("Error processing %s: %v\n", target, err)
		}
	}

	return nil
}

func processFileBuffer(fileData []byte, sourcePath string, cfg PipelineConfig) error {
	// Convert CLI BPM to SDK format (BPM * 1000)
	sdkTempo := 0
	if cfg.Tempo > 0 {
		sdkTempo = cfg.Tempo * 1000
	}

	chunk, err := RenderLoopPreview(fileData, cfg.SampleRate, sdkTempo)
	if err != nil {
		return fmt.Errorf("loop render failed: %w", err)
	}

	if cfg.Mono && chunk.Metadata.Channels == 2 {
		chunk.Interleaved = downmixStereoToMono(chunk.Interleaved)
		chunk.TotalFrames = len(chunk.Interleaved)
		chunk.Metadata.Channels = 1
	}

	var chunks []SliceExtraction
	if cfg.SliceLimit > 0 {
		chunks = splitLoopAtCues(chunk, cfg.SliceLimit, cfg.NormalizeSplits)
	} else {
		chunks = []SliceExtraction{*chunk}
	}

	totalFiles := len(chunks)
	digitWidth := 2
	if totalFiles > 99 {
		digitWidth = len(fmt.Sprintf("%d", totalFiles))
	}
	postfixFormat := fmt.Sprintf("_%%0%dd.wav", digitWidth)

	for idx, c := range chunks {
		var finalPath string

		if sourcePath == "stdin" {
			outName := "output"
			if cfg.OutputFile != "" {
				outName = strings.TrimSuffix(cfg.OutputFile, ".wav")
			}
			if totalFiles > 1 {
				finalPath = fmt.Sprintf(outName+postfixFormat, idx+1)
			} else {
				finalPath = outName + ".wav"
			}
		} else {
			baseName := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
			if cfg.OutputFile != "" {
				baseName = strings.TrimSuffix(cfg.OutputFile, ".wav")
			}

			var nameWithExt string
			if totalFiles > 1 {
				nameWithExt = fmt.Sprintf(baseName+postfixFormat, idx+1)
			} else {
				nameWithExt = baseName + ".wav"
			}

			if cfg.OutputDir != "" {
				if cfg.Preserve && cfg.InputDir != "" {
					relDir, err := filepath.Rel(cfg.InputDir, filepath.Dir(sourcePath))
					if err == nil && relDir != "." {
						finalPath = filepath.Join(cfg.OutputDir, relDir, filepath.Base(nameWithExt))
					} else {
						finalPath = filepath.Join(cfg.OutputDir, filepath.Base(nameWithExt))
					}
				} else {
					finalPath = filepath.Join(cfg.OutputDir, filepath.Base(nameWithExt))
				}
			} else {
				finalPath = nameWithExt
			}
		}

		outDir := filepath.Dir(finalPath)
		if outDir != "." && outDir != "" {
			_ = os.MkdirAll(outDir, 0o755)
		}

		outFile, err := os.Create(finalPath)
		if err != nil {
			return fmt.Errorf("failed creating output file %s: %w", finalPath, err)
		}

		if !cfg.Quiet {
			fmt.Printf("Converting: %s -> %s | Slices: %d | Channels: %d | Rate: %dHz | Tempo: %.1f BPM\n",
				filepath.Base(sourcePath), filepath.Base(finalPath), len(c.CuePoints),
				c.Metadata.Channels, c.Metadata.SampleRate, c.Metadata.OriginalTempo)
		}

		err = EncodeWavContainer(outFile, &c, cfg.BitRate)
		outFile.Close()
		if err != nil {
			return fmt.Errorf("failed encoding container for %s: %w", finalPath, err)
		}
	}

	return nil
}

// splitLoopAtCues partitions a loop-rendered SliceExtraction at cue marker
// boundaries. Each output file gets a contiguous PCM range covering its group of
// slices, with cue positions rebased to the start of that range.
func splitLoopAtCues(src *SliceExtraction, maxSlices int, normalize bool) []SliceExtraction {
	totalSlices := len(src.CuePoints)
	if totalSlices == 0 || maxSlices <= 0 {
		return []SliceExtraction{*src}
	}

	// Build partition sizes
	var partitionSizes []int
	if normalize && maxSlices > 1 && totalSlices > maxSlices {
		numFiles := int(math.Ceil(float64(totalSlices) / float64(maxSlices)))
		baseSize := totalSlices / numFiles
		remainder := totalSlices % numFiles
		for i := 0; i < numFiles; i++ {
			size := baseSize
			if i < remainder {
				size++
			}
			partitionSizes = append(partitionSizes, size)
		}
	} else {
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

	ch := src.Metadata.Channels
	fullPCM := src.Interleaved
	var results []SliceExtraction
	cueIdx := 0

	for _, size := range partitionSizes {
		startCue := cueIdx
		endCue := cueIdx + size
		if endCue > totalSlices {
			endCue = totalSlices
		}

		startFrame := int(src.CuePoints[startCue].Position)
		var endFrame int
		if endCue < totalSlices {
			endFrame = int(src.CuePoints[endCue].Position)
		} else {
			endFrame = src.TotalFrames
		}
		if endFrame > src.TotalFrames {
			endFrame = src.TotalFrames
		}

		numFrames := endFrame - startFrame
		pcmRange := make([]float32, numFrames*ch)
		copy(pcmRange, fullPCM[startFrame*ch:endFrame*ch])

		cues := make([]WavCueMarker, size)
		for i := 0; i < size; i++ {
			cp := src.CuePoints[startCue+i]
			cues[i] = WavCueMarker{
				SliceID:  i,
				Position: cp.Position - uint32(startFrame),
				Label:    cp.Label,
			}
		}

		results = append(results, SliceExtraction{
			Metadata:    src.Metadata,
			CuePoints:   cues,
			Interleaved: pcmRange,
			TotalFrames: numFrames,
		})

		cueIdx = endCue
	}

	return results
}

func downmixStereoToMono(stereoData []float32) []float32 {
	mono := make([]float32, len(stereoData)/2)
	for i := 0; i < len(mono); i++ {
		// Average left and right interleaved samples safely
		mono[i] = (stereoData[i*2] + stereoData[i*2+1]) / 2.0
	}
	return mono
}

func scanDirectory(dir string, recursive bool) ([]string, error) {
	var matches []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !recursive && info.IsDir() && path != dir {
			return filepath.SkipDir
		}
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".rx2" || ext == ".rex" {
				matches = append(matches, path)
			}
		}
		return nil
	})
	return matches, err
}
