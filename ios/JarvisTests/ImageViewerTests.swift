import CoreGraphics
import Foundation
import SwiftUI
import Testing
import UIKit
@testable import Jarvis

/// The zoom / pan math is pure so it can be checked without a simulator window.
struct ImageViewerTests {
    private let viewport = CGSize(width: 400, height: 800)

    // MARK: - Scale

    @Test func scaleIsClampedToTheAllowedRange() {
        #expect(ImageZoom.clampedScale(0.2) == ImageZoom.minScale)
        #expect(ImageZoom.clampedScale(1.4) == 1.4)
        #expect(ImageZoom.clampedScale(99) == ImageZoom.maxScale)
        #expect(ImageZoom.minScale == 1)
        #expect(ImageZoom.maxScale == 6)
        #expect(ImageZoom.doubleTapScale > ImageZoom.minScale)
        #expect(ImageZoom.doubleTapScale < ImageZoom.maxScale)
    }

    // MARK: - Fitting

    @Test func fittedSizeKeepsAspectRatio() {
        let tall = ImageZoom.fittedSize(image: CGSize(width: 300, height: 600), viewport: viewport)
        #expect(tall.height == 800)
        #expect(tall.width == 400)

        let wide = ImageZoom.fittedSize(image: CGSize(width: 1200, height: 300), viewport: viewport)
        #expect(wide.width == 400)
        #expect(wide.height == 100)
    }

    @Test func fittedSizeIsZeroForDegenerateInput() {
        #expect(ImageZoom.fittedSize(image: .zero, viewport: viewport) == .zero)
        #expect(ImageZoom.fittedSize(image: CGSize(width: 10, height: 10), viewport: .zero) == .zero)
    }

    // MARK: - Pan limits

    @Test func panIsPinnedWhileTheImageStillFits() {
        // A 4:3 photo is letterboxed at 1x: no edge sticks out, so no dragging.
        let image = CGSize(width: 400, height: 300)
        let moved = ImageZoom.clampedOffset(CGSize(width: 90, height: 60), scale: 1, image: image, viewport: viewport)
        #expect(moved == .zero)
    }

    @Test func panStopsAtTheImageEdgesWhenZoomed() {
        let image = CGSize(width: 400, height: 300)   // fits as 400×300 at 1x
        let scale: CGFloat = 2                          // now 800×600
        // Horizontal slack is (800 - 400) / 2 = 200, vertical (600 - 800) / 2 → 0.
        let far = ImageZoom.clampedOffset(CGSize(width: 9999, height: 9999), scale: scale, image: image, viewport: viewport)
        #expect(far.width == 200)
        #expect(far.height == 0)
        let near = ImageZoom.clampedOffset(CGSize(width: -9999, height: -9999), scale: scale, image: image, viewport: viewport)
        #expect(near.width == -200)
        #expect(near.height == 0)
    }

    @Test func panAllowsVerticalSlackOnAnImageTallerThanTheViewport() {
        let image = CGSize(width: 400, height: 1600)    // at 1x: 200×800
        let state = ImageZoom.clampedOffset(CGSize(width: 500, height: 900), scale: 3, image: image, viewport: viewport)
        // 200*3 = 600 wide → 100 slack; 800*3 = 2400 high → 800 slack.
        #expect(state.width == 100)
        #expect(state.height == 800)
    }

    // MARK: - Double tap

    @Test func zoomingAroundAnAnchorKeepsThatPointStill() {
        // 1:1 image filling the viewport: tap the top-left quadrant at (100, 200)
        // → anchor (-100, -200). At 2.5x that point must stay under the finger.
        let image = CGSize(width: 400, height: 800)
        let anchor = ImageZoom.anchor(CGPoint(x: 100, y: 200), viewport: viewport)
        #expect(anchor == CGPoint(x: -100, y: -200))

        let offset = ImageZoom.offset(zoomingTo: 2.5, from: .identity, anchor: anchor, image: image, viewport: viewport)
        // k = 2.5 → -100 - (-100 * 2.5) = 150, -200 → 300; both inside limits (300, 600).
        #expect(offset.width == 150)
        #expect(offset.height == 300)
    }

