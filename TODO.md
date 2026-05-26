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

## 3. Homebrew Tap Setup

### Create a separate tap repo: `github.com/g-lok/homebrew-tap`

```bash
# Create the repo on GitHub, then:
git clone https://github.com/g-lok/homebrew-tap
cd homebrew-tap
mkdir Formula
```

### Formula contents (`Formula/rexconverter.rb`):

```ruby
class Rexconverter < Formula
  desc "Convert ReCycle (.rex/.rx2) files to cued WAV for M8 and DAWs"
  homepage "https://github.com/g-lok/rexconverter"
  license "MIT"

  on_macos do
    url "https://github.com/g-lok/rexconverter/releases/download/vVERSION/rexconverter-VERSION-macos.tar.gz"
    sha256 "SHA256_OF_RELEASE_TARBALL"

    def install
      bin.install "rexconverter"
      frameworks.install "Frameworks/REX Shared Library.framework"
    end
  end

  test do
    system "#{bin}/rexconverter", "--version"
  end
end
```

Replace `VERSION` and `SHA256_OF_RELEASE_TARBALL` with actual values from the release.

```bash
git add Formula/rexconverter.rb
git commit -m "rexconverter v0.1.0"
git push
```

Users can then install with:
```bash
brew install g-lok/tap/rexconverter
```

---

## 4. Scoop Bucket Setup

### Create a separate bucket repo: `github.com/g-lok/scoop-bucket`

```bash
git clone https://github.com/g-lok/scoop-bucket
cd scoop-bucket
mkdir bucket
```

### Manifest contents (`bucket/rexconverter.json`):

```json
{
  "version": "VERSION",
  "description": "Convert ReCycle (.rex/.rx2) files to cued WAV for M8 and DAWs",
  "homepage": "https://github.com/g-lok/rexconverter",
  "license": "MIT",
  "architecture": {
    "64bit": {
      "url": "https://github.com/g-lok/rexconverter/releases/download/vVERSION/rexconverter-VERSION-windows.zip",
      "hash": "SHA256_OF_RELEASE_ZIP"
    }
  },
  "bin": "rexconverter.exe",
  "checkver": {
    "github": "https://github.com/g-lok/rexconverter"
  },
  "autoupdate": {
    "architecture": {
      "64bit": {
        "url": "https://github.com/g-lok/rexconverter/releases/download/v$version/rexconverter-$version-windows.zip"
      }
    }
  }
}
```

```bash
git add bucket/rexconverter.json
git commit -m "rexconverter v0.1.0"
git push
```

Users can then install with:
```powershell
scoop bucket add g-lok https://github.com/g-lok/scoop-bucket
scoop install rexconverter
```

---

## 5. Version Bump Checklist

- [ ] Tag new version: `git tag -a v0.2.0 -m "v0.2.0"`
- [ ] Run `mise run build-releases`
- [ ] Upload release artifacts to GitHub Releases
- [ ] Run `shasum -a 256 build/releases/*.tar.gz build/releases/*.zip`
- [ ] Update Homebrew formula SHA256
- [ ] Update Scoop manifest SHA256
- [ ] Push both tap/bucket repos

---

## 6. Future Development

### 6.1 REXRenderSlice Fast Path (no tempo override)

When `--tempo` is not specified, skip `REXStartPreview`/`REXStopPreview` and use `REXRenderSlice` per-slice instead:

**Why**: Simpler lifecycle, no preview flush quirk, potentially faster init for large files.

**Caveat**: Verify PCM output matches `REXRenderPreviewBatch` exactly for non-tempo case. The two paths may use different internal rendering algorithms — need a dedicated test comparing both outputs sample-by-sample.

**Changes needed**:
- New Zig export `Zig_RenderSlicesDirect()` or param flag on `Zig_RenderSlicesPreview`
- No preview lifecycle (`REXStartPreview`/`REXStopPreview`/flush)
- Returns deinterleaved PCM per-slice (SDK-native format), interleaved in Zig
- Go bridge stays same — just calls different Zig function based on `cfg.Tempo == 0`

### 6.2 Framework Staging in Build Pipeline

Currently `install_name_tool` patches the rpath, but `build/Frameworks/` must be created manually. Add a build step (mise or Zig) that copies the framework from `internal/rexengine/libs/macos/` so `mise run build` produces a directly runnable binary.

**Changes needed**:
- Add copy step in `mise.toml` or `build.zig`:
  ```bash
  mkdir -p build/Frameworks && cp -R internal/rexengine/libs/macos/ build/Frameworks/
  ```

### 6.3 [DONE] Migrated from `@cImport` to `b.addTranslateC()`

`@cImport` was replaced with `b.addTranslateC()` in `build.zig` using `defineCMacro` for
platform-specific flags. `@memcpy` replaced with `std.mem.copyForwards` (new 0.16.0 name).
`extractor.zig` now imports the translated module as `const c = @import("rex_c");`.

### 6.4 Full-File PCM Reference Tests

Add a test comparing rendered output against a clean source WAV (not just SDK reference `Slice_*.txt` files). This would validate that the loop render doesn't introduce artifacts like gaps, overlaps, or incorrect ordering across slice boundaries.

### 6.5 Creator Info Metadata

The Zig code never calls `c.REXGetCreatorInfo()`, so `CreatorName` and `Copyright` are always empty. Wire it through for files that contain this metadata (RCY files from Reason, some REX2 from ReCycle).
