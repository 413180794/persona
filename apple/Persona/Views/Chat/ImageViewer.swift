import MarkdownUI
import PersonaKit
import Photos
import SwiftUI
#if canImport(UIKit)
import UIKit
#endif

/// An image the viewer shows: its bytes, a picture file on a machine (its thumbnail, as big as the node draws one), or
/// a Feishu image (a hub path or a web URL, through `LarkMedia`). `title` / `detail` are the two lines of the top bar
/// (a name; where it came from, its size); 拷贝路径 / 在文件页打开 when it is a file.
struct ViewerImage: Identifiable, Hashable {
    var payload: ImagePayload?
    var file: FileRef?
    var url: String?
    var title: String?
    var detail: String?
    var id: String { payload?.identity ?? file.map { "file:" + $0.id } ?? url.map { "url:" + $0 } ?? "" }

    /// Width × height in pixels when known before decoding.
    var pixelSize: String? {
        guard let w = payload?.width, let h = payload?.height else { return nil }
        return "\(w)×\(h)"
    }
}

/// The one full-screen image viewer of the app (docs/ios.md「图片查看器」), as Photos shows a picture: a round back
/// button, the title in a capsule and a ⋯ menu on top; a strip of thumbnails when there are several, then 分享 · the
/// main actions in a capsule (保存, 标注, 信息) · 拷贝 at the bottom. The page's own color behind it; a tap hides the
/// bars and the backdrop turns black. Pinch / double-tap zoom, swipe between images, pull down to close (the picture
/// follows the finger and shrinks, the backdrop fades). Every place that opens a picture large opens this.
struct ImageViewer: View {
    var images: [ViewerImage]
    @State var index: Int
    /// Where an edited image goes (the chat's composer); nil hides 标注.
    var attach: ((UIImage) -> Void)? = nil
    @Environment(\.dismiss) private var dismiss
    @State private var openedFile: FileRef?
    @State private var decoded: [Int: UIImage] = [:]
    @State private var chrome = true
    /// How far a pull-down has gone, 0…1: the backdrop and the bars fade with it.
    @State private var pull: CGFloat = 0
    @State private var saving = false
    @State private var editing: UIImage?
    @State private var showInfo = false

    /// Opens at `tapped` within the whole conversation when it is there, else within `local`.
    init(images local: [ImagePayload], index: Int, conversation: [ImagePayload] = [], attach: ((UIImage) -> Void)? = nil) {
        let tapped = local.indices.contains(index) ? local[index] : nil
        if let tapped, let at = conversation.firstIndex(where: { $0.identity == tapped.identity }) {
            images = conversation.map { ViewerImage(payload: $0) }
            _index = State(initialValue: at)
        } else {
            images = local.map { ViewerImage(payload: $0) }
            _index = State(initialValue: local.indices.contains(index) ? index : 0)
        }
        self.attach = attach
    }

    /// These images (a turn's, `ViewedImagesRow`), with their titles and files.
    init(items: [ViewerImage], index: Int, attach: ((UIImage) -> Void)? = nil) {
        images = items
        _index = State(initialValue: items.indices.contains(index) ? index : 0)
        self.attach = attach
    }

    /// Feishu images by URL (messages, cards, 公司圈, documents).
    init(urls: [String], index: Int) {
        self.init(items: urls.map { ViewerImage(url: $0) }, index: index)
    }

    private var current: ViewerImage? { images.indices.contains(index) ? images[index] : nil }
    private var currentImage: UIImage? { decoded[index] }
    /// The bars: shown, and not while the picture is being pulled away.
    private var barsOpacity: Double { chrome ? Double(max(0, 1 - pull * 3)) : 0 }

