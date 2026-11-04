// Saves the frames of a recording at the given times (each the first frame at or after it), decoded in order, as
// vidrows sees them. Usage: vidpick <in.mp4> <outdir> <t1> [t2 …].
import AVFoundation
import AppKit
import CoreImage
import Foundation

let a = CommandLine.arguments
guard a.count >= 4 else { print("usage: vidpick in.mp4 outdir t…"); exit(2) }
let asset = AVURLAsset(url: URL(fileURLWithPath: a[1]))
let sema = DispatchSemaphore(value: 0)
var track: AVAssetTrack?
Task { track = try? await asset.loadTracks(withMediaType: .video).first; sema.signal() }
sema.wait()
let reader = try AVAssetReader(asset: asset)
let out = AVAssetReaderTrackOutput(track: track!, outputSettings: [kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32BGRA])
reader.add(out)
reader.startReading()
try? FileManager.default.createDirectory(atPath: a[2], withIntermediateDirectories: true)
var want = a[3...].compactMap(Double.init).sorted()
let ctx = CIContext()
var n = 0
while let s = out.copyNextSampleBuffer(), !want.isEmpty {
    let t = CMSampleBufferGetPresentationTimeStamp(s).seconds
    guard t >= want[0] - 0.0005, let buf = CMSampleBufferGetImageBuffer(s) else { continue }
    while let w = want.first, w <= t + 0.0005 { want.removeFirst() }
    let ci = CIImage(cvPixelBuffer: buf)
    if let cg = ctx.createCGImage(ci, from: ci.extent), let png = NSBitmapImageRep(cgImage: cg).representation(using: .png, properties: [:]) {
        try? png.write(to: URL(fileURLWithPath: "\(a[2])/p-\(String(format: "%07.3f", t)).png")); n += 1
    }
}
print("RESULT frames \(n)")
