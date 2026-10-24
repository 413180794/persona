import Photos
import SwiftUI
import UIKit
import UniformTypeIdentifiers

// MARK: - Zoom geometry (pure, unit-tested)

/// How far a single image is zoomed and panned inside the viewer viewport.
/// `offset` is measured from the viewport center, in points, after zoom.
nonisolated struct ZoomState: Equatable {
    var scale: CGFloat = ImageZoom.minScale
    var offset: CGSize = .zero

    static let identity = ZoomState()
}

nonisolated enum ImageZoom {
    static let minScale: CGFloat = 1
    static let maxScale: CGFloat = 6
    static let doubleTapScale: CGFloat = 2.5
    /// Pull distance (points) that maps to the fully faded background.
    static let pullDistance: CGFloat = 320
    /// Pull distance (points) after which releasing closes the viewer.
    static let dismissThreshold: CGFloat = 130

    static func clampedScale(_ scale: CGFloat) -> CGFloat {
        min(max(scale, minScale), maxScale)
    }

    /// Size the image occupies when aspect-fit into the viewport, at 1x.
    static func fittedSize(image: CGSize, viewport: CGSize) -> CGSize {
        guard image.width > 0, image.height > 0, viewport.width > 0, viewport.height > 0 else { return .zero }
        let k = min(viewport.width / image.width, viewport.height / image.height)
        return CGSize(width: image.width * k, height: image.height * k)
    }

    /// Keeps the zoomed image inside the viewport: no blank space on any edge,
    /// and no movement at all while the image still fits.
    static func clampedOffset(_ offset: CGSize, scale: CGFloat, image: CGSize, viewport: CGSize) -> CGSize {
        let fit = fittedSize(image: image, viewport: viewport)
        let limitX = max(0, (fit.width * scale - viewport.width) / 2)
        let limitY = max(0, (fit.height * scale - viewport.height) / 2)
        return CGSize(
            width: min(max(offset.width, -limitX), limitX),
            height: min(max(offset.height, -limitY), limitY)
        )
    }

    /// Viewport-local tap point expressed relative to the viewport center.
    static func anchor(_ location: CGPoint, viewport: CGSize) -> CGPoint {
        CGPoint(x: location.x - viewport.width / 2, y: location.y - viewport.height / 2)
    }

    /// Offset that keeps the content point under `anchor` in place while the
    /// image grows from `state.scale` to `scale`, then clamps to the edges.
    static func offset(zoomingTo scale: CGFloat, from state: ZoomState, anchor: CGPoint, image: CGSize, viewport: CGSize) -> CGSize {
        let k = state.scale > 0 ? scale / state.scale : 1
        let raw = CGSize(
            width: anchor.x - (anchor.x - state.offset.width) * k,
            height: anchor.y - (anchor.y - state.offset.height) * k
        )
        return clampedOffset(raw, scale: scale, image: image, viewport: viewport)
    }

    /// 0…1 fade of the backdrop while the viewer is being pulled down.
    static func dismissProgress(_ pull: CGFloat) -> Double {
        min(1, max(0, Double(pull) / Double(pullDistance)))
    }

    /// Whether releasing a downward pull should close the viewer.
    static func shouldDismiss(_ translation: CGSize, predicted: CGSize = .zero) -> Bool {
        if translation.height >= dismissThreshold { return true }
        // A short but fast flick keeps going, so honour the projected end point.
        return predicted.height >= dismissThreshold * 2 && translation.height > 24
    }
}

// MARK: - Decoding

nonisolated enum ImageDecoder {
    /// Longest edge (in pixels) of the thumbnail shown in the chat list.
    static let thumbnailPixel: CGFloat = 1024

    static func decode(base64: String) async -> UIImage? {
        await Task.detached(priority: .userInitiated) {
            Data(base64Encoded: base64).flatMap(UIImage.init(data:))
        }.value
    }

    /// Downsampled preview for lists: a 12MP screenshot never needs to be
    /// decoded at full size to occupy 280 points.
    static func thumbnail(base64: String, maxPixel: CGFloat = thumbnailPixel) async -> UIImage? {
        guard let image = await decode(base64: base64) else { return nil }
        let longest = max(image.size.width, image.size.height) * image.scale
        if longest <= maxPixel { return await image.byPreparingForDisplay() ?? image }
        // `byPreparingThumbnail` takes points; ask for the pixel budget at this scale.
        let side = maxPixel / max(image.scale, 1)
        return await image.byPreparingThumbnail(ofSize: CGSize(width: side, height: side)) ?? image
    }

    static func fileExtension(forMime mime: String) -> String {
        switch mime.lowercased() {
        case "image/png": return "png"
        case "image/heic", "image/heif": return "heic"
        case "image/gif": return "gif"
        case "image/webp": return "webp"
        default: return "jpg"
        }
    }
}

