package rexengine

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

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
		var err error
		targets, err = scanDirectory(cfg.InputDir, cfg.Recursive)
		if err != nil {
			return err
		}
	} else {
		targets = cfg.InputFiles
	}

	if len(targets) == 0 {
		return nil
	}

	var mu sync.Mutex
	numWorkers := runtime.NumCPU()
	guard := make(chan struct{}, numWorkers)
	errCh := make(chan error, len(targets))

	for _, target := range targets {
		go func(t string) {
			guard <- struct{}{}
			defer func() { <-guard }()

			data, err := os.ReadFile(t)
			if err != nil {
				errCh <- fmt.Errorf("skipping unreadable target %s: %v", t, err)
				return
			}

			mu.Lock()
			err = processFileBuffer(data, t, cfg)
			mu.Unlock()

			errCh <- err
		}(target)
	}

	for range targets {
		if err := <-errCh; err != nil {
			fmt.Printf("Error: %v\n", err)
		}
	}

	return nil
}

func processFileBuffer(fileData []byte, sourcePath string, cfg PipelineConfig) error {
	sdkTempo := 0
	if cfg.Tempo > 0 {
		sdkTempo = cfg.Tempo * 1000
	}

	slices, err := RenderSlicesPreview(fileData, cfg.SampleRate, sdkTempo)
	if err != nil {
		return fmt.Errorf("slices render failed: %w", err)
	}

	if len(slices) == 0 {
		return nil
	}

	// Apply mono downmix per-slice
	if cfg.Mono && slices[0].Metadata.Channels == 2 {
		for i := range slices {
			slices[i].Interleaved = downmixStereoToMono(slices[i].Interleaved)
			slices[i].TotalFrames = len(slices[i].Interleaved)
		}
		slices[0].Metadata.Channels = 1
	}

	// Build output chunks: group slices or concatenate into single file
	var chunks []SliceExtraction
	if cfg.SliceLimit > 0 {
		chunks = groupSlices(slices, cfg.SliceLimit, cfg.NormalizeSplits)
	} else {
		chunks = buildSingleOutput(slices)
	}

	totalFiles := len(chunks)
	digitWidth := 2
	if totalFiles > 99 {
		digitWidth = len(fmt.Sprintf("%d", totalFiles))
	}
	postfixFormat := fmt.Sprintf("_%%0%dd.wav", digitWidth)

	var wg sync.WaitGroup
	errCh := make(chan error, totalFiles)

	for idx, c := range chunks {
		wg.Add(1)
		go func(idx int, c SliceExtraction) {
			defer wg.Done()
			finalPath := outputPath(sourcePath, cfg, idx, totalFiles, postfixFormat)

			outDir := filepath.Dir(finalPath)
			if outDir != "." && outDir != "" {
				_ = os.MkdirAll(outDir, 0o755)
			}

			outFile, err := os.Create(finalPath)
			if err != nil {
				errCh <- fmt.Errorf("failed creating output file %s: %w", finalPath, err)
				return
			}
			defer outFile.Close()

			if !cfg.Quiet {
				fmt.Printf("Converting: %s -> %s | Slices: %d | Channels: %d | Rate: %dHz | Tempo: %.1f BPM\n",
					filepath.Base(sourcePath), filepath.Base(finalPath), len(c.CuePoints),
					c.Metadata.Channels, c.Metadata.SampleRate, c.Metadata.OriginalTempo)
			}

			if err := EncodeWavContainer(outFile, &c, cfg.BitRate); err != nil {
				errCh <- fmt.Errorf("failed encoding container for %s: %w", finalPath, err)
			}
		}(idx, c)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		return err
	}

	return nil
}

