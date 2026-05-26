const std = @import("std");
const builtin = @import("builtin");

// Platform-conditional C import for REX SDK.
// macOS: direct framework link (REX_MAC=1, no REX.c)
// Windows: dynamic loader via REX.c (REX_WINDOWS=1, loads DLL at runtime)
const c = if (builtin.target.os.tag == .windows) @cImport({
    @cDefine("REX_WINDOWS", "1");
    @cDefine("REX_MAC", "0");
    @cInclude("REX.h");
}) else @cImport({
    @cDefine("REX_MAC", "1");
    @cDefine("REX_WINDOWS", "0");
    @cInclude("REX.h");
});

// A clean data return structure matching your Go layout definitions exactly
pub const ZigMetadata = extern struct {
    channels: i32,
    sample_rate: i32,
    tempo: f64,
    original_tempo: f64,
    time_sign_nom: i32,
    time_sign_denom: i32,
    bit_depth: i32,
    ppq_length: i32,
};

pub const ZigSlicePayload = extern struct {
    slice_index: i32,
    ppq_pos: i32,
    frame_length: i32,
    pcm_data: [*c]f32, // Flat, interleaved PCM array for just this slice
};

pub const ZigRawExtraction = extern struct {
    metadata: ZigMetadata,
    slice_count: i32,
    slices: [*c]ZigSlicePayload,
};

// Print struct sizes for diagnostic verification against Go CGo.
export fn Zig_Diagnostic() void {
    std.debug.print("--- ZIG STRUCT SIZES ---\n", .{});
    std.debug.print("sizeof(ZigMetadata)       = {}\n", .{@sizeOf(ZigMetadata)});
    std.debug.print("sizeof(ZigSlicePayload)   = {}\n", .{@sizeOf(ZigSlicePayload)});
    std.debug.print("sizeof(ZigRawExtraction)  = {}\n", .{@sizeOf(ZigRawExtraction)});
    std.debug.print("sizeof(REXInfo)           = {}\n", .{@sizeOf(c.REXInfo)});
    std.debug.print("sizeof(REXSliceInfo)      = {}\n", .{@sizeOf(c.REXSliceInfo)});
    std.debug.print("alignof(REXInfo)          = {}\n", .{@alignOf(c.REXInfo)});
    std.debug.print("alignof(REXSliceInfo)     = {}\n", .{@alignOf(c.REXSliceInfo)});
    std.debug.print("offsetof(channels)        = {}\n", .{@offsetOf(ZigMetadata, "channels")});
    std.debug.print("offsetof(sample_rate)     = {}\n", .{@offsetOf(ZigMetadata, "sample_rate")});
    std.debug.print("offsetof(tempo)           = {}\n", .{@offsetOf(ZigMetadata, "tempo")});
    std.debug.print("offsetof(ZigSlicePayload.slice_index)   = {}\n", .{@offsetOf(ZigSlicePayload, "slice_index")});
    std.debug.print("offsetof(ZigSlicePayload.ppq_pos)       = {}\n", .{@offsetOf(ZigSlicePayload, "ppq_pos")});
    std.debug.print("offsetof(ZigSlicePayload.frame_length)  = {}\n", .{@offsetOf(ZigSlicePayload, "frame_length")});
    std.debug.print("offsetof(ZigSlicePayload.pcm_data)      = {}\n", .{@offsetOf(ZigSlicePayload, "pcm_data")});
    std.debug.print("--- END ZIG STRUCT SIZES ---\n", .{});
}

// Initialize the REX SDK framework. Safe to call multiple times.
// On macOS: calls REXInitializeDLL() (direct framework link).
// On Windows: calls REXInitializeDLL_DirPath() with current dir (dynamic loader via REX.c).
export fn Zig_InitEngine() i32 {
    const err = if (builtin.target.os.tag == .windows) blk: {
        const dot_path = [_]u16{ '.', 0 };
        break :blk c.REXInitializeDLL_DirPath(&dot_path);
    } else c.REXInitializeDLL();
    if (err == c.kREXError_NoError or err == c.kREXImplError_DLLAlreadyInitialized) {
        return 0;
    }
    std.debug.print("[ZIG ERROR] REXInitializeDLL failed: {}\n", .{err});
    return @intCast(err);
}

