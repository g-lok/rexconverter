# rexconverter — Contributor Guide

Go + Zig hybrid CLI tool that converts ReCycle files (.rex, .rx2, .rcy) into beat machine
native formats: WAV, PTI (Polyend Tracker), OT (Elektron Octatrack), OP-1 AIFF, XY preset
(OP-XY), Elektron multi-sample text, and DT2 preset (Digitakt II).

## Architecture

### Phase 1 — REX → multi-format output (current)

```
mise run build  →  zig build (orchestrates Go → Zig link)
     └── Go compiled as static C archive (buildmode=c-archive)
     └── Zig compiles extractor.zig (main entrypoint, calls REX SDK C API)
     └── Go archive statically linked into Zig executable
     └── install_name_tool patches framework rpath at end
```

### Language Roles

| Layer | Role | Key Files |
|-------|------|-----------|
| **Zig** | Main executable. Calls REX SDK via `b.addTranslateC()` (build-time C→Zig translation). Exports functions for Go to call. | `internal/rexengine/extractor.zig` |
| **Go** | CLI (cobra), file I/O, format encoding, cue marker calculation. Compiled as c-archive. | `main.go`, `cmd/root.go`, `internal/rexengine/` |
| **REX SDK** | Proprietary C library from Reason Studios for reading/rendering REX files. Read-only — no write API. | `internal/rexengine/REX.h`, `internal/rexengine/libs/macos/` |

### Data Flow

```
REX file bytes
  → Go reads file → passes bytes to Zig via CGo
    → Zig calls REXCreate → REXStartPreview → REXRenderPreviewBatch → REXStopPreview
    → Zig returns interleaved float32 PCM + PPQ positions
  → Go converts to cue positions
  → Go optionally downmixes/splits at cue boundaries
   → Go optionally resamples (linear interpolation), converts bit depth,
      downmixes to mono (5 strategies: sum/left/right/difference/dual-detect)
   → Go routes to selected encoder:
        wav  → EncodeWavContainer (fmt + data + cue)
        pti  → EncodePTI (392-byte header + 44.1k/16-bit mono PCM)
        ot   → EncodeWavContainer + EncodeOT (0x340-byte sidecar)
        aif-op1 → EncodeOP1AIF (AIFF + APPL "op-1" JSON chunk)
        xy   → EncodeXYPreset (ZIP with patch.json + per-slice WAVs)
        el   → EncodeWavContainer + EncodeEL (text sidecar)
        d2pst → EncodeDT2Preset (ZIP: manifest.json + 48k WAV + preset binary)
```

### Phase 2 — General-purpose cross-converter (planned)

```
Any audio input (WAV, AIFF, MP3, FLAC, OGG)
  → Input reader (direct parse for WAV/AIFF, pure Go decoders for MP3/FLAC/OGG,
     optional ffmpeg subprocess for unsupported formats)
  → Normalized SliceExtraction (same intermediate format as Phase 1)
  → Route to selected output encoder (same as Phase 1, unchanged)
  → Optional: manual grid / explicit list slicing strategies
```

## Code Layout

```
├── main.go                  # C-archive entry, exports GoMainEntry()
├── cmd/root.go              # Cobra CLI flags + validation
├── internal/rexengine/
│   ├── bridge.go            # CGo bridge: calls Zig exported functions
│   ├── encoder.go           # Manual WAV encoder (no external libs)
│   ├── encoder_pti.go       # PTI format: 392-byte header + 44.1k/16-bit mono PCM
│   ├── encoder_ot.go        # OT sidecar: 0x340-byte big-endian binary w/ checksum
│   ├── encoder_op1.go       # OP-1 AIFF: FORM/AIFF/COMM/APPL(op-1 JSON)/SSND
│   ├── encoder_xy.go        # XY preset ZIP: patch.json + per-slice WAVs
│   ├── encoder_el.go        # EL text sidecar: key-zone mapping format
│   ├── encoder_d2pst.go     # DT2 preset ZIP: manifest.json + WAV + TLV preset bin
│   ├── resample.go          # ForceSampleRate, DownmixToMono (5 strategies), format force-helpers
│   ├── extractor.zig        # REX SDK interface via translate-c (Zig)
│   ├── runner.go            # Pipeline orchestrator
│   ├── types.go             # Go data types
│   ├── REX.h                # REX SDK C header (patched for MinGW)
│   ├── rex/REX.c            # Windows DLL loader (outside CGo path)
│   └── libs/macos/          # macOS REX Shared Library.framework
├── build.zig                # Build coordinator
└── tests/
    ├── integration_test.go  # 30 integration tests
    ├── encoder_test.go      # WAV unit tests
    ├── encoder_format_test.go # Multi-format encoder tests (subprocess)
    ├── processor_test.go    # Slice partition tests
    └── testdata/            # Test REX files
```

## Building

### Prerequisites

