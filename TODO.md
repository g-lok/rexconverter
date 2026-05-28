# TODO: Post-Release Tasks

## 1. Push Clean History to `main`

### Option A: Using jujutsu (jj)

```bash
# Initialize jj repo from current state
jj git init

# Describe the first commit
jj describe -m "initial commit: convert REX/RX2 files to cued WAV for M8 and DAWs"

# Add a bookmark for main
jj bookmark move main@origin -B main

# Push with force (this is the first real commit)
jj git push --remote origin --force
```

### Option B: Using git

```bash
# Create orphan branch (no parent history)
git checkout --orphan main

# Stage all files
git add -A

# Create the initial commit
git commit -m "initial commit: convert REX/RX2 files to cued WAV for M8 and DAWs"

# Force push (replaces whatever was on main before)
git push -f origin main
```

Note: Both approaches create a clean history with a single commit. The old commit
history is not deleted from the remote immediately — run `git remote prune origin`
or GC on the remote to reclaim space.

---

## 2. Create GitHub Release

```bash
# Tag the release
VERSION="v0.1.0"
git tag -a "$VERSION" -m "rexconverter $VERSION"

# Push tag
git push origin "$VERSION"
```

Then on GitHub, create a release from the tag and upload:
- `rexconverter-<version>-macos.tar.gz` (built with `mise run build-releases`)
- `rexconverter-<version>-windows.zip` (built with `mise run build-releases`)

---

## 3. Version Bump Checklist

- [ ] Tag new version: `git tag -a v0.3.0-beta -m "rexconverter v0.3.0-beta"`
- [ ] Run `mise run build-releases`
- [ ] Upload release artifacts to GitHub Releases
- [ ] Run `shasum -a 256 build/releases/*.tar.gz build/releases/*.zip`
- [ ] Update Homebrew formula SHA256
- [ ] Update Scoop manifest SHA256
- [ ] Push both tap/bucket repos

---

## 4. Phase 2 — General-Purpose Cross-Converter

### 4.1 WAV Input Reader (`reader_wav.go`)

Direct RIFF parsing — WAV fmt chunk → `SliceExtraction`. No external deps.

### 4.2 AIFF Input Reader (`reader_aiff.go`)

Direct FORM/COMM/SSND parsing. Need extended sample rate decoding.

### 4.3 Go-Lib Readers (`reader_mp3.go`, `reader_flac.go`, `reader_ogg.go`)

Pure Go libraries, no CGo, no linking, permissive licenses:

| Format | Go Library | License | Reader File |
|--------|-----------|---------|-------------|
| MP3 | `github.com/hajimehoshi/go-mp3` | Apache 2.0 | `reader_mp3.go` |
| FLAC | `github.com/mewkiz/flac` | MIT | `reader_flac.go` |
| OGG/Vorbis | `github.com/jfreymuth/oggvorbis` | MIT | `reader_ogg.go` |

Each wraps the library's decoder to produce `SliceExtraction` (float32 PCM + metadata).

### 4.4 Optional ffmpeg Subprocess Fallback (`reader_ffmpeg.go`)

**ffmpeg is NOT a hard dependency.** Used only for unsupported formats not covered by pure Go libs.
Detected at runtime — if ffmpeg missing, produces helpful error (brew/scoop/apt instructions):

```go
cmd := exec.Command("ffmpeg", "-i", "pipe:0", "-f", "f32le", "-ar", "44100", "-ac", "2", "pipe:1")
cmd.Stdin = bytes.NewReader(fileBytes)
stdout, _ := cmd.StdoutPipe()
// read stdout → SliceExtraction
```

Thread-safe: each call is an independent OS process. No mutex needed.

### 4.5 InputReader Interface (`reader.go`)

```go
type InputReader interface {
    Probe(data []byte) (*AudioMetadata, error)
    Read(data []byte, sampleRate int) ([]SliceExtraction, error)
    SupportedExtensions() []string
}
```

### 4.6 Auto-Slicing (`slicer.go`)

