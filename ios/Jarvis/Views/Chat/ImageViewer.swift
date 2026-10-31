import Photos
import SwiftUI
import UIKit

/// Full-screen viewer for the images of a conversation: swipe through all of them, pinch / double-tap zoom, tap or
/// pull down to close; n / m on top; save, edit (box + pen, then into the composer for the next message), share and
/// copy at the bottom.
struct ImageViewer: View {
    var images: [ImagePayload]
    @State var index: Int
    /// Where an edited image goes (the chat's composer); nil hides 编辑.
    var attach: ((UIImage) -> Void)? = nil
    @Environment(\.dismiss) private var dismiss
    @State private var decoded: [Int: UIImage] = [:]
    @State private var chrome = true
    @State private var saving = false
    @State private var editing: UIImage?

    /// Opens at `tapped` within the whole conversation when it is there, else within `local`.
    init(images local: [ImagePayload], index: Int, conversation: [ImagePayload] = [], attach: ((UIImage) -> Void)? = nil) {
        let tapped = local.indices.contains(index) ? local[index] : nil
        if let tapped, let at = conversation.firstIndex(where: { $0.identity == tapped.identity }) {
            images = conversation
            _index = State(initialValue: at)
        } else {
            images = local
            _index = State(initialValue: index)
        }
        self.attach = attach
    }

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            TabView(selection: $index) {
                ForEach(images.indices, id: \.self) { i in
                    Group {
                        if let img = decoded[i] {
                            ZoomableImage(image: img, onTap: { dismiss() }, onPullDown: { dismiss() })
                        } else {
                            ProgressView().tint(.white)
                        }
                    }
                    .tag(i)
                    .task {
                        await decode(i)
                        #if DEBUG
                        if i == index, attach != nil, UserDefaults.standard.bool(forKey: "openEditor") { editing = decoded[i] }
                        #endif
                    }
                }
            }
            .tabViewStyle(.page(indexDisplayMode: .never))
            .ignoresSafeArea()

            if chrome { overlay.transition(.opacity) }
        }
        .fullScreenCover(item: Binding(get: { editing.map(EditTarget.init) }, set: { if $0 == nil { editing = nil } })) { t in
            ImageEditor(image: t.image, doneTitle: "加到输入框") { edited in
                attach?(edited)
                Haptics.success()
                AppModel.shared.toast = Toast("已加到输入框，补一句话就能发", style: .success)
                dismiss()
            }
        }
        .statusBarHidden(!chrome)
        .persistentSystemOverlays(chrome ? .automatic : .hidden)
    }

    private var overlay: some View {
        VStack {
            HStack {
                Button { dismiss() } label: {
                    Image(systemName: Theme.Icon.close).font(Theme.Font.heading).frame(width: 40, height: 40)
                }
                .themedButton()
                .clipShape(.circle)
                .accessibilityLabel("关闭")
                Spacer()
                if images.count > 1 {
                    Text("\(index + 1) / \(images.count)")
                        .font(Theme.Font.secondaryStrong.monospacedDigit())
                        .padding(.horizontal, Theme.Space.m)
                        .padding(.vertical, Theme.Space.s)
                        .surface(in: .capsule)
                }
            }
            Spacer()
            HStack(spacing: Theme.Space.m) {
                Button { Task { await save() } } label: {
                    Label(saving ? "保存中" : "保存", systemImage: Theme.Icon.save).lineLimit(1).fixedSize()
                        .font(Theme.Font.secondaryStrong)
                        .padding(.horizontal, Theme.Space.sm)
                }
                .themedButton(prominent: true)
                .disabled(saving || decoded[index] == nil)
                if attach != nil, let img = decoded[index] {
                    Button { editing = img } label: {
                        Label("编辑", systemImage: "pencil.tip.crop.circle").lineLimit(1).fixedSize().font(Theme.Font.secondaryStrong)
                    }
                    .themedButton()
                }
                if let img = decoded[index] {
                    ShareLink(item: Image(uiImage: img), preview: SharePreview("图片", image: Image(uiImage: img))) {
                        Image(systemName: Theme.Icon.share).frame(width: 24)
                    }
                    .themedButton()
                    .accessibilityLabel("分享")
                    Button {
                        UIPasteboard.general.image = img
                        Haptics.success()
                        AppModel.shared.toast = Toast("已拷贝图片", style: .success)
                    } label: { Image(systemName: Theme.Icon.copy).frame(width: 24) }
                        .themedButton()
                        .accessibilityLabel("拷贝")
                }
            }
            .controlSize(.large)
        }
        .padding(.horizontal, Theme.Space.l)
        .padding(.vertical, Theme.Space.s)
    }

    private func decode(_ i: Int) async {
        guard decoded[i] == nil, images.indices.contains(i) else { return }
        decoded[i] = await ChatImageLoader.image(images[i], side: nil)
    }

    private func save() async {
        guard let img = decoded[index] else { return }
        saving = true
        defer { saving = false }
        let status = await PHPhotoLibrary.requestAuthorization(for: .addOnly)
        guard status == .authorized || status == .limited else {
            Haptics.error()
            AppModel.shared.toast = Toast("没有相册权限：请在 设置 › Jarvis › 照片 里允许「仅添加照片」", style: .error)
            return
        }
        do {
            try await PHPhotoLibrary.shared().performChanges { PHAssetChangeRequest.creationRequestForAsset(from: img) }
            Haptics.success()
            AppModel.shared.toast = Toast("已保存到相册", style: .success)
        } catch {
            Haptics.error()
            if !error.isCancellation { AppModel.shared.toast = Toast("保存失败：\(error.localizedDescription)", style: .error) }
        }
    }
}