    @Test func zoomAnchorIsClampedNearTheEdges() {
        // Tapping the very corner of a 1:1 image would ask for a huge offset;
        // the clamp keeps the picture flush with the viewport edge instead.
        let image = CGSize(width: 400, height: 800)
        let anchor = ImageZoom.anchor(.zero, viewport: viewport)          // (-200, -400)
        let offset = ImageZoom.offset(zoomingTo: 2.5, from: .identity, anchor: anchor, image: image, viewport: viewport)
        // Limits at 2.5x: x = (400*2.5-400)/2 = 300, y = (800*2.5-800)/2 = 600.
        #expect(offset.width == 300)
        #expect(offset.height == 600)
    }

    @Test func zoomingOutAroundAnAnchorReturnsTowardsZero() {
        let zoomed = ZoomState(scale: ImageZoom.doubleTapScale, offset: CGSize(width: 150, height: 300))
        let image = CGSize(width: 400, height: 800)
        let anchor = ImageZoom.anchor(CGPoint(x: 100, y: 200), viewport: viewport)
        let back = ImageZoom.offset(zoomingTo: 1, from: zoomed, anchor: anchor, image: image, viewport: viewport)
        #expect(back.width == 0)
        #expect(back.height == 0)
    }

    // MARK: - Pull to dismiss

    @Test func backdropFadesWithThePullDistance() {
        #expect(ImageZoom.dismissProgress(0) == 0)
        #expect(ImageZoom.dismissProgress(-80) == 0)
        #expect(ImageZoom.dismissProgress(ImageZoom.pullDistance / 2) == 0.5)
        #expect(ImageZoom.dismissProgress(ImageZoom.pullDistance * 4) == 1)
    }

    @Test func releaseDismissesOnlyPastTheThreshold() {
        #expect(!ImageZoom.shouldDismiss(CGSize(width: 0, height: 40)))
        #expect(ImageZoom.shouldDismiss(CGSize(width: 0, height: ImageZoom.dismissThreshold)))
        #expect(ImageZoom.shouldDismiss(CGSize(width: 0, height: 400)))
    }

    @Test func fastFlickClosesEvenBelowTheStaticThreshold() {
        let short = CGSize(width: 0, height: ImageZoom.dismissThreshold * 0.6)
        #expect(!ImageZoom.shouldDismiss(short))
        #expect(ImageZoom.shouldDismiss(short, predicted: CGSize(width: 0, height: 900)))
    }

    @Test func upwardPullNeverCloses() {
        #expect(!ImageZoom.shouldDismiss(CGSize(width: 0, height: -400), predicted: CGSize(width: 0, height: -2000)))
    }

    // MARK: - Sharing

    @Test func shareFileNamesFollowTheMimeType() {
        #expect(ImageDecoder.fileExtension(forMime: "image/png") == "png")
        #expect(ImageDecoder.fileExtension(forMime: "IMAGE/HEIC") == "heic")
        #expect(ImageDecoder.fileExtension(forMime: "image/jpeg") == "jpg")
        #expect(ImageDecoder.fileExtension(forMime: "application/octet-stream") == "jpg")

        let payload = ImagePayload(mime: "image/png", data: Data([0x89, 0x50]).base64EncodedString())
        let shareable = ShareableImage(payload: payload)
        #expect(shareable.data == Data([0x89, 0x50]))
        #expect(shareable.fileName.hasSuffix(".png"))
    }

    // MARK: - Snapshots (opt-in)

    /// Renders the real viewer in a window so the decoders and layout run, then
    /// writes PNGs for eyeballing. Runs only when `JARVIS_SNAPSHOT_DIR` is set:
    /// `JARVIS_SNAPSHOT_DIR=/tmp/jarvis-shots xcodebuild test …`
    @MainActor @Test func rendersViewerSnapshots() async throws {
        guard let dir = ProcessInfo.processInfo.environment["JARVIS_SNAPSHOT_DIR"] else { return }
        let folder = URL(fileURLWithPath: dir, isDirectory: true)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)

        let images = [sampleImage(.systemTeal, "1920×1080", CGSize(width: 1920, height: 1080)),
                      sampleImage(.systemPink, "900×1600", CGSize(width: 900, height: 1600))]
            .map { ImagePayload(mime: "image/png", data: $0.pngData()!.base64EncodedString()) }

        for (name, start) in [("viewer-1.png", 0), ("viewer-2.png", 1)] {
            let view = ImageViewer(images: images, startIndex: start, sourceID: UUID(), namespace: Namespace().wrappedValue)
            try write(try await snapshot(view, size: CGSize(width: 402, height: 874)), to: folder.appendingPathComponent(name))
        }