For unstructured audio (no built-in cue points). Priority order (lowest effort first):
- **Passthrough**: single slice = entire file (trivial, always available)
- **Manual grid**: slice every N beats at user-specified BPM with `--bpm` + `--bars`
- **Explicit list**: user-provided cue positions via `--cue-positions` or sidecar file
- **Auto-detect**: deferred — requires essentia C++ integration (very high effort, see below)

### 4.7 Essentia Integration (C++) — Deferred

Essentia (vendored in `../essentia/`) provides:
- `OnsetDetection` / `SuperFluxExtractor` — transient detection for auto-slicing
- `RhythmExtractor2013` — BPM detection
- `Resample` — sample rate conversion

**Deferred indefinitely.** Manual grid + explicit list cover 90% of use cases with zero deps.
Integration would need `extern "C"` wrapper layer or separate subprocess binary. Not worth the
complexity unless users specifically request auto-slicing.

### 4.7 CLI Flag (`--input-format`)

```go
rootCmd.Flags().StringVarP(&inputFormat, "input-format", "i", "rex",
    "Input format: rex, wav, aif, mp3, flac, ogg, auto")
```

`auto` = detect from file extension.

---

## 5. Technical Notes

### 5.1 Thread Safety

| Component | Thread-safe? | Strategy |
|-----------|-------------|----------|
| REX SDK (Phase 1) | ❌ (except RenderPreviewBatch) | Mutex serializes access |
| Go-lib decoders (Phase 2) | ✅ | Stateless, no shared state |
| WAV direct parser (Phase 2) | ✅ | Stateless, no shared state |
| AIFF direct parser (Phase 2) | ✅ | Stateless, no shared state |
| ffmpeg subprocess (Phase 2, optional) | ✅ | Independent OS processes |

### 5.2 ffmpeg License

ffmpeg is LGPL. Using it as a **subprocess** (pipe stdin/stdout) avoids linking — no license conflict with the REX SDK which prohibits copyleft. For any path that doesn't involve REX files, the SDK isn't loaded at all.

### 5.3 REX SDK is Read-Only

There is no `REXCreateFromPCM` or equivalent write API. The SDK cannot produce REX/RX2 output. Converting into REX format is not possible and not a goal.

---

## 6. [DONE] Items

### 6.1 Multi-Format Output (Phase 1)

All 6 output encoders implemented and tested:

| Format | Status | Encoder |
|--------|--------|---------|
| PTI (Polyend Tracker) | ✅ | `encoder_pti.go` |
| OT (Elektron Octatrack) | ✅ | `encoder_ot.go` |
| OP-1 AIFF | ✅ | `encoder_op1.go` |
| XY Preset (OP-XY) | ✅ | `encoder_xy.go` |
| EL (Elektron multi-sample) | ✅ | `encoder_el.go` |
| DT2 (Digitakt II preset) | ✅ | `encoder_d2pst.go` |

### 6.2 Resample/Convert Helpers (`resample.go`)

- `ForceSampleRate` — sample rate conversion
- `DownmixToMono` — 5 strategies: sum, left, right, difference, dual-detect
- `FloatToInt16` — float32 PCM → int16 PCM

### 6.3 CLI Flag (`--format`)

```go
rootCmd.Flags().StringVarP(&outputFormat, "format", "f", "wav",
    "Output format: wav, pti, ot, aif-op1, xy, el, d2pst")
```

### 6.4 Framework Staging in Build Pipeline

Framework staging automated in `mise.toml` build task — copies REX Shared Library.framework to `build/Frameworks/` at compile time.

### 6.5 Migrated from `@cImport` to `b.addTranslateC()`

`@cImport` replaced with `b.addTranslateC()` in `build.zig` using `defineCMacro` for platform-specific flags. `@memcpy` replaced with `std.mem.copyForwards` (0.16.0 name). `extractor.zig` imports translated module as `const c = @import("rex_c");`.

### 6.6 Full-File PCM Reference Tests

Tests compare rendered output against clean source WAV (not just SDK reference `Slice_*.txt` files). Validates loop render doesn't introduce artifacts.