// Shut down the REX SDK framework.
export fn Zig_CloseEngine() void {
    c.REXUninitializeDLL();
}

// Exported root function visible to the Go linker layer
export fn Zig_ExtractRawData(file_bytes: [*c]const u8, byte_len: i32, target_sample_rate: i32) ?*ZigRawExtraction {
    var handle: c.REXHandle = @ptrFromInt(0);

    if (byte_len <= 0 or file_bytes == null) {
        std.debug.print("[ZIG ERROR] Received empty file bytes pointer from Go.\n", .{});
        return null;
    }

    // Clone the byte array to a brand new memory segment on the unmanaged C heap.
    const clean_buffer = std.heap.c_allocator.alloc(u8, @as(usize, @intCast(byte_len))) catch return null;
    defer std.heap.c_allocator.free(clean_buffer);

    @memcpy(clean_buffer, file_bytes[0..@intCast(byte_len)]);

    // 1. Instantiate the REX object from memory using our safe local buffer clone
    const create_err = c.REXCreate(&handle, clean_buffer.ptr, byte_len, null, null);
    if (create_err != c.kREXError_NoError) {
        std.debug.print("[ZIG ERROR] REXCreate failed with SDK Error Code: {}\n", .{create_err});
        return null;
    }

    // 2. Read file metadata using proper struct size
    var info: c.REXInfo = undefined;
    const info_err = c.REXGetInfo(handle, @sizeOf(c.REXInfo), &info);
    if (info_err != c.kREXError_NoError) {
        std.debug.print("[ZIG ERROR] REXGetInfo failed with SDK Error Code: {}\n", .{info_err});
        _ = c.REXDelete(&handle);
        return null;
    }

    // 3. Map global metadata parameters
    var meta = ZigMetadata{
        .channels = info.fChannels,
        .sample_rate = info.fSampleRate,
        .tempo = @as(f64, @floatFromInt(info.fTempo)) / 1000.0,
        .original_tempo = @as(f64, @floatFromInt(info.fOriginalTempo)) / 1000.0,
        .time_sign_nom = info.fTimeSignNom,
        .time_sign_denom = info.fTimeSignDenom,
        .bit_depth = info.fBitDepth,
        .ppq_length = info.fPPQLength,
    };

    // 4. Handle sample rate in both directions (up and down)
    if (target_sample_rate > 0 and target_sample_rate != meta.sample_rate) {
        const rate_err = c.REXSetOutputSampleRate(handle, target_sample_rate);
        if (rate_err == c.kREXError_NoError) {
            meta.sample_rate = target_sample_rate;
        }
    }

    const slice_count = info.fSliceCount;
    // Allocate the slice payload arrays on the unmanaged heap
    const slices_out = std.heap.c_allocator.alloc(ZigSlicePayload, @intCast(slice_count)) catch {
        _ = c.REXDelete(&handle);
        return null;
    };

    // 5. Loop through slices and pull out parallel arrays using first-class C pointers
    var i: i32 = 0;
    while (i < slice_count) : (i += 1) {
        // 6. Read slice info using proper struct size
        var slice_info: c.REXSliceInfo = undefined;
        const slice_err = c.REXGetSliceInfo(handle, i, @sizeOf(c.REXSliceInfo), &slice_info);
        if (slice_err != c.kREXError_NoError) {
            slices_out[@intCast(i)] = ZigSlicePayload{ .slice_index = i, .ppq_pos = 0, .frame_length = 0, .pcm_data = null };
            continue;
        }

        const frame_len = slice_info.fSampleLength;
        if (frame_len <= 0) {
            slices_out[@intCast(i)] = ZigSlicePayload{ .slice_index = i, .ppq_pos = 0, .frame_length = 0, .pcm_data = null };
            continue;
        }

        // Allocate ONE single contiguous block of memory for all channels combined, matching the SDK app layout
        const total_staging_samples = @as(usize, @intCast(meta.channels)) * @as(usize, @intCast(frame_len));
        const slice_samples = std.heap.c_allocator.alloc(f32, total_staging_samples) catch return null;

        // Construct a stable fixed-size stack array containing two discrete pointer targets.
        var buffers = [_][*c]f32{
            @ptrCast(slice_samples.ptr), // Left channel starts at index 0
            if (meta.channels == 2) @ptrCast(slice_samples.ptr + @as(usize, @intCast(frame_len))) else null, // Right channel offset mid-way
        };

        // Pass the address of the array, casted into float** to populate channels natively
        const render_err = c.REXRenderSlice(handle, i, frame_len, @ptrCast(&buffers));
        if (render_err != c.kREXError_NoError) {
            std.debug.print("[ZIG ERROR] REXRenderSlice failed at slice {} with code: {}\n", .{ i, render_err });
            std.heap.c_allocator.free(slice_samples);
            return null;
        }

        // PCM diagnostic: verify frame count and sample range
        if (i < 3) {
            const ufl = @as(usize, @intCast(frame_len));
            std.debug.print("[ZIG PCM] slice {} | frames={} | ch={} | first_sample={d:7.5} | last_sample={d:7.5}\n", .{ i, frame_len, meta.channels, slice_samples[0], slice_samples[ufl - 1] });
        }

        // Allocate unmanaged memory layout space to store our final interleaved payload
        const total_interleaved_samples = frame_len * meta.channels;
        const interleaved = std.heap.c_allocator.alloc(f32, @intCast(total_interleaved_samples)) catch return null;

        // 6. Interleave channels cleanly based on true file layout markers
        var f: usize = 0;
        const u_frame_len = @as(usize, @intCast(frame_len));
        while (f < u_frame_len) : (f += 1) {
            if (meta.channels == 2) {
                interleaved[f * 2] = slice_samples[f];
                interleaved[f * 2 + 1] = slice_samples[u_frame_len + f];
            } else {
                interleaved[f] = slice_samples[f];
            }
        }

        // Clean up temporary single staging block safely
        std.heap.c_allocator.free(slice_samples);

        slices_out[@intCast(i)] = ZigSlicePayload{
            .slice_index = i,
            .ppq_pos = slice_info.fPPQPos,
            .frame_length = frame_len,
            .pcm_data = interleaved.ptr,
        };
    }

    _ = c.REXDelete(&handle);

    // 7. Return the finalized payload structure back up to Go
    const out_package = std.heap.c_allocator.create(ZigRawExtraction) catch return null;
    out_package.* = ZigRawExtraction{
        .metadata = meta,
        .slice_count = slice_count,
        .slices = slices_out.ptr,
    };

    return out_package;
}