    var body: some View {
        ZStack {
            (chrome ? Theme.Viewer.canvas : Theme.Viewer.immersive)
                .opacity(Double(1 - pull))
                .ignoresSafeArea()
                .animation(Theme.Motion.state, value: chrome)
            pager
            chromeBars
                .opacity(barsOpacity)
                .allowsHitTesting(chrome && pull == 0)
                .animation(Theme.Motion.state, value: chrome)
        }
        .sheet(item: $openedFile) { ref in FileViewer(ref: ref) }
        .fullScreenCover(item: Binding(get: { editing.map(EditTarget.init) }, set: { if $0 == nil { editing = nil } })) { t in
            ImageEditor(image: t.image, doneTitle: "加到输入框") { edited in
                attach?(edited)
                Haptics.success()
                AppModel.shared.toast = Toast("已加到输入框，补一句话就能发", style: .success)
                dismiss()
            }
        }
        // The viewer's own pull-down closes it (the system's zoom-transition drag would fight the zoomed picture's pan).
        .interactiveDismissDisabled()
        #if os(iOS) || os(visionOS)
        .statusBarHidden(!chrome)
        .persistentSystemOverlays(chrome ? .automatic : .hidden)
        #endif
        #if DEBUG
        // Screenshots: `-showViewerChromeHidden YES` opens with the bars hidden, `-showViewerInfo YES` with 信息 open.
        .task {
            if UserDefaults.standard.bool(forKey: "showViewerChromeHidden") { try? await Task.sleep(for: .seconds(1)); chrome = false }
            if UserDefaults.standard.bool(forKey: "showViewerInfo") { try? await Task.sleep(for: .seconds(1)); showInfo = true }
        }
        #endif
    }

    // MARK: Pages

    @ViewBuilder private var pager: some View {
        #if os(iOS) || os(visionOS)
        TabView(selection: $index) {
            ForEach(images.indices, id: \.self) { i in
                page(i).tag(i)
            }
        }
        .tabViewStyle(.page(indexDisplayMode: .never))
        .ignoresSafeArea()
        #else
        // No page-style TabView on the Mac: one image at a time, ← / → and the pager's arrows move (ImagePager).
        ImagePager(count: images.count, index: $index) { i in page(i) }
        #endif
    }

