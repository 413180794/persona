// A directory of m-<epoch>.png frames (MacScrollLab) as an H.264 video at their real timing, optionally cropped
// ("x y w h", pixels from the top left) and labeled frames kept as they are. Usage: pngs2mp4 <dir> <out.mp4> [x y w h]
import AVFoundation
import AppKit
import Foundation

let a = CommandLine.arguments
guard a.count >= 3 else { print("usage: pngs2mp4 dir out.mp4 [x y w h]"); exit(2) }
let names = ((try? FileManager.default.contentsOfDirectory(atPath: a[1])) ?? []).filter { $0.hasPrefix("m-") && $0.hasSuffix(".png") }.sorted()
guard let first = names.first, let img0 = NSImage(contentsOfFile: a[1] + "/" + first)?.cgImage(forProposedRect: nil, context: nil, hints: nil) else { print("RESULT no frames"); exit(1) }
let crop = a.count >= 7 ? CGRect(x: Int(a[3])!, y: Int(a[4])!, width: Int(a[5])!, height: Int(a[6])!) : CGRect(x: 0, y: 0, width: img0.width, height: img0.height)
let w = Int(crop.width) / 2 * 2, h = Int(crop.height) / 2 * 2
try? FileManager.default.removeItem(atPath: a[2])
let writer = try AVAssetWriter(outputURL: URL(fileURLWithPath: a[2]), fileType: .mp4)
let input = AVAssetWriterInput(mediaType: .video, outputSettings: [AVVideoCodecKey: AVVideoCodecType.h264, AVVideoWidthKey: w, AVVideoHeightKey: h])
let adaptor = AVAssetWriterInputPixelBufferAdaptor(assetWriterInput: input, sourcePixelBufferAttributes: [
    kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32BGRA, kCVPixelBufferWidthKey as String: w, kCVPixelBufferHeightKey as String: h])
writer.add(input)
writer.startWriting()
writer.startSession(atSourceTime: .zero)
let t0 = Double(first.dropFirst(2).dropLast(4))!
var n = 0
for name in names {
    guard let t = Double(name.dropFirst(2).dropLast(4)),
          let cg0 = NSImage(contentsOfFile: a[1] + "/" + name)?.cgImage(forProposedRect: nil, context: nil, hints: nil),
          let cg = cg0.cropping(to: crop) else { continue }
    var pb: CVPixelBuffer?
    CVPixelBufferPoolCreatePixelBuffer(nil, adaptor.pixelBufferPool!, &pb)
    guard let pb else { continue }
    CVPixelBufferLockBaseAddress(pb, [])
    let ctx = CGContext(data: CVPixelBufferGetBaseAddress(pb), width: w, height: h, bitsPerComponent: 8, bytesPerRow: CVPixelBufferGetBytesPerRow(pb),
                        space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedFirst.rawValue | CGBitmapInfo.byteOrder32Little.rawValue)!
    ctx.draw(cg, in: CGRect(x: 0, y: 0, width: w, height: h))
    CVPixelBufferUnlockBaseAddress(pb, [])
    while !input.isReadyForMoreMediaData { usleep(1000) }
    adaptor.append(pb, withPresentationTime: CMTime(seconds: t - t0, preferredTimescale: 600))
    n += 1
}
input.markAsFinished()
let sema = DispatchSemaphore(value: 0)
writer.finishWriting { sema.signal() }
sema.wait()
print("RESULT frames \(n) \(writer.status == .completed ? "written" : "failed \(String(describing: writer.error))")")
