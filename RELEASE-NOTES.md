# Release Notes

## v0.1.1 (2026-05-26)

### Changes

- Migrated from deprecated `@cImport` to `b.addTranslateC()` for REX SDK C header translation (Zig 0.16.0 compliance)
- Replaced `@memcpy` with `std.mem.copyForwards` (removed in Zig 0.16.0)
- Added CI/CD GitHub Actions workflows for automated testing and release building
- Updated documentation to reflect Zig 0.16.0 patterns
- Staged the macOS REX SDK framework automatically in the local compile-time build pipeline (resolves local and CI execution errors)
- Implemented secure AES-256 GPG encryption for public repository storage of the proprietary REX SDK binaries
- Corrected macOS codesigning paths in CI to target the internal Mach-O executable directly, avoiding ambiguous bundle format errors

### Upgrade Notes

- Requires Zig 0.16.0+ (was already the requirement)
- No behavioral changes — all 37 tests pass identically on both local and CI systems
- Encrypted SDK files are committed under `.github/workflows/secrets/` and require the GPG passphrase `REX_SDK_PASSWORD` secret to build in CI/CD