// groupSlices groups per-slice data into output files with maxSlices per group.
func groupSlices(slices []SliceExtraction, maxSlices int, normalize bool) []SliceExtraction {
	total := len(slices)
	if total == 0 || maxSlices <= 0 {
		return buildSingleOutput(slices)
	}

	var groupSizes []int
	if normalize && maxSlices > 1 && total > maxSlices {
		numFiles := int(math.Ceil(float64(total) / float64(maxSlices)))
		baseSize := total / numFiles
		remainder := total % numFiles
		for i := 0; i < numFiles; i++ {
			size := baseSize
			if i < remainder {
				size++
			}
			groupSizes = append(groupSizes, size)
		}
	} else {
		remaining := total
		for remaining > 0 {
			take := maxSlices
			if remaining < maxSlices {
				take = remaining
			}
			groupSizes = append(groupSizes, take)
			remaining -= take
		}
	}

	ch := slices[0].Metadata.Channels
	var results []SliceExtraction
	sliceIdx := 0

	for _, size := range groupSizes {
		if sliceIdx+size > total {
			size = total - sliceIdx
		}

		totalFrames := 0
		for j := 0; j < size; j++ {
			totalFrames += slices[sliceIdx+j].TotalFrames
		}

		pcm := make([]float32, totalFrames*ch)
		offset := 0
		for j := 0; j < size; j++ {
			s := slices[sliceIdx+j]
			copy(pcm[offset:offset+len(s.Interleaved)], s.Interleaved)
			offset += len(s.Interleaved)
		}

		cues := make([]WavCueMarker, size)
		frameOffset := 0
		for j := 0; j < size; j++ {
			cues[j] = WavCueMarker{
				SliceID:  j,
				Position: uint32(frameOffset),
				Label:    fmt.Sprintf("Slice %02d", sliceIdx+j+1),
			}
			frameOffset += slices[sliceIdx+j].TotalFrames
		}

		results = append(results, SliceExtraction{
			Metadata:    slices[0].Metadata,
			CuePoints:   cues,
			Interleaved: pcm,
			TotalFrames: totalFrames,
		})

		sliceIdx += size
	}

	return results
}

// buildSingleOutput concatenates all slices into a single SliceExtraction.
func buildSingleOutput(slices []SliceExtraction) []SliceExtraction {
	if len(slices) == 0 {
		return nil
	}
	if len(slices) == 1 {
		return slices
	}

	ch := slices[0].Metadata.Channels
	totalFrames := 0
	for _, s := range slices {
		totalFrames += s.TotalFrames
	}

	pcm := make([]float32, totalFrames*ch)
	cues := make([]WavCueMarker, len(slices))
	frameOffset := 0

	for i, s := range slices {
		copy(pcm[frameOffset*ch:], s.Interleaved)
		cues[i] = WavCueMarker{
			SliceID:  i,
			Position: uint32(frameOffset),
			Label:    fmt.Sprintf("Slice %02d", i+1),
		}
		frameOffset += s.TotalFrames
	}

	return []SliceExtraction{
		{
			Metadata:    slices[0].Metadata,
			CuePoints:   cues,
			Interleaved: pcm,
			TotalFrames: totalFrames,
		},
	}
}

func outputPath(sourcePath string, cfg PipelineConfig, idx, totalFiles int, postfixFormat string) string {
	if sourcePath == "stdin" {
		outName := "output"
		if cfg.OutputFile != "" {
			outName = strings.TrimSuffix(cfg.OutputFile, ".wav")
		}
		if totalFiles > 1 {
			return fmt.Sprintf(outName+postfixFormat, idx+1)
		}
		return outName + ".wav"
	}

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
				return filepath.Join(cfg.OutputDir, relDir, filepath.Base(nameWithExt))
			}
			return filepath.Join(cfg.OutputDir, filepath.Base(nameWithExt))
		}
		return filepath.Join(cfg.OutputDir, filepath.Base(nameWithExt))
	}

	return nameWithExt
}

func downmixStereoToMono(stereoData []float32) []float32 {
	mono := make([]float32, len(stereoData)/2)
	for i := 0; i < len(mono); i++ {
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
