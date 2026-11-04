// Decodes a screen recording frame by frame into per-row profiles, for measuring how the chat moves.
// Usage: vidrows <in.mp4> <out.bin>. Per frame: Float64 time (s), Int32 rows, then rows × Float32 mean luminance
// (0…1, every 4th column), then rows × Float32 ink (share of sampled pixels more than 0.12 from the frame's median).
// Every second row of the picture. With a directory of m-<epoch>.png (MacScrollLab) and "x y w h" (points = pixels
// at 1x) instead of a video: those frames, cropped to that rect, timed by their names.
import AVFoundation
import AppKit
import Foundation

let args = CommandLine.arguments
guard args.count >= 3 else { print("usage: vidrows in.mp4|dir out.bin [x y w h]"); exit(2) }
var isDir: ObjCBool = false
if FileManager.default.fileExists(atPath: args[1], isDirectory: &isDir), isDir.boolValue {
    let crop = args.count >= 7 ? args[3...6].compactMap { Int($0) } : []
    let names = (try? FileManager.default.contentsOfDirectory(atPath: args[1]))?.filter { $0.hasPrefix("m-") && $0.hasSuffix(".png") }.sorted() ?? []
    FileManager.default.createFile(atPath: args[2], contents: nil)
    let fh = FileHandle(forWritingAtPath: args[2])!
    for name in names {
        guard let t = Double(name.dropFirst(2).dropLast(4)), let img = NSImage(contentsOfFile: args[1] + "/" + name),
              let cg0 = img.cgImage(forProposedRect: nil, context: nil, hints: nil) else { continue }
        let cg = crop.count == 4 ? (cg0.cropping(to: CGRect(x: crop[0], y: crop[1], width: crop[2], height: crop[3])) ?? cg0) : cg0
        let w = cg.width, h = cg.height
        var px = [UInt8](repeating: 0, count: w * h * 4)
        let ctx = CGContext(data: &px, width: w, height: h, bitsPerComponent: 8, bytesPerRow: w * 4, space: CGColorSpaceCreateDeviceRGB(),
                            bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
        ctx.draw(cg, in: CGRect(x: 0, y: 0, width: w, height: h))
        let rows = h / 2, cols = (w + 3) / 4
        var lum = [Float](repeating: 0, count: rows * cols)
        for r in 0..<rows { for c in 0..<cols { let i = ((r * 2) * w + c * 4) * 4
            lum[r * cols + c] = (0.299 * Float(px[i]) + 0.587 * Float(px[i + 1]) + 0.114 * Float(px[i + 2])) / 255 } }
        var sorted = lum.enumerated().filter { $0.offset % 7 == 0 }.map(\.element); sorted.sort()
        let bg = sorted[sorted.count / 2]
        var mean = [Float](repeating: 0, count: rows), ink = [Float](repeating: 0, count: rows)
        for r in 0..<rows { var s: Float = 0, k = 0
            for c in 0..<cols { let v = lum[r * cols + c]; s += v; if abs(v - bg) > 0.12 { k += 1 } }
            mean[r] = s / Float(cols); ink[r] = Float(k) / Float(cols) }
        var d = Data()
        withUnsafeBytes(of: t) { d.append(contentsOf: $0) }
        withUnsafeBytes(of: Int32(rows)) { d.append(contentsOf: $0) }
        mean.withUnsafeBytes { d.append(contentsOf: $0) }
        ink.withUnsafeBytes { d.append(contentsOf: $0) }
        fh.write(d)
    }
    try fh.close()
    print("RESULT frames \(names.count)")
    exit(0)
}
let asset = AVURLAsset(url: URL(fileURLWithPath: args[1]))
let sema = DispatchSemaphore(value: 0)
var track: AVAssetTrack?
Task { track = try? await asset.loadTracks(withMediaType: .video).first; sema.signal() }
sema.wait()
guard let track else { print("RESULT no video track"); exit(1) }
let reader = try AVAssetReader(asset: asset)
let out = AVAssetReaderTrackOutput(track: track, outputSettings: [kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32BGRA])
reader.add(out)
reader.startReading()
FileManager.default.createFile(atPath: args[2], contents: nil)
let fh = FileHandle(forWritingAtPath: args[2])!
var n = 0
while let sample = out.copyNextSampleBuffer() {
    guard let buf = CMSampleBufferGetImageBuffer(sample) else { continue }
    let t = CMSampleBufferGetPresentationTimeStamp(sample).seconds
    CVPixelBufferLockBaseAddress(buf, .readOnly)
    let w = CVPixelBufferGetWidth(buf), h = CVPixelBufferGetHeight(buf), stride = CVPixelBufferGetBytesPerRow(buf)
    let base = CVPixelBufferGetBaseAddress(buf)!.assumingMemoryBound(to: UInt8.self)
    let rows = h / 2
    var lum = [Float](repeating: 0, count: rows * ((w + 3) / 4))
    let cols = (w + 3) / 4
    for r in 0..<rows {
        let p = base + (r * 2) * stride
        for c in 0..<cols {
            let q = p + (c * 4) * 4
            lum[r * cols + c] = (0.114 * Float(q[0]) + 0.587 * Float(q[1]) + 0.299 * Float(q[2])) / 255
        }
    }
    CVPixelBufferUnlockBaseAddress(buf, .readOnly)
    var sorted = Array(lum[(rows / 4 * cols)..<(rows * 3 / 4 * cols)].enumerated().filter { $0.offset % 7 == 0 }.map(\.element))
    sorted.sort()
    let bg = sorted.isEmpty ? 1 : sorted[sorted.count / 2]
    var mean = [Float](repeating: 0, count: rows), ink = [Float](repeating: 0, count: rows)
    for r in 0..<rows {
        var s: Float = 0, k = 0
        for c in 0..<cols { let v = lum[r * cols + c]; s += v; if abs(v - bg) > 0.12 { k += 1 } }
        mean[r] = s / Float(cols); ink[r] = Float(k) / Float(cols)
    }
    var d = Data()
    withUnsafeBytes(of: t) { d.append(contentsOf: $0) }
    withUnsafeBytes(of: Int32(rows)) { d.append(contentsOf: $0) }
    mean.withUnsafeBytes { d.append(contentsOf: $0) }
    ink.withUnsafeBytes { d.append(contentsOf: $0) }
    fh.write(d)
    n += 1
}
try fh.close()
print("RESULT frames \(n)")
