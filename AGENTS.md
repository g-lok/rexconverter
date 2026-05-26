# rexconverter — Contributor Guide

Go + Zig hybrid CLI tool that converts Reason Studios ReCycle files (.rex, .rx2, .rcy)
into sliced WAV files with RIFF cue markers for Dirtywave M8 and DAWs.

## Architecture

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
| **Zig** | Main executable. Calls REX SDK via `@cImport`. Exports functions for Go to call. | `internal/rexengine/extractor.zig` |
| **Go** | CLI (cobra), file I/O, WAV encoding, cue marker calculation. Compiled as c-archive. | `main.go`, `cmd/root.go`, `internal/rexengine/` |
| **REX SDK** | Proprietary C library from Reason Studios for reading/rendering REX files. | `internal/rexengine/REX.h`, `internal/rexengine/libs/macos/` |

### Data Flow

```
REX file bytes
  → Go reads file → passes bytes to Zig via CGo
    → Zig calls REXCreate → REXStartPreview → REXRenderPreviewBatch → REXStopPreview
    → Zig returns interleaved float32 PCM + PPQ positions
  → Go converts to cue positions
  → Go optionally downmixes/splits at cue boundaries
  → Go encodes WAV: fmt + data + cue (no go-audio/wav)
```

## Code Layout

```
├── main.go                  # C-archive entry, exports GoMainEntry()
├── cmd/root.go              # Cobra CLI flags + validation
├── internal/rexengine/
│   ├── bridge.go            # CGo bridge: calls Zig exported functions
│   ├── encoder.go           # Manual WAV encoder (no external libs)
│   ├── extractor.zig        # REX SDK interface (Zig)
│   ├── processor.go         # Legacy slice partitioning
│   ├── runner.go            # Pipeline orchestrator
│   ├── types.go             # Go data types
│   ├── REX.h                # REX SDK C header (patched for MinGW)
│   ├── rex/REX.c            # Windows DLL loader (outside CGo path)
│   └── libs/macos/          # macOS REX Shared Library.framework
├── build.zig                # Build coordinator
└── tests/
    ├── integration_test.go  # 30 integration tests
    ├── encoder_test.go      # WAV unit tests
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


## Adding a New CLI Flag

1. Add field to `PipelineConfig` in `internal/rexengine/bridge.go`
2. Add flag + var in `cmd/root.go` `init()` function
3. Wire flag → PipelineConfig in `RunE`
4. Use `pipelineConfig.Field` in `runner.go`
5. Add test in `tests/integration_test.go`

## SDK Notes

- The REX SDK is **not thread-safe** (except `REXRenderPreviewBatch`)
- `REX.h` is patched at line 84: `#elif defined(__GNUC__)` (was `__GNUC__ && REX_MAC`) for MinGW support
- Output WAV uses `fmt → data → cue` chunk order, `dwPosition = dwSampleOffset` (sample offset, not byte offset), `dwChunkStart = 0`
- The `@cImport` in `extractor.zig` is deprecated in Zig 0.16.0 but still compiles

## REX SDK License

This project links against the Reason Studios REX SDK, which has specific license terms:

- **Royalty-free** for study, amendment, and use
- **Cannot be used** with copyleft-licensed open source software
- The SDK license and copyright notice must be distributed with all copies

See `REX_SDK_LICENSE.txt` and `NOTICE.md` for full details.