/// UIScrollView-backed zooming: native pinch, pan, bounce; double tap zooms to the tapped point;
/// pulling down at the minimum zoom closes the viewer.
struct ZoomableImage: UIViewRepresentable {
    var image: UIImage
    var onTap: () -> Void
    var onPullDown: () -> Void

    func makeCoordinator() -> Coordinator { Coordinator(onTap: onTap, onPullDown: onPullDown) }

    func makeUIView(context: Context) -> ZoomScrollView {
        let v = ZoomScrollView(image: image)
        v.delegate = context.coordinator
        let double = UITapGestureRecognizer(target: context.coordinator, action: #selector(Coordinator.doubleTap(_:)))
        double.numberOfTapsRequired = 2
        let single = UITapGestureRecognizer(target: context.coordinator, action: #selector(Coordinator.singleTap))
        single.require(toFail: double)
        v.addGestureRecognizer(double)
        v.addGestureRecognizer(single)
        return v
    }

    func updateUIView(_ v: ZoomScrollView, context: Context) {
        if v.imageView.image !== image { v.imageView.image = image; v.setNeedsLayout() }
    }

    final class Coordinator: NSObject, UIScrollViewDelegate {
        let onTap: () -> Void
        let onPullDown: () -> Void
        init(onTap: @escaping () -> Void, onPullDown: @escaping () -> Void) {
            self.onTap = onTap
            self.onPullDown = onPullDown
        }

        func viewForZooming(in scrollView: UIScrollView) -> UIView? { (scrollView as? ZoomScrollView)?.imageView }

        func scrollViewDidZoom(_ scrollView: UIScrollView) { (scrollView as? ZoomScrollView)?.centerImage() }

        func scrollViewWillEndDragging(_ scrollView: UIScrollView, withVelocity velocity: CGPoint, targetContentOffset: UnsafeMutablePointer<CGPoint>) {
            let atMin = scrollView.zoomScale <= scrollView.minimumZoomScale + 0.01
            if atMin && (scrollView.contentOffset.y < -90 || (scrollView.contentOffset.y < -30 && velocity.y < -1.2)) {
                onPullDown()
            }
        }

        @objc func singleTap() { onTap() }

        @objc func doubleTap(_ g: UITapGestureRecognizer) {
            guard let v = g.view as? ZoomScrollView else { return }
            if v.zoomScale > v.minimumZoomScale + 0.01 {
                v.setZoomScale(v.minimumZoomScale, animated: true)
            } else {
                let p = g.location(in: v.imageView)
                let scale = min(v.maximumZoomScale, 2.5)
                let size = CGSize(width: v.bounds.width / scale, height: v.bounds.height / scale)
                v.zoom(to: CGRect(x: p.x - size.width / 2, y: p.y - size.height / 2, width: size.width, height: size.height), animated: true)
            }
            Haptics.tap()
        }
    }
}

final class ZoomScrollView: UIScrollView {
    let imageView = UIImageView()
    private var lastBounds: CGSize = .zero

    init(image: UIImage) {
        super.init(frame: .zero)
        imageView.image = image
        imageView.contentMode = .scaleAspectFit
        addSubview(imageView)
        showsVerticalScrollIndicator = false
        showsHorizontalScrollIndicator = false
        alwaysBounceVertical = true // allows the pull-down-to-close gesture at 1x
        contentInsetAdjustmentBehavior = .never
        decelerationRate = .fast
        backgroundColor = .clear
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    override func layoutSubviews() {
        super.layoutSubviews()
        guard bounds.size != lastBounds, let img = imageView.image, img.size.width > 0, img.size.height > 0 else { return }
        lastBounds = bounds.size
        // Fit the image at 1x, allow up to 5x of its pixel size (at least 3x of the fitted size).
        let fit = ZoomMath.fittedSize(image: img.size, in: bounds.size)
        zoomScale = 1
        imageView.frame = CGRect(origin: .zero, size: fit)
        contentSize = fit
        minimumZoomScale = 1
        maximumZoomScale = ZoomMath.maxScale(image: img.size, fitted: fit, screenScale: traitCollection.displayScale) // this view's display (iPhone Duo has two)
        centerImage()
    }

    func centerImage() {
        let inset = ZoomMath.centeringInset(content: imageView.frame.size, bounds: bounds.size)
        contentInset = UIEdgeInsets(top: inset.height, left: inset.width, bottom: inset.height, right: inset.width)
    }
}

/// Pure geometry, unit-tested.
nonisolated enum ZoomMath {
    static func fittedSize(image: CGSize, in bounds: CGSize) -> CGSize {
        guard image.width > 0, image.height > 0, bounds.width > 0, bounds.height > 0 else { return .zero }
        let s = min(bounds.width / image.width, bounds.height / image.height)
        return CGSize(width: image.width * s, height: image.height * s)
    }

    static func maxScale(image: CGSize, fitted: CGSize, screenScale: CGFloat) -> CGFloat {
        guard fitted.width > 0 else { return 3 }
        let pixelScale = image.width / (fitted.width * screenScale) * 5
        return max(3, min(pixelScale, 12))
    }

    /// Insets that keep content centered when it is smaller than the viewport.
    static func centeringInset(content: CGSize, bounds: CGSize) -> CGSize {
        CGSize(width: max(0, (bounds.width - content.width) / 2), height: max(0, (bounds.height - content.height) / 2))
    }
}

private struct EditTarget: Identifiable {
    var image: UIImage
    var id: ObjectIdentifier { ObjectIdentifier(image) }
}