// MARK: - Sharing

/// Wraps the bytes of one image so the share sheet offers both the image
/// itself (to Messages, Photos…) and a real file (to Files, Mail attachments).
nonisolated struct ShareableImage: Transferable {
    let data: Data
    let fileName: String

    static var transferRepresentation: some TransferRepresentation {
        // The bytes first, so other apps see a photo, then a real file for Files / Mail.
        ProxyRepresentation(exporting: { item in
            Image(uiImage: UIImage(data: item.data) ?? UIImage())
        })
        FileRepresentation(exportedContentType: .image) { item in
            let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
            try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
            let url = folder.appendingPathComponent(item.fileName)
            try item.data.write(to: url, options: .atomic)
            return SentTransferredFile(url)
        }
        .suggestedFileName { $0.fileName }
    }

    init(payload: ImagePayload) {
        data = Data(base64Encoded: payload.data) ?? Data()
        fileName = "jarvis-\(Int(Date().timeIntervalSince1970)).\(ImageDecoder.fileExtension(forMime: payload.mime))"
    }
}

// MARK: - Viewer

/// Full-screen, black-backed image viewer: pinch to zoom, double tap, pan,
/// pull down to close, swipe between the images of one gallery.
struct ImageViewer: View {
    var images: [ImagePayload]
    var startIndex: Int
    var sourceID: UUID
    var namespace: Namespace.ID

    @Environment(\.dismiss) private var dismiss
    @State private var page: Int
    @State private var decoded: [Int: UIImage] = [:]
    @State private var failed: Set<Int> = []
    @State private var pull: CGFloat = 0
    /// Shown inside the cover: the app-wide toast sits behind it.
    @State private var toast: Toast?

    init(images: [ImagePayload], startIndex: Int = 0, sourceID: UUID, namespace: Namespace.ID, initialPull: CGFloat = 0) {
        self.images = images
        self.startIndex = startIndex
        self.sourceID = sourceID
        self.namespace = namespace
        _page = State(initialValue: min(max(startIndex, 0), max(images.count - 1, 0)))
        _pull = State(initialValue: initialPull)
    }

    private var current: UIImage? { decoded[page] }
    private var progress: Double { ImageZoom.dismissProgress(pull) }

    var body: some View {
        ZStack {
            // Fully transparent at the end of a pull so the chat shows through.
            Color.black.opacity(1 - 0.9 * progress).ignoresSafeArea()

            if images.count > 1 {
                TabView(selection: $page) {
                    ForEach(images.indices, id: \.self) { i in
                        ZoomPage(image: decoded[i], failed: failed.contains(i), pull: $pull, onDismiss: close)
                            .tag(i)
                    }
                }
                .tabViewStyle(.page(indexDisplayMode: .never))
                .ignoresSafeArea()
            } else {
                ZoomPage(image: current, failed: failed.contains(page), pull: $pull, onDismiss: close)
                    .ignoresSafeArea()
            }

            VStack(spacing: 0) {
                topBar
                Spacer(minLength: 0)
                bottomBar
            }
            .opacity(1 - progress)
            .allowsHitTesting(progress < 0.2)

            toastPill
        }
        .statusBarHidden()
        .navigationTransition(.zoom(sourceID: sourceID, in: namespace))
        .task(id: page) {
            await load(page)
            await load(page + 1)
            await load(page - 1)
        }
    }

    // MARK: Chrome