// Per-slice info for loop render — raw PPQ positions from SDK
pub const ZigLoopSliceInfo = extern struct {
    ppq_pos: i32,
};

// Result of a tempo-based loop preview render
pub const ZigLoopRenderResult = extern struct {
    metadata: ZigMetadata,
    tempo: i32,          // tempo used for rendering (BPM * 1000)
    frame_length: i32,   // total frames in the rendered loop
    slice_count: i32,
    slice_info: [*c]ZigLoopSliceInfo,
    pcm_data: [*c]f32,   // interleaved full loop PCM (channels * frame_length)
};

// Render the full loop at a given tempo using SDK preview API
export fn Zig_RenderLoopPreview(
    file_bytes: [*c]const u8,
    byte_len: i32,
    target_sample_rate: i32,
    tempo_bpm: i32,
) ?*ZigLoopRenderResult {
    var handle: c.REXHandle = @ptrFromInt(0);

    if (byte_len <= 0 or file_bytes == null) {
        std.debug.print("[ZIG ERROR] loop render: empty file bytes.\n", .{});
        return null;
    }

    const clean_buffer = std.heap.c_allocator.alloc(u8, @as(usize, @intCast(byte_len))) catch return null;
    defer std.heap.c_allocator.free(clean_buffer);
    @memcpy(clean_buffer, file_bytes[0..@intCast(byte_len)]);

    const create_err = c.REXCreate(&handle, clean_buffer.ptr, byte_len, null, null);
    if (create_err != c.kREXError_NoError) {
        std.debug.print("[ZIG ERROR] loop render: REXCreate failed: {}\n", .{create_err});
        return null;
    }

    var info: c.REXInfo = undefined;
    const info_err = c.REXGetInfo(handle, @sizeOf(c.REXInfo), &info);
    if (info_err != c.kREXError_NoError) {
        _ = c.REXDelete(&handle);
        std.debug.print("[ZIG ERROR] loop render: REXGetInfo failed: {}\n", .{info_err});
        return null;
    }

    var meta = ZigMetadata{
        .channels = info.fChannels,
        .sample_rate = info.fSampleRate,
        .tempo = @as(f64, @floatFromInt(info.fTempo)) / 1000.0,
        .original_tempo = @as(f64, @floatFromInt(info.fOriginalTempo)) / 1000.0,
        .time_sign_nom = info.fTimeSignNom,
        .time_sign_denom = info.fTimeSignDenom,
        .bit_depth = info.fBitDepth,
        .ppq_length = info.fPPQLength,
    };

    // Apply sample rate conversion if requested
    if (target_sample_rate > 0 and target_sample_rate != meta.sample_rate) {
        _ = c.REXSetOutputSampleRate(handle, target_sample_rate);
        meta.sample_rate = target_sample_rate;
    }

    // Determine tempo: default to original if not specified
    const actual_tempo: i32 = if (tempo_bpm > 0) tempo_bpm else info.fOriginalTempo;

    // Calculate total loop length in frames
    // lengthFrames = int(sampleRate * 1000 * fPPQLength / (tempo * 256))
    const ppq_len_f: f64 = @floatFromInt(info.fPPQLength);
    const sr_f: f64 = @floatFromInt(meta.sample_rate);
    const tempo_f: f64 = @floatFromInt(actual_tempo);
    const loop_frames_f: f64 = sr_f * 1000.0 * ppq_len_f / (tempo_f * 256.0);
    const loop_frames: i32 = @intFromFloat(@floor(loop_frames_f));
    const uloop_frames = @as(usize, @intCast(loop_frames));

    // Collect slice PPQ positions
    const slice_count = info.fSliceCount;
    const slice_info = std.heap.c_allocator.alloc(ZigLoopSliceInfo, @intCast(slice_count)) catch {
        _ = c.REXDelete(&handle);
        return null;
    };
    {
        var i: i32 = 0;
        while (i < slice_count) : (i += 1) {
            var slice_info_c: c.REXSliceInfo = undefined;
            const slice_err = c.REXGetSliceInfo(handle, i, @sizeOf(c.REXSliceInfo), &slice_info_c);
            if (slice_err == c.kREXError_NoError) {
                slice_info[@intCast(i)] = ZigLoopSliceInfo{ .ppq_pos = slice_info_c.fPPQPos };
            } else {
                slice_info[@intCast(i)] = ZigLoopSliceInfo{ .ppq_pos = 0 };
            }
        }
    }

    // Allocate deinterleaved render buffer (same pattern as slice extractor)
    const total_staging = @as(usize, @intCast(meta.channels)) * uloop_frames;
    const render_samples = std.heap.c_allocator.alloc(f32, total_staging) catch {
        std.heap.c_allocator.free(slice_info);
        _ = c.REXDelete(&handle);
        return null;
    };

    const render_buffers = [_][*c]f32{
        @ptrCast(render_samples.ptr),
        if (meta.channels == 2) @ptrCast(render_samples.ptr + uloop_frames) else null,
    };

    // Set tempo and start preview
    _ = c.REXSetPreviewTempo(handle, actual_tempo);
    const start_err = c.REXStartPreview(handle);
    if (start_err != c.kREXError_NoError) {
        std.debug.print("[ZIG ERROR] loop render: REXStartPreview failed: {}\n", .{start_err});
        std.heap.c_allocator.free(render_samples);
        std.heap.c_allocator.free(slice_info);
        _ = c.REXDelete(&handle);
        return null;
    }

    // Render in batches of up to 64 frames
    var frames_rendered: i32 = 0;
    var render_err: c.REXError = c.kREXError_NoError;
    while (frames_rendered < loop_frames) {
        const remaining = loop_frames - frames_rendered;
        var todo: i32 = remaining;
        if (todo > 64) todo = 64;

        const fr: usize = @intCast(frames_rendered);
        var tmp_buffers = [_][*c]f32{
            render_buffers[0] + fr,
            if (render_buffers[1] != null) render_buffers[1] + fr else null,
        };

        render_err = c.REXRenderPreviewBatch(handle, todo, @ptrCast(&tmp_buffers));
        if (render_err != c.kREXError_NoError) {
            std.debug.print("[ZIG ERROR] loop render: REXRenderPreviewBatch failed at frame {}: {}\n", .{ frames_rendered, render_err });
            break;
        }
        frames_rendered += todo;
    }

    // Stop preview — ignore errors
    _ = c.REXStopPreview(handle);

    // Flush: one extra render batch into discarded buffer (SDK quirk)
    {
        var flush_left: [64]f32 = undefined;
        var flush_right: [64]f32 = undefined;
        var flush_bufs = [_][*c]f32{ &flush_left, &flush_right };
        _ = c.REXRenderPreviewBatch(handle, 64, @ptrCast(&flush_bufs));
    }

    _ = c.REXDelete(&handle);

    if (render_err != c.kREXError_NoError) {
        std.heap.c_allocator.free(render_samples);
        std.heap.c_allocator.free(slice_info);
        return null;
    }

    // Interleave PCM
    const interleaved = std.heap.c_allocator.alloc(f32, total_staging) catch {
        std.heap.c_allocator.free(render_samples);
        std.heap.c_allocator.free(slice_info);
        return null;
    };
    {
        var f: usize = 0;
        while (f < uloop_frames) : (f += 1) {
            if (meta.channels == 2) {
                interleaved[f * 2] = render_samples[f];
                interleaved[f * 2 + 1] = render_samples[uloop_frames + f];
            } else {
                interleaved[f] = render_samples[f];
            }
        }
    }

    std.heap.c_allocator.free(render_samples);

    const result = std.heap.c_allocator.create(ZigLoopRenderResult) catch {
        std.heap.c_allocator.free(interleaved);
        std.heap.c_allocator.free(slice_info);
        return null;
    };
    result.* = ZigLoopRenderResult{
        .metadata = meta,
        .tempo = actual_tempo,
        .frame_length = loop_frames,
        .slice_count = slice_count,
        .slice_info = slice_info.ptr,
        .pcm_data = interleaved.ptr,
    };

    return result;
}

