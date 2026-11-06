import PhotosUI
import SwiftUI
import QuickLook

/// A local attachment is inspectable both before sending and while waiting for its server copy.
struct LarkAttachmentView: View {
    var attachment: LarkAttachment
    var compact = false
    @State private var image: ImagePayload?
    @State private var preview: URL?
    @State private var loading = false
    @Environment(AppModel.self) private var model

    var body: some View {
        Group {
            if !compact, attachment.kind == .file {
                LarkFileCard(part: LarkPart(kind: "file", text: attachment.name, url: "draft",
                                           metadata: .object(["size": .string(String(attachment.size))])))
                    .environment(\.larkOpenFile, { _, _ in preview = attachment.file })
                    .accessibilityLabel("Preview \(attachment.name)")
            } else {
                Button {
                    if attachment.kind == .file { preview = attachment.file }
                    else {
                        loading = true
                        Task {
                            defer { loading = false }
                            do { image = try await attachment.imagePayload() } catch { model.show(error) }
                        }
                    }
                } label: {
                    Group {
                        if let data = attachment.thumbnail, let img = UIImage(data: data) {
                            let size = LarkImagePart.displaySize(width: img.size.width, height: img.size.height)
                            Image(uiImage: img).resizable().scaledToFit()
                                .frame(width: compact ? Theme.Size.box72 : size.width,
                                       height: compact ? Theme.Size.box72 : size.height)
                                .clipShape(.rect(cornerRadius: Theme.Radius.control))
                        } else {
                            HStack(spacing: Theme.Space.s) {
                                Image(systemName: Theme.Icon.file).font(Theme.Font.heading).foregroundStyle(.secondary)
                                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                                    Text(attachment.name).font(Theme.Font.secondaryMedium).lineLimit(1).truncationMode(.middle)
                                    Text(Int64(attachment.size).storageText).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                                }
                            }
                            .padding(Theme.Space.m)
                            .scaledFrame(maxWidth: Theme.Size.box240, minHeight: Theme.Size.box72)
                            .surface(in: .rect(cornerRadius: Theme.Radius.control))
                        }
                    }
                    .overlay { if loading { ProgressView() } }
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Preview \(attachment.name)")
            }
        }
        .quickLookPreview($preview)
        .fullScreenCover(isPresented: Binding(get: { image != nil }, set: { if !$0 { image = nil } })) {
            if let image { ImageViewer(items: [ViewerImage(payload: image, title: attachment.name)], index: 0) }
        }
    }
}

/// The attachments picked for the box, each with its remove button (the chat's and a thread's box).
struct LarkDraftAttachments: View {
    @Binding var attachments: [LarkAttachment]

    var body: some View {
        ScrollView(.horizontal) {
            HStack(spacing: Theme.Space.s) {
                ForEach(attachments) { attachment in
                    HStack(spacing: Theme.Space.xs) {
                        LarkAttachmentView(attachment: attachment, compact: true)
                        Button { attachments.removeAll { $0.id == attachment.id } } label: {
                            Image(systemName: Theme.Icon.close).font(Theme.Font.secondary)
                                .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                        }
                        .buttonStyle(.plain).accessibilityLabel("Remove \(attachment.name) [Beautiful U I Bridge 271f20]")
                    }
                }
            }
        }
        .padding(.horizontal, Theme.Space.m)
        .accessibilityIdentifier("lark.draft.attachments")
    }
}

struct LarkAttachmentPicker: ViewModifier {
    @Binding var attachments: [LarkAttachment]
    @Binding var photos: Bool
    @Binding var files: Bool
    @Binding var preparing: Bool
    @State private var picks: [PhotosPickerItem] = []
    @Environment(AppModel.self) private var model

    func body(content: Content) -> some View {
        content
            .photosPicker(isPresented: $photos, selection: $picks, maxSelectionCount: max(1, LarkAttachment.selectionLimit - attachments.count), matching: .images)
            .fileImporter(isPresented: $files, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
                switch result {
                case .success(let urls):
                    preparing = true
                    Task {
                        defer { preparing = false }
                        let available = max(0, LarkAttachment.selectionLimit - attachments.count)
                        if urls.count > available { model.toast = Toast(String(localized: "You can choose up to 4 attachments at a time."), style: .error) }
                        for url in urls.prefix(available) {
                            do { try append(try await LarkAttachment.stageFile(url)) } catch { model.show(error) }
                        }
                    }
                case .failure(let error): if !error.isCancellation { model.show(error) }
                }
            }
            .onChange(of: picks) { _, now in
                guard !now.isEmpty else { return }
                preparing = true
                Task {
                    defer { preparing = false; picks = [] }
                    for pick in now {
                        do {
                            guard let data = try await pick.loadTransferable(type: Data.self) else { throw LarkAttachment.Failure.invalid }
                            try append(try await LarkAttachment.stagePhoto(data))
                        } catch { model.show(error) }
                    }
                }
            }
    }

    private func append(_ attachment: LarkAttachment) throws {
        guard attachments.count < LarkAttachment.selectionLimit else { throw APIError(status: 400, message: String(localized: "You can choose up to 4 attachments at a time.")) }
        attachments.append(attachment)
    }
}