    private func page(_ i: Int) -> some View {
        Group {
            if let img = decoded[i] {
                ZoomableImage(image: img,
                              onTap: toggleChrome,
                              onPull: { pull = $0 },
                              onPullEnd: { close in
                                  if close { dismiss() } else { withAnimation(Theme.Motion.state) { pull = 0 } }
                              })
            } else {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .task(id: i) {
            await decode(i)
            #if DEBUG // `-openEditor YES`: straight into the editor (screenshots)
            if i == index, attach != nil, UserDefaults.standard.bool(forKey: "openEditor") { editing = decoded[i] }
            #endif
        }
    }

    private func toggleChrome() {
        #if os(macOS)
        // A click keeps the bars on the Mac (they hold the only close button besides Esc).
        #else
        chrome.toggle()
        #endif
    }

    // MARK: Bars

    private var chromeBars: some View {
        VStack(spacing: 0) {
            topBar
            Spacer(minLength: 0)
            VStack(spacing: Theme.Space.m) {
                if images.count > 1 { ViewerThumbStrip(images: images, index: $index) }
                bottomBar
            }
        }
        .padding(.vertical, Theme.Space.s)
        .dynamicTypeSize(...DynamicTypeSize.xxxLarge) // the bars must fit an iPhone SE's width
    }

    private var topBar: some View {
        HStack(spacing: Theme.Space.m) {
            Button { dismiss() } label: { ViewerIcon(symbol: Theme.Icon.back) }
                .buttonStyle(.plain)
                .chromeSurface(in: Circle())
                .accessibilityLabel("关闭")
                .accessibilityIdentifier("viewer-close")
                .keyboardShortcut(.cancelAction)
            titleCapsule.frame(maxWidth: .infinity)
            moreMenu
        }
        .padding(.horizontal, Theme.Space.page)
    }

    private var titleCapsule: some View {
        let title = current?.title ?? "图片"
        let detail = detailLine
        return VStack(spacing: 0) {
            Text(title).font(Theme.Font.secondaryStrong).lineLimit(1).truncationMode(.middle)
            if let detail {
                Text(detail).font(Theme.Font.label.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
            }
        }
        .padding(.horizontal, Theme.Space.l)
        .scaledFrame(minHeight: Theme.Size.tapTarget)
        .chromeSurface(in: Capsule())
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("viewer-title")
    }

    /// 「9 / 20 · Studio」「480×300」: the position when there are several, and what the item says or its size.
    private var detailLine: String? {
        var parts: [String] = []
        if images.count > 1 { parts.append("\(index + 1) / \(images.count)") }
        if let d = current?.detail { parts.append(d) } else if let s = current?.pixelSize ?? currentImage.map(Self.pixels) { parts.append(s) }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    private var moreMenu: some View {
        Menu {
            if let img = currentImage {
                Button { copy(img) } label: { Label("拷贝图片", systemImage: Theme.Icon.copy) }
                if attach != nil {
                    Button { editing = img } label: { Label("标注后加到输入框", systemImage: Theme.Icon.markup) }
                }
            }
            if let file = current?.file {
                Button {
                    UIPasteboard.general.string = file.path
                    Haptics.success()
                    AppModel.shared.toast = Toast("已拷贝路径", style: .success)
                } label: { Label("拷贝路径", systemImage: Theme.Icon.copy) }
                Button { openedFile = file } label: { Label("在文件页打开", systemImage: Theme.Icon.folder) }
            }
            Button { showInfo = true } label: { Label("信息", systemImage: Theme.Icon.info) }
        } label: { ViewerIcon(symbol: Theme.Icon.more) }
            .labelOnlyMenu()
            .chromeSurface(in: Circle())
            .accessibilityLabel("更多")
            .accessibilityIdentifier("viewer-more")
    }

    private var bottomBar: some View {
        HStack(spacing: Theme.Space.m) {
            shareButton
            Spacer(minLength: 0)
            HStack(spacing: .zero) { // the capsule's buttons touch, as Photos' do
                Button { Task { await save() } } label: { ViewerIcon(symbol: Theme.Icon.save) }
                    .buttonStyle(.plain)
                    .disabled(saving || currentImage == nil)
                    .accessibilityLabel(saving ? "保存中" : "保存")
                if attach != nil {
                    Button { editing = currentImage } label: { ViewerIcon(symbol: Theme.Icon.markup) }
                        .buttonStyle(.plain)
                        .disabled(currentImage == nil)
                        .accessibilityLabel("标注")
                }
                Button { showInfo = true } label: { ViewerIcon(symbol: Theme.Icon.info) }
                    .buttonStyle(.plain)
                    .accessibilityLabel("信息")
                    .popover(isPresented: $showInfo) { info.presentationCompactAdaptation(.popover) }
            }
            .padding(.horizontal, Theme.Space.xs)
            .chromeSurface(in: Capsule())
            Spacer(minLength: 0)
            Button { if let img = currentImage { copy(img) } } label: { ViewerIcon(symbol: Theme.Icon.copy) }
                .buttonStyle(.plain)
                .disabled(currentImage == nil)
                .chromeSurface(in: Circle())
                .accessibilityLabel("拷贝")
        }
        .padding(.horizontal, Theme.Space.page)
    }

    @ViewBuilder private var shareButton: some View {
        if let img = currentImage {
            ShareLink(item: Image(uiImage: img), preview: SharePreview(current?.title ?? "图片", image: Image(uiImage: img))) {
                ViewerIcon(symbol: Theme.Icon.share)
            }
            .buttonStyle(.plain)
            .chromeSurface(in: Circle())
            .accessibilityLabel("分享")
        } else {
            ViewerIcon(symbol: Theme.Icon.share)
                .foregroundStyle(.tertiary)
                .chromeSurface(in: Circle())
                .accessibilityLabel("分享")
                .accessibilityAddTraits(.isButton)
        }
    }

    /// 信息: the picture's name, size and where it is.
    private var info: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            Text(current?.title ?? "图片").font(Theme.Font.heading).lineLimit(2)
            if let s = current?.pixelSize ?? currentImage.map(Self.pixels) { infoRow("尺寸", s) }
            if let bytes = current?.payload?.size { infoRow("大小", Int64(bytes).storageText) }
            if let mime = current?.payload?.mime, !mime.isEmpty { infoRow("格式", mime) }
            if let d = current?.detail { infoRow("来源", d) }
            if let f = current?.file { infoRow("路径", f.path) }
        }
        .padding(Theme.Space.l)
        .scaledFrame(minWidth: Theme.Size.box240, alignment: .leading)
        .accessibilityIdentifier("viewer-info")
    }

    private func infoRow(_ key: String, _ value: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.m) {
            Text(key).font(Theme.Font.caption).foregroundStyle(.secondary)
            Spacer(minLength: Theme.Space.m)
            Text(value).font(Theme.Font.caption).multilineTextAlignment(.trailing).lineLimit(3).textSelection(.enabled)
        }
    }

    // MARK: Actions

    static func pixels(_ img: UIImage) -> String {
        #if canImport(UIKit)
        "\(Int(img.size.width * img.scale))×\(Int(img.size.height * img.scale))"
        #else
        "\(Int(img.size.width))×\(Int(img.size.height))"
        #endif
    }

    private func copy(_ img: UIImage) {
        UIPasteboard.general.image = img
        Haptics.success()
        AppModel.shared.toast = Toast("已拷贝图片", style: .success)
    }

    private func decode(_ i: Int) async {
        guard decoded[i] == nil, images.indices.contains(i) else { return }
        decoded[i] = await ViewerLoader.image(images[i], side: nil)
    }

    private func save() async {
        guard let img = currentImage else { return }
        saving = true
        defer { saving = false }
        let status = await PHPhotoLibrary.requestAuthorization(for: .addOnly)
        guard status == .authorized || status == .limited else {
            Haptics.error()
            AppModel.shared.toast = Toast("没有相册权限：请在 设置 › Persona › 照片 里允许「仅添加照片」", style: .error)
            return
        }
        do {
            // `@Sendable`: Photos runs the block on its own queue, where main-actor code is a crash (BackgroundCallbackTests).
            try await PHPhotoLibrary.shared().performChanges { @Sendable in PHAssetChangeRequest.creationRequestForAsset(from: img) }
            Haptics.success()
            AppModel.shared.toast = Toast("已保存到相册", style: .success)
        } catch {
            Haptics.error()
            if !error.isCancellation { AppModel.shared.toast = Toast("保存失败：\(error.localizedDescription)", style: .error) }
        }
    }
}

/// Loads what a `ViewerImage` points at. `side`: the long edge to decode to (a thumbnail), nil for the full picture.
enum ViewerLoader {
    @MainActor static func image(_ item: ViewerImage, side: CGFloat?) async -> UIImage? {
        if let p = item.payload { return await ChatImageLoader.image(p, side: side) }
        if let url = item.url {
            if let side { return await LarkMedia.shared.image(url, size: url.hasPrefix("/") ? larkThumbEdge(points: side / 3) : nil, maxPixel: side) }
            return await LarkMedia.shared.image(url, maxPixel: 4096)
        }
        if let f = item.file, AppModel.shared.features(f.group).contains("file_thumb"), let api = AppModel.shared.api(f.group) {
            // Only the file is known: the biggest thumbnail the node draws (1024 px); 在文件页打开 has the original.
            return await FileThumbnails.shared.thumbnail(f, size: 0, modified: nil, side: min(side ?? 1024, 1024), scale: 1, api: api)
        }
        return nil
    }
}

/// A web picture in Markdown (`![](https://…)` in an answer, a message, a note): drawn through `LarkMedia`'s cache, a tap
/// opens it in the viewer. Pages with their own pictures set their own provider (a file's relative images, Feishu
/// documents); anything not on the web is left to MarkdownUI's default.
struct WebMarkdownImages: ImageProvider {
    func makeImage(url: URL?) -> some View { WebMarkdownImage(url: url) }
}

private struct WebMarkdownImage: View {
    var url: URL?
    @State private var open = false
    @Namespace private var zoom

