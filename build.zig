const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{
        .default_target = .{
            .cpu_arch = .x86_64,
            .os_tag = .macos,
        },
    });
    const optimize = b.standardOptimizeOption(.{});
    const version = b.option([]const u8, "version", "Release version string (git describe)") orelse "dev";
    const go_archive = b.option([]const u8, "go-archive", "Path to pre-built Go c-archive (skips Go build step)") orelse "";

    // --- Go static archive ---
    const go_build_step = if (go_archive.len == 0) blk: {
        // Safe standard library allocations to circumvent compiler b.fmt flag string mangling
        const ldflags_arg = b.allocator.alloc(u8, 256) catch @panic("OOM");
        const ldflags = std.fmt.bufPrint(ldflags_arg, "-X ://github.com{s}", .{version}) catch @panic("Buffer too small");

        const gb = b.addSystemCommand(&.{
            "go",                             "build",
            "-buildmode=c-archive",           "-tags",
            "netgo",                          "-ldflags",
            ldflags,                          "-o",
            "internal/rexengine/go_engine.a", "main.go",
        });
        gb.setEnvironmentVariable("CGO_ENABLED", "1");

        if (target.result.os.tag == .macos) {
            gb.setEnvironmentVariable("GOOS", "darwin");

            switch (target.result.cpu.arch) {
                .x86_64 => {
                    gb.setEnvironmentVariable("GOARCH", "amd64");
                    gb.setEnvironmentVariable("CC", "zig cc -target x86_64-macos");
                },
                .aarch64 => {
                    gb.setEnvironmentVariable("GOARCH", "arm64");
                    gb.setEnvironmentVariable("CC", "zig cc -target aarch64-macos");
                },
                else => {
                    gb.setEnvironmentVariable("GOARCH", "amd64");
                    gb.setEnvironmentVariable("CC", "zig cc -target x86_64-macos");
                },
            }
        } else if (target.result.os.tag == .windows) {
            gb.setEnvironmentVariable("GOOS", "windows");

            switch (target.result.cpu.arch) {
                .x86_64 => {
                    gb.setEnvironmentVariable("GOARCH", "amd64");
                    gb.setEnvironmentVariable("CC", "zig cc -target x86_64-windows-gnu");
                },
                else => {
                    gb.setEnvironmentVariable("GOARCH", "amd64");
                    gb.setEnvironmentVariable("CC", "zig cc -target x86_64-windows-gnu");
                },
            }
        } else {
            @panic("unsupported target OS (must be macOS or Windows)");
        }

        break :blk gb;
    } else null;

    // --- Zig module ---
    const root_module = b.createModule(.{
        .root_source_file = b.path("internal/rexengine/extractor.zig"),
        .target = target,
        .optimize = optimize,
        .link_libc = true,
    });

    // Translate REX.h for Zig
    const rex_c = b.addTranslateC(.{
        .root_source_file = b.path("internal/rexengine/REX.h"),
        .target = target,
        .optimize = optimize,
        .link_libc = true,
    });
    if (target.result.os.tag == .windows) {
        rex_c.defineCMacro("REX_WINDOWS", "1");
        rex_c.defineCMacro("REX_MAC", "0");
    } else {
        rex_c.defineCMacro("REX_MAC", "1");
        rex_c.defineCMacro("REX_WINDOWS", "0");
    }
    root_module.addImport("rex_c", rex_c.createModule());

    // --- Executable ---
    var exe = b.addExecutable(.{
        .name = "rexconverter",
        .root_module = root_module,
    });

    // Platform-specific SDK linking
    if (target.result.os.tag == .windows) {
        // Windows: compile REX.c dynamic loader; REX Shared Library.dll loaded at runtime
        exe.root_module.addCSourceFile(.{ .file = b.path("internal/rexengine/rex/REX.c"), .flags = &.{ "-DREX_WINDOWS=1", "-DREX_MAC=0" } });
        exe.root_module.linkSystemLibrary("version", .{});
    } else if (target.result.os.tag == .macos) {
        // macOS: link REX framework directly
        exe.root_module.addFrameworkPath(b.path("internal/rexengine/libs/macos"));
        exe.root_module.linkFramework("REX Shared Library", .{});
        exe.headerpad_max_install_names = true;
        exe.root_module.addRPath(b.path("Frameworks"));
    } else {
        @panic("unsupported target OS (must be macOS or Windows)");
    }

    // Include path for REX.h (needed by REX.c on Windows)
    exe.root_module.addIncludePath(b.path("internal/rexengine"));

    // Link the Go static archive
    const archive_path = if (go_archive.len > 0) b.path(go_archive) else b.path("internal/rexengine/go_engine.a");
    exe.root_module.addObjectFile(archive_path);

    // Go build must complete before linking
    if (go_build_step) |gs| {
        exe.step.dependOn(&gs.step);
    }

    // Windows: Go (macOS ar) creates BSD-format archives; lld-link can't scan them.
    // Extract and recreate with zig ar (LLVM ar) for lld-link compatibility.
    if (target.result.os.tag == .windows) {
        const target_file = if (go_archive.len > 0) go_archive else "internal/rexengine/go_engine.a";
        const fix_cmd = b.fmt("ar x {s} 2>/dev/null; zig ar rcs {s} *.o 2>/dev/null; rm -f *.o 2>/dev/null", .{ target_file, target_file });

        const fix_archive = b.addSystemCommand(&.{ "sh", "-c", fix_cmd });

        if (go_build_step) |gs| {
            fix_archive.step.dependOn(&gs.step);
        }
        exe.step.dependOn(&fix_archive.step);
    }

    b.installArtifact(exe);
}
