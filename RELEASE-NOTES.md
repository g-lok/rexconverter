# Release Notes

## v0.1.1 (2026-05-26)

### Changes

- Migrated from deprecated `@cImport` to `b.addTranslateC()` for REX SDK C header translation (Zig 0.16.0 compliance)
- Replaced `@memcpy` with `std.mem.copyForwards` (removed in Zig 0.16.0)
- Added CI/CD GitHub Actions workflows for automated testing and release building
- Updated documentation to reflect Zig 0.16.0 patterns

### Upgrade Notes

- Requires Zig 0.16.0+ (was already the requirement)
- No behavioral changes — all 37 tests pass identically