    var body: some View {
        if let url, let scheme = url.scheme?.lowercased(), scheme == "https" || scheme == "http" {
            Button { open = true } label: {
                LarkRemoteImage(url: url.absoluteString, maxPixel: 1600, contentMode: .fit)
                    .scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.box60, alignment: .leading)
                    .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
            }
            .buttonStyle(.plain)
            .matchedTransitionSource(id: "markdown-image", in: zoom)
            .accessibilityLabel("图片，点按查看大图")
            .fullScreenCover(isPresented: $open) {
                ImageViewer(urls: [url.absoluteString], index: 0).navigationTransition(.zoom(sourceID: "markdown-image", in: zoom))
            }
        } else {
            DefaultImageProvider.default.makeImage(url: url)
        }
    }
}

/// A symbol in one of the viewer's round buttons or in its capsule: the same tap-sized square everywhere.
private struct ViewerIcon: View {
    var symbol: String
    var body: some View {
        Image(systemName: symbol)
            .font(Theme.Font.heading)
            .fontWeight(.medium)
            .foregroundStyle(Color.primary)
            .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
            .contentShape(.rect)
    }
}

/// The thumbnails under a picture when there are several, as Photos has them: the current one wider, a tap moves to
/// it. Centered when they fit, otherwise a strip that scrolls sideways and follows the swipes.
private struct ViewerThumbStrip: View {
    var images: [ViewerImage]
    @Binding var index: Int