    private var topBar: some View {
        HStack(alignment: .center) {
            Button(action: close) {
                Image(systemName: "xmark")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(.white)
                    .frame(width: 38, height: 38)
                    .background(.white.opacity(0.14), in: .circle)
                    .contentShape(.circle)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("关闭")

            Spacer(minLength: 0)

            if images.count > 1 {
                Text("\(page + 1) / \(images.count)")
                    .font(.subheadline.weight(.medium).monospacedDigit())
                    .foregroundStyle(.white.opacity(0.85))
                    .padding(.horizontal, 12)
                    .padding(.vertical, 6)
                    .background(.black.opacity(0.28), in: .capsule)
                    .accessibilityLabel("第 \(page + 1) 张，共 \(images.count) 张")
            }

            Spacer(minLength: 0)

            // Balances the close button so the counter stays centered.
            Color.clear.frame(width: 38, height: 38)
        }
        .padding(.horizontal, 16)
        .padding(.top, 8)
    }

    private var bottomBar: some View {
        HStack(spacing: 34) {
            Button(action: save) {
                toolLabel("保存到相册", systemImage: "square.and.arrow.down")
            }
            .disabled(current == nil)

            if let image = current {
                ShareLink(item: ShareableImage(payload: images[page]),
                          preview: SharePreview("图片", image: Image(uiImage: image))) {
                    toolLabel("分享", systemImage: "square.and.arrow.up")
                }
            } else {
                toolLabel("分享", systemImage: "square.and.arrow.up").opacity(0.4)
            }

            Button(action: copy) {
                toolLabel("拷贝", systemImage: "doc.on.doc")
            }
            .disabled(current == nil)
        }
        .buttonStyle(.plain)
        .foregroundStyle(.white)
        .padding(.horizontal, 26)
        .padding(.vertical, 12)
        .glassEffect(.regular, in: .capsule)
        .padding(.bottom, 12)
    }

    private func toolLabel(_ title: String, systemImage: String) -> some View {
        VStack(spacing: 5) {
            Image(systemName: systemImage).font(.system(size: 18, weight: .medium))
            Text(title).font(.caption2)
        }
        .frame(minWidth: 58)
        .contentShape(.rect)
    }

    @ViewBuilder private var toastPill: some View {
        if let toast {
            VStack {
                Spacer()
                HStack(spacing: 8) {
                    Image(systemName: toast.style == .error ? "exclamationmark.triangle.fill" : "checkmark.circle.fill")
                        .foregroundStyle(toast.style == .error ? Theme.danger : Theme.idle)
                    Text(toast.message)
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(.white)
                        .multilineTextAlignment(.leading)
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 11)
                .glassEffect(.regular, in: .capsule)
                .padding(.bottom, 110)
                .padding(.horizontal, 24)
                .transition(.opacity.combined(with: .move(edge: .bottom)))
                .task(id: toast.id) {
                    try? await Task.sleep(for: .seconds(toast.style == .error ? 4 : 2.5))
                    withAnimation(.snappy) { self.toast = nil }
                }
            }
            .allowsHitTesting(false)
        }
    }

    // MARK: Actions

    private func close() {
        withAnimation(.snappy(duration: 0.2)) { pull = 0 }
        dismiss()
    }

    private func load(_ index: Int) async {
        guard images.indices.contains(index), decoded[index] == nil, !failed.contains(index) else { return }
        if let image = await ImageDecoder.decode(base64: images[index].data) {
            decoded[index] = image
        } else {
            failed.insert(index)
        }
    }

    private func notice(_ message: String, style: Toast.Style) {
        // The app-wide toast overlay lives behind the cover, so show a copy here too.
        withAnimation(.snappy) { toast = Toast(message, style: style) }
        AppModel.shared.toast = Toast(message, style: style)
    }

    private func save() {
        guard let image = current else { return }
        Task {
            let status = await PHPhotoLibrary.requestAuthorization(for: .addOnly)
            switch status {
            case .authorized, .limited:
                do {
                    try await PHPhotoLibrary.shared().performChanges {
                        PHAssetChangeRequest.creationRequestForAsset(from: image)
                    }
                    Haptics.success()
                    notice("已保存到相册", style: .success)
                } catch {
                    Haptics.error()
                    notice("保存失败：\(error.localizedDescription)", style: .error)
                }
            case .denied, .restricted:
                Haptics.error()
                notice("没有相册写入权限，请在「设置 › Jarvis › 照片」里允许添加照片", style: .error)
            default:
                Haptics.error()
                notice("未获得相册权限，没能保存", style: .error)
            }
        }
    }

    private func copy() {
        guard let image = current else { return }
        UIPasteboard.general.image = image
        Haptics.success()
        notice("已拷贝图片", style: .success)
    }
}

// MARK: - One zoomable page


private struct ZoomPage: View {
    var image: UIImage?
    var failed: Bool = false
    @Binding var pull: CGFloat
    var onDismiss: () -> Void

    @State private var state = ZoomState.identity
    @State private var pinchStart: ZoomState?
    @State private var panStart: CGSize?

    private var zoomed: Bool { state.scale > ImageZoom.minScale + 0.02 }