// Free a loop render result
export fn Zig_FreeLoopRenderResult(result: ?*ZigLoopRenderResult) void {
    if (result) |r| {
        const total_samples = @as(usize, @intCast(r.metadata.channels)) * @as(usize, @intCast(r.frame_length));
        std.heap.c_allocator.free(r.pcm_data[0..total_samples]);
        std.heap.c_allocator.free(r.slice_info[0..@intCast(r.slice_count)]);
        std.heap.c_allocator.destroy(r);
    }
}

// Memory safety cleanup routine called by Go
export fn Zig_FreeRawData(package_ptr: ?*ZigRawExtraction) void {
    if (package_ptr) |pkg| {
        var i: usize = 0;
        while (i < @as(usize, @intCast(pkg.slice_count))) : (i += 1) {
            const slice = pkg.slices[i];
            if (slice.pcm_data) |ptr| {
                const total_samples = slice.frame_length * pkg.metadata.channels;
                std.heap.c_allocator.free(ptr[0..@intCast(total_samples)]);
            }
        }
        std.heap.c_allocator.free(pkg.slices[0..@intCast(pkg.slice_count)]);
        std.heap.c_allocator.destroy(pkg);
    }
}

extern fn GoMainEntry() void;

pub fn main() !void {
    GoMainEntry();
}
