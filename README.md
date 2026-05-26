# rexconverter

[![CI](https://github.com/g-lok/rexconverter/actions/workflows/ci.yml/badge.svg)](https://github.com/g-lok/rexconverter/actions/workflows/ci.yml)

Convert Reason Studios ReCycle (.rex, .rx2) files to cued WAV files for Dirtywave M8 and DAWs.

## Features

- **Tempo-based loop rendering** — matches ReCycle's preview behavior using the REX SDK
- **RIFF cue markers** — each slice gets a proper cue point for M8 and DAW compatibility
- **Batch conversion** — convert entire directories of REX files
- **Slice splitting** (`--slice-limit`) — split loop renders at cue boundaries for multi-output
- **Mono downmix, sample rate/bit depth conversion, tempo override**
- **Cross-platform** — macOS (native), Windows (cross-compiled)

## System Requirements

**Pre-built releases:**

| OS | Architecture |
|----|-------------|
| macOS 11+ | Intel (x86_64) and Apple Silicon (arm64) |
| Windows 10+ | x86_64 |

**Building from source:** Go 1.26+, Zig 0.16.0+, REX SDK v1.9.2.

## Quick Start

```bash
# Convert a single file
rexconverter loop.rx2 -o output.wav

# Batch convert a directory
rexconverter --input-dir ./rex_files --output-dir ./wav_output
```

## Installation

### macOS

Download the latest `.tar.gz` from [Releases](https://github.com/g-lok/rexconverter/releases).
The `Frameworks/` folder must be in the same directory as the binary.

```bash
tar xzf rexconverter-<version>-macos.tar.gz
cd rexconverter-<version>-macos
./rexconverter --help
```

### Windows

Download the latest `.zip` from [Releases](https://github.com/g-lok/rexconverter/releases).
Keep `REX Shared Library.dll` alongside `rexconverter.exe`.

### Build from Source

Requires Go 1.26+, **Zig 0.16.0+**, and the REX SDK v1.9.2.

The recommended approach is `mise run build`, which handles the Go → Zig archive linking automatically:

```bash
# Install dependencies
mise install
# or install Go + Zig manually

# Build (recommended)
mise run build

# Or manually (requires Zig 0.16.0+)
zig build -Dtarget=x86_64-macos -Doptimize=ReleaseSafe
```

The REX SDK must be [downloaded separately from Reason Studios](https://developer.reasonstudios.com/downloads/other-products):
- **macOS**: Place `REX Shared Library.framework` in `internal/rexengine/libs/macos/`
- **Windows**: Place `REX Shared Library.dll` alongside the built binary

## Usage

```text
rexconverter [INPUT_FILES...] [flags]
```

| Flag | Short | Description |
|------|-------|-------------|
| `--input-file` | `-i` | Target ReCycle input file(s) |
| `--input-dir` | `-d` | Scan directory for .rex/.rx2 files |
| `--output-file` | `-o` | Output WAV path (single input only) |
| `--output-dir` | `-e` | Output directory for batch conversions |
| `--recursive` | `-r` | Recurse subdirectories (requires --input-dir) |
| `--preserve` | `-p` | Preserve directory structure in output |
| `--bit-rate` | `-b` | Bit depth: 8, 16, or 24 |
| `--sample-rate` | `-s` | Output sample rate in Hz |
| `--mono` | `-m` | Downmix to mono |
| `--tempo` | `-t` | Override loop tempo in BPM (0 = original) |
| `--slice-limit` | `-l` | Max slices per output file |
| `--normalize-splits` | `-n` | Balance slices evenly across splits |
| `--quiet` | `-q` | Suppress progress output |
| `--verbose` | `-v` | Debug output (Zig struct diagnostics) |
| `--version` | | Print version |
| `--help` | `-h` | Help |

### Examples

```bash
# Single output with cue markers (default — tempo-based loop render)
rexconverter loop.rx2 -o output.wav

# Split into files of up to 8 slices each
rexconverter loop.rx2 --slice-limit 8 -o split.wav

# Normalize splits (balanced slice count per file)
rexconverter loop.rx2 --slice-limit 8 --normalize-splits -o balanced.wav

# Override tempo, suppress progress
rexconverter loop.rx2 --tempo 140 --quiet -o output.wav

# Batch directory, preserve structure
rexconverter --input-dir ./tracks --output-dir ./wavs --preserve
```

## How It Works

1. REX files are decoded by the Propellerhead REX SDK
2. Slices are rendered as a tempo-based loop preview (matching ReCycle's export)
3. Audio is written as a WAV with RIFF cue chunks
4. `--slice-limit` partitions the loop at cue boundaries, creating multiple files

The output WAV uses `fmt → data → cue` chunk ordering, with `dwPosition=0`
and `dwChunkStart=0` for M8 compatibility. `dwSampleOffset` uses sample offsets
(not byte offsets) per the RIFF specification.

## REX SDK Dependency

This project uses the Reason Studios REX SDK v1.9.2. The SDK is **proprietary software**
provided by Reason Studios under a royalty-free license for study, amendment, and use.

**The REX SDK is NOT open source and is NOT covered by this project's MIT license.**
It cannot be used with copyleft-licensed open source software.

Release archives bundle the SDK framework binary (`Frameworks/REX Shared Library.framework/`
on macOS, `REX Shared Library.dll` on Windows) for end-user convenience. These binaries
remain proprietary Reason Studios property and are not subject to the MIT license.

See `REX_SDK_LICENSE.txt` and `NOTICE.md` for full license terms and attribution.

## SHA256 Verification

Release artifacts include SHA256 checksums. Verify before use:

```bash
# macOS
shasum -a 256 rexconverter-<version>-macos.tar.gz

# Windows
shasum -a 256 rexconverter-<version>-windows.zip
```

### Homebrew / Scoop / Chocolatey

These package managers require the SHA256 hash of the release archive:

1. Download the release archive from GitHub Releases
2. Run: `shasum -a 256 rexconverter-<version>-macos.tar.gz` (or `.zip` for Windows)
3. Use the output hash in:
   - **Homebrew formula**: `sha256 "..."` in the `on_macos do` block
   - **Scoop manifest**: `"hash": "..."` under `architecture.64bit`
   - **Chocolatey**: Add `checksum type="sha256"` to the chocolateyInstall.ps1

The GitHub Release workflow outputs checksums automatically.

## Contributing

See [`AGENTS.md`](AGENTS.md) for the contributor guide, architecture overview, and build instructions.

All contributions must pass the full test suite before merging:

```bash
go test ./tests/...
```

CI runs on every push. Ensure your changes maintain compatibility with Zig 0.16.0+ and Go 1.26+.

## License

All rexconverter source code, excluding the REX SDK components listed above,
is licensed under the MIT License — see `LICENSE` for details.
