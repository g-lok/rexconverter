package cmd

import (
	"fmt"
	"os"

	"github.com/g-lok/rexconverter/internal/rexengine"
	"github.com/spf13/cobra"
)

// Set at build time via -ldflags -X
var version = "dev"

var (
	inputFiles      []string
	inputDir        string
	outputFile      string
	outputDir       string
	recursive       bool
	bitRate         int
	sampleRate      int
	mono            bool
	sliceLimit      int
	normalizeSplits bool
	tempo           int
	quiet           bool
	preserve        bool
	verbose         bool
)

var rootCmd = &cobra.Command{
	Use:     "rexconverter [INPUT_FILES...]",
	Short:   "A cross-platform CLI multitool for converting ReCycle files to sliced WAV format",
	Version: version,
	Long: `A high-performance batch utility to convert Reason Studios ReCycle (.rex, .rx2) files 
into multi-slice WAV containers embedded with native RIFF cue markers. Supports 
in-memory streaming, concurrency optimization, and sampler hardware formatting.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		hasFlagInputs := len(inputFiles) > 0
		hasPositionalInputs := len(args) > 0
		hasDirInput := inputDir != ""

		stat, _ := os.Stdin.Stat()
		hasStdin := (stat.Mode() & os.ModeCharDevice) == 0

		if hasDirInput && (hasFlagInputs || hasPositionalInputs || hasStdin) {
			return fmt.Errorf("error: cannot mix --input-dir with explicit file targets or stdin")
		}

		if !hasDirInput && !hasFlagInputs && !hasPositionalInputs && !hasStdin {
			return fmt.Errorf("error: missing input targets. Provide positional args, --input-file, or --input-dir")
		}

		if outputFile != "" && outputDir != "" {
			return fmt.Errorf("error: cannot combine --output-file with --output-dir")
		}

		if outputFile != "" {
			totalInputs := len(inputFiles) + len(args)
			if totalInputs > 1 || hasDirInput {
				return fmt.Errorf("error: --output-file cannot be used with multiple input files; use --output-dir (-e) instead")
			}
		}

		if recursive && !hasDirInput {
			return fmt.Errorf("error: --recursive requires --input-dir")
		}

		if preserve && !hasDirInput {
			return fmt.Errorf("error: --preserve requires --input-dir")
		}

		if normalizeSplits && sliceLimit <= 0 {
			return fmt.Errorf("error: --normalize-splits requires --slice-limit")
		}

		if bitRate != 0 && bitRate != 8 && bitRate != 16 && bitRate != 24 {
			return fmt.Errorf("error: unsupported bit-rate %d. Supported: 8, 16, or 24", bitRate)
		}

		if hasPositionalInputs {
			inputFiles = append(inputFiles, args...)
		}

		if err := rexengine.InitEngine(verbose); err != nil {
			return fmt.Errorf("failed to initialize REX SDK: %w", err)
		}
		defer rexengine.CloseEngine()

		pipelineConfig := rexengine.PipelineConfig{
			InputFiles:      inputFiles,
			InputDir:        inputDir,
			OutputFile:      outputFile,
			OutputDir:       outputDir,
			Recursive:       recursive,
			BitRate:         bitRate,
			SampleRate:      sampleRate,
			Mono:            mono,
			SliceLimit:      sliceLimit,
			NormalizeSplits: normalizeSplits,
			Tempo:           tempo,
			Quiet:           quiet,
			Preserve:        preserve,
			Verbose:         verbose,
		}

		return rexengine.ExecuteConversionPipeline(pipelineConfig)
	},
}

func Execute() {
	if rootCmd == nil {
		return
	}
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringSliceVarP(&inputFiles, "input-file", "i", []string{}, "Target ReCycle input file(s)")
	rootCmd.Flags().StringVarP(&inputDir, "input-dir", "d", "", "Scan directory for .rex/.rx2 files")
	rootCmd.Flags().StringVarP(&outputFile, "output-file", "o", "", "Output WAV path (single input only)")
	rootCmd.Flags().StringVarP(&outputDir, "output-dir", "e", "", "Output directory for batch conversions")
	rootCmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Recurse subdirectories (requires --input-dir)")
	rootCmd.Flags().BoolVarP(&preserve, "preserve", "p", false, "Preserve directory structure in output (requires --input-dir)")
	rootCmd.Flags().IntVarP(&bitRate, "bit-rate", "b", 0, "Bit depth: 8, 16, or 24")
	rootCmd.Flags().IntVarP(&sampleRate, "sample-rate", "s", 0, "Output sample rate in Hz")
	rootCmd.Flags().BoolVarP(&mono, "mono", "m", false, "Downmix to mono")
	rootCmd.Flags().IntVarP(&tempo, "tempo", "t", 0, "Override loop tempo in BPM (default: original)")
	rootCmd.Flags().IntVarP(&sliceLimit, "slice-limit", "l", 0, "Max slices per output file")
	rootCmd.Flags().BoolVarP(&normalizeSplits, "normalize-splits", "n", false, "Balance slices evenly across splits")
	rootCmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Suppress progress output")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Debug output (Zig struct diagnostics)")
}