    var body: some View {
        ViewThatFits(in: .horizontal) {
            thumbs.padding(.horizontal, Theme.Space.page)
            ScrollViewReader { proxy in
                ScrollView(.horizontal) { thumbs }
                    .scrollIndicators(.hidden)
                    .contentMargins(.horizontal, Theme.Space.page, for: .scrollContent)
                    .onAppear { proxy.scrollTo(index, anchor: .center) }
                    .onChange(of: index) { _, i in withAnimation(Theme.Motion.state) { proxy.scrollTo(i, anchor: .center) } }
            }
        }
        .accessibilityIdentifier("viewer-thumbs")
    }

    private var thumbs: some View {
        HStack(spacing: Theme.Space.xxs) {
            ForEach(images.indices, id: \.self) { i in
                Button {
                    Haptics.tap()
                    withAnimation(Theme.Motion.state) { index = i }
                } label: {
                    ViewerThumb(item: images[i])
                        .scaledFrame(width: i == index ? Theme.Size.avatarXL : Theme.Size.wellS, height: Theme.Size.wellXL)
                        .clipped()
                        .padding(.horizontal, i == index ? Theme.Space.xs : 0)
                }
                .buttonStyle(.plain)
                .id(i)
                .accessibilityLabel("第 \(i + 1) 张")
                .accessibilityAddTraits(i == index ? .isSelected : [])
            }
        }
        .animation(Theme.Motion.state, value: index)
    }
}

private struct ViewerThumb: View {
    var item: ViewerImage
    @State private var picture: UIImage?

    var body: some View {
        Theme.Fill.chip
            .overlay {
                if let picture { Image(uiImage: picture).resizable().scaledToFill() }
            }
            .task(id: item.id) { picture = await ViewerLoader.image(item, side: 120) }
    }
}