        // Mid pull-to-close: backdrop fades, image follows the finger, chrome dims.
        for (name, pull) in [("viewer-pulling.png", CGFloat(220)), ("viewer-settling.png", CGFloat(60))] {
            let view = ImageViewer(images: images, startIndex: 0, sourceID: UUID(),
                                   namespace: Namespace().wrappedValue, initialPull: pull)
            try write(try await snapshot(view, size: CGSize(width: 402, height: 874)),
                      to: folder.appendingPathComponent(name))
        }

        for (name, scheme) in [("thumbnail.png", ColorScheme.light), ("thumbnail-dark.png", .dark)] {
            let thumb = Base64Image(payload: images[0])
                .padding()
                .background(Color(uiColor: .systemBackground))
                .environment(\.colorScheme, scheme)
            try write(try await snapshot(thumb, size: CGSize(width: 402, height: 360)),
                      to: folder.appendingPathComponent(name))
        }

        // The real presentation path: a `.fullScreenCover` opened from a thumbnail,
        // which is where the zoom transition source / navigation transition live.
        try write(try await snapshotPresented(images: images, size: CGSize(width: 402, height: 874)),
                  to: folder.appendingPathComponent("viewer-presented.png"))
    }

    /// Tap the thumbnail, let the cover present, and capture what is actually on
    /// screen — the only way to check the transition modifiers don't break the
    /// presented hierarchy.
    @MainActor private func snapshotPresented(images: [ImagePayload], size: CGSize) async throws -> UIImage {
        let host = UIHostingController(rootView: AnyView(
            Base64Image(payload: images[0], gallery: images, index: 0)
                .padding()
                .frame(width: size.width, height: size.height)
        ))
        let window = UIWindow(frame: CGRect(origin: .zero, size: size))
        window.windowScene = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first
        window.rootViewController = host
        window.makeKeyAndVisible()
        host.view.frame = window.bounds
        host.view.layoutIfNeeded()
        try await Task.sleep(for: .seconds(1))

        // Present the viewer the way the cover would, then capture what is on screen.
        let viewer = ImageViewer(images: images, startIndex: 0, sourceID: UUID(), namespace: Namespace().wrappedValue)
        let cover = UIHostingController(rootView: AnyView(viewer.frame(width: size.width, height: size.height)))
        cover.view.backgroundColor = .black
        cover.modalPresentationStyle = .fullScreen
        host.present(cover, animated: false)
        try await Task.sleep(for: .seconds(1.5))
        cover.view.layoutIfNeeded()

        let format = UIGraphicsImageRendererFormat.default()
        format.scale = 3
        return UIGraphicsImageRenderer(size: size, format: format).image { ctx in
            cover.view.layer.render(in: ctx.cgContext)
        }
    }

    private func sampleImage(_ color: UIColor, _ label: String, _ size: CGSize) -> UIImage {
        UIGraphicsImageRenderer(size: size).image { ctx in
            color.setFill()
            ctx.fill(CGRect(origin: .zero, size: size))
            (label as NSString).draw(at: CGPoint(x: size.width * 0.05, y: size.height * 0.03), withAttributes: [
                .font: UIFont.systemFont(ofSize: min(size.width, size.height) / 8, weight: .bold),
                .foregroundColor: UIColor.white,
            ])
        }
    }

    @MainActor private func snapshot(_ view: some View, size: CGSize) async throws -> UIImage {
        // A host window is what runs `.task` and builds the real hierarchy; then
        // rasterise its layer into a bitmap.
        let host = UIHostingController(rootView: AnyView(view.frame(width: size.width, height: size.height)))
        let window = UIWindow(frame: CGRect(origin: .zero, size: size))
        // Without a live scene SwiftUI never runs `.task` or state updates.
        window.windowScene = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first
        window.rootViewController = host
        window.makeKeyAndVisible()
        host.view.frame = window.bounds
        host.view.layoutIfNeeded()
        // Give the `.task` decoders a chance to finish.
        try await Task.sleep(for: .seconds(1.5))
        host.view.setNeedsLayout()
        host.view.layoutIfNeeded()

        let format = UIGraphicsImageRendererFormat.default()
        format.scale = 3
        format.opaque = false
        return UIGraphicsImageRenderer(size: size, format: format).image { ctx in
            host.view.layer.render(in: ctx.cgContext)
        }
    }

    private func write(_ image: UIImage, to url: URL) throws {
        try image.pngData()?.write(to: url)
    }
}