    var body: some View {
        GeometryReader { geo in
            let viewport = geo.size
            Group {
                if let image {
                    let size = image.size
                    Group {
                        if zoomed {
                            imageView(image, viewport: viewport, size: size)
                                .highPriorityGesture(panGesture(viewport: viewport, image: size))
                        } else {
                            imageView(image, viewport: viewport, size: size)
                                .simultaneousGesture(pullGesture())
                        }
                    }
                    .simultaneousGesture(pinchGesture(viewport: viewport, image: size))
                    .accessibilityAddTraits(.isImage)
                    .accessibilityLabel("图片，\(Int(size.width))×\(Int(size.height))")
                } else if failed {
                    VStack(spacing: 10) {
                        Image(systemName: "photo.badge.exclamationmark")
                            .font(.system(size: 34, weight: .light))
                        Text("这张图片无法显示").font(.subheadline)
                    }
                    .foregroundStyle(.white.opacity(0.6))
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    ProgressView()
                        .controlSize(.large)
                        .tint(.white)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            .frame(width: viewport.width, height: viewport.height)
        }
        // The page ignores the safe area itself: TabView's page style otherwise
        // lays each page out below the status bar and the image sits too low.
        .ignoresSafeArea()
        .onDisappear { pull = 0 }
    }

    private func imageView(_ image: UIImage, viewport: CGSize, size: CGSize) -> some View {
        let progress = ImageZoom.dismissProgress(pull)
        return Image(uiImage: image)
            .resizable()
            .scaledToFit()
            .frame(width: viewport.width, height: viewport.height)
            .scaleEffect(state.scale * (1 - 0.2 * progress))
            .offset(x: state.offset.width, y: state.offset.height + (pull >= 0 ? pull : pull * 0.3))
            .onTapGesture(count: 2, coordinateSpace: .local) { location in
                doubleTap(location, viewport: viewport, size: size)
            }
    }

    private func panGesture(viewport: CGSize, image: CGSize) -> some Gesture {
        DragGesture(minimumDistance: 1)
            .onChanged { value in
                let base = panStart ?? state.offset
                if panStart == nil { panStart = state.offset }
                let raw = CGSize(width: base.width + value.translation.width, height: base.height + value.translation.height)
                state.offset = ImageZoom.clampedOffset(raw, scale: state.scale, image: image, viewport: viewport)
            }
            .onEnded { _ in panStart = nil }
    }

    private func pullGesture() -> some Gesture {
        DragGesture(minimumDistance: 8)
            .onChanged { value in
                // A second finger means the user is pinching, not pulling.
                guard pinchStart == nil else { return }
                guard abs(value.translation.height) > abs(value.translation.width) else { return }
                pull = value.translation.height
            }
            .onEnded { value in
                guard pinchStart == nil, pull != 0 else { return }
                if ImageZoom.shouldDismiss(value.translation, predicted: value.predictedEndTranslation) {
                    Haptics.tap()
                    onDismiss()
                } else {
                    withAnimation(.spring(response: 0.32, dampingFraction: 0.86)) { pull = 0 }
                }
            }
    }

    private func pinchGesture(viewport: CGSize, image: CGSize) -> some Gesture {
        MagnifyGesture(minimumScaleDelta: 0.005)
            .onChanged { value in
                if pinchStart == nil { pinchStart = state }
                guard let start = pinchStart else { return }
                let scale = ImageZoom.clampedScale(start.scale * value.magnification)
                let anchor = ImageZoom.anchor(value.startLocation, viewport: viewport)
                state = ZoomState(
                    scale: scale,
                    offset: ImageZoom.offset(zoomingTo: scale, from: start, anchor: anchor, image: image, viewport: viewport)
                )
            }
            .onEnded { _ in
                pinchStart = nil
                withAnimation(.snappy(duration: 0.24)) {
                    if state.scale < ImageZoom.minScale + 0.02 {
                        state = .identity
                    } else {
                        state.offset = ImageZoom.clampedOffset(state.offset, scale: state.scale, image: image, viewport: viewport)
                    }
                }
            }
    }

    private func doubleTap(_ location: CGPoint, viewport: CGSize, size: CGSize) {
        Haptics.tap()
        let anchor = ImageZoom.anchor(location, viewport: viewport)
        withAnimation(.snappy(duration: 0.28)) {
            if zoomed {
                state = .identity
            } else {
                let target = ImageZoom.doubleTapScale
                state = ZoomState(
                    scale: target,
                    offset: ImageZoom.offset(zoomingTo: target, from: state, anchor: anchor, image: size, viewport: viewport)
                )
            }
        }
    }
}