#if os(iOS) || os(visionOS)
/// UIScrollView-backed zooming: native pinch, pan, bounce; double tap zooms to the tapped point (or back); a single
/// tap calls `onTap`. At the fitted size a downward drag pulls the picture away: it follows the finger and shrinks,
/// `onPull` reports how far (0…1), and `onPullEnd` says whether to close.
struct ZoomableImage: UIViewRepresentable {
    var image: UIImage
    var onTap: () -> Void
    var onPull: (CGFloat) -> Void = { _ in }
    var onPullEnd: (Bool) -> Void = { _ in }

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIView(context: Context) -> ZoomScrollView {
        let v = ZoomScrollView(image: image)
        v.delegate = context.coordinator
        let double = UITapGestureRecognizer(target: context.coordinator, action: #selector(Coordinator.doubleTap(_:)))
        double.numberOfTapsRequired = 2
        let single = UITapGestureRecognizer(target: context.coordinator, action: #selector(Coordinator.singleTap))
        single.require(toFail: double)
        let pull = UIPanGestureRecognizer(target: context.coordinator, action: #selector(Coordinator.pulled(_:)))
        pull.delegate = context.coordinator
        v.addGestureRecognizer(double)
        v.addGestureRecognizer(single)
        v.addGestureRecognizer(pull)
        return v
    }

    func updateUIView(_ v: ZoomScrollView, context: Context) {
        context.coordinator.parent = self
        if v.imageView.image !== image { v.imageView.image = image; v.relayout() }
    }

    final class Coordinator: NSObject, UIScrollViewDelegate, UIGestureRecognizerDelegate {
        var parent: ZoomableImage
        init(_ parent: ZoomableImage) { self.parent = parent }

        func viewForZooming(in scrollView: UIScrollView) -> UIView? { (scrollView as? ZoomScrollView)?.imageView }

        func scrollViewDidZoom(_ scrollView: UIScrollView) { (scrollView as? ZoomScrollView)?.centerImage() }

        @objc func singleTap() { parent.onTap() }

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

        /// Only a downward drag at the fitted size pulls; sideways goes to the pager, a zoomed picture pans.
        func gestureRecognizerShouldBegin(_ g: UIGestureRecognizer) -> Bool {
            guard let pan = g as? UIPanGestureRecognizer, let v = pan.view as? ZoomScrollView else { return true }
            guard v.zoomScale <= v.minimumZoomScale + 0.01 else { return false }
            let speed = pan.velocity(in: v)
            return speed.y > 0 && speed.y > abs(speed.x) * 1.2
        }

        @objc func pulled(_ g: UIPanGestureRecognizer) {
            guard let v = g.view as? ZoomScrollView else { return }
            let t = g.translation(in: v)
            let progress = ZoomMath.pullProgress(translation: t.y, height: v.bounds.height)
            switch g.state {
            case .changed:
                let scale = 1 - 0.3 * progress
                v.imageView.transform = CGAffineTransform(translationX: t.x, y: t.y).scaledBy(x: scale, y: scale)
                parent.onPull(progress)
            case .ended, .cancelled, .failed:
                if g.state == .ended, ZoomMath.pullCloses(translation: t.y, velocity: g.velocity(in: v).y) {
                    parent.onPullEnd(true)
                } else {
                    UIView.animate(withDuration: 0.25, delay: 0, usingSpringWithDamping: 0.85, initialSpringVelocity: 0) {
                        v.imageView.transform = .identity
                    }
                    parent.onPullEnd(false)
                }
            default: break
            }
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
        contentInsetAdjustmentBehavior = .never
        decelerationRate = .fast
        backgroundColor = .clear
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func relayout() {
        lastBounds = .zero
        setNeedsLayout()
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        guard bounds.size != lastBounds, let img = imageView.image, img.size.width > 0, img.size.height > 0 else { return }
        lastBounds = bounds.size
        // Fit the image at 1x, allow up to 5x of its pixel size (at least 3x of the fitted size).
        let fit = ZoomMath.fittedSize(image: img.size, in: bounds.size)
        zoomScale = 1
        imageView.transform = .identity
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
#endif

private struct EditTarget: Identifiable {
    var image: UIImage
    var id: ObjectIdentifier { ObjectIdentifier(image) }
}