- **Go** 1.26+ (managed via [mise](https://mise.jdx.dev) or manually)
- **Zig** 0.16.0+ (managed via mise or manually)
- **REX SDK v1.9.2** — download from Reason Studios:
  - macOS: `REXSDK_Mac_1.9.2.zip` → place REX Shared Library.framework in `internal/rexengine/libs/macos/`
  - Windows: `REXSDK_Win_1.9.2.zip` → place `REX Shared Library.dll` + `.lib` alongside the built binary

### Commands

```bash
# macOS native build
mise run build
# Output: build/rexconverter

# macOS universal + Windows x86_64 release archives
mise run build-releases
# Output: build/releases/ (tar.gz + zip)

# Manual build
zig build -Dtarget=x86_64-macos -Doptimize=ReleaseSafe
```

### Windows Cross-Compile (from macOS)

```bash
CC="zig cc -target x86_64-windows-gnu" GOOS=windows GOARCH=amd64 CGO_ENABLED=1 \
  go build -buildmode=c-archive -tags netgo -o build/go_engine_windows.a main.go
cd build && ar x go_engine_windows.a && zig ar rcs go_engine_windows.a *.o && rm -f *.o && cd ..
zig build -Dtarget=x86_64-windows-gnu -Doptimize=ReleaseSafe \
  "-Dgo-archive=build/go_engine_windows.a" "--prefix" build/zig-out-win
```

Note: The `ar x` + `zig ar rcs` dance fixes BSD-format archives (created by macOS `ar`) for
lld-link compatibility on Windows targets.

## Testing

```bash
go test ./tests/...
```

Tests run the built binary as a subprocess (`os/exec`), so build first.

Key test categories:

| Test | What it validates |
|------|------------------|
| `TestIntegration_StereoDefaultOutput` | Full pipeline: stereo REX → WAV output |
| `TestIntegration_SliceLimit` | `--slice-limit` splitting at cue boundaries |
| `TestIntegration_NormalizeSplits` | `--normalize-splits` balanced partitioning |
| `TestIntegration_CleanWAVStructure` | Only fmt/data/cue chunks present (no LIST/INFO) |
| `TestIntegration_CueMarkersCorrect` | Every cue point field validated |
| `TestLoopRenderMatch_Stereo` | PCM matches SDK PreviewRender (0/176k samples off by >2) |
| `TestFormatPTI` | PTI header byte validation + PCM length |
| `TestFormatOT` | OT sidecar checksum + structure + 64-slice table |
| `TestFormatOP1` | OP-1 AIFF form type + APPL chunk JSON |
| `TestFormatXY` | XY ZIP structure + patch.json regions |
| `TestFormatEL` | EL text sidecar key-zone sections |
| `TestFormat_FLAG_DT2` | DT2 ZIP manifest + WAV + TLV preset binary |
| `TestNoSlicesFlag` | `--no-slices` produces single monolithic WAV |
| `TestMonoModeFlags` | `--mono-mode` strategies produce correct channels |


## Adding a New CLI Flag

1. Add field to `PipelineConfig` in `internal/rexengine/bridge.go`
2. Add flag + var in `cmd/root.go` `init()` function
3. Wire flag → PipelineConfig in `RunE`
4. Use `pipelineConfig.Field` in `runner.go`
5. Add test in `tests/integration_test.go`

## Adding a New Output Format

1. Create `internal/rexengine/encoder_<format>.go` with `Encode<Format>(...)` function
2. Create `internal/rexengine/resample.go` helpers if format forces specific sample rate/channels
3. Add format case to the switch in `runner.go` `processFileBuffer()`
4. Add format to `outputExt()` helper for extension mapping
5. Add format constant/flag to `PipelineConfig` + `cmd/root.go`
6. Add `--format` flag if not already present
7. Add tests in `tests/encoder_format_test.go` (or `tests/encoder_<format>_test.go`)

## Adding a New Input Format (Phase 2)

1. Create `internal/rexengine/reader_<format>.go` implementing `InputReader` interface
2. Return `[]SliceExtraction` from PCM + positional data
3. Input readers should be stateless (no shared data) for thread safety
4. For WAV/AIFF: hand-rolled RIFF/FORM parsing (follow existing encoder patterns)
5. For MP3/FLAC/OGG: wrap pure Go library (`go-mp3`, `go-flac`, `oggvorbis`) — all permissive license, no CGo
6. For unsupported formats: optional ffmpeg subprocess fallback, runtime-detected (not a hard dependency)

## SDK Notes

- The REX SDK is **not thread-safe** (except `REXRenderPreviewBatch`)
- `REX.h` is patched at line 84: `#elif defined(__GNUC__)` (was `__GNUC__ && REX_MAC`) for MinGW support
- Output WAV uses `fmt → data → cue` chunk order, `dwPosition = dwSampleOffset` (sample offset, not byte offset), `dwChunkStart = 0`
- `REX.h` is translated via `b.addTranslateC()` in `build.zig` with target-specific `defineCMacro` calls for `REX_MAC`/`REX_WINDOWS`
- `REXGetInfoFromBuffer()` is available but unused — enables fast metadata scanning without `REXCreate`
- `REXGetCreatorInfo()` is never called — creator metadata not extracted

## REX SDK License

This project links against the Reason Studios REX SDK, which has specific license terms:

- **Royalty-free** for study, amendment, and use
- **Cannot be used** with copyleft-licensed open source software
- The SDK license and copyright notice must be distributed with all copies
- The SDK is **read-only** — there is no API to produce REX/RX2 files
- **Phase 2** input readers (pure Go decoders, optional ffmpeg subprocess) do NOT link against the REX SDK and have no license restrictions

See `REX_SDK_LICENSE.txt` and `NOTICE.md` for full details.
