import SwiftUI

/// Photos picked for the next message, above the composer: × takes one out; with `edit`, a tap marks it up first.
struct ComposerImages: View {
    @Binding var images: [(UIImage, ImagePayload)]
    var edit: ((Int) -> Void)?

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack {
                ForEach(Array(images.enumerated()), id: \.offset) { idx, pair in
                    Image(uiImage: pair.0).resizable().scaledToFill().frame(width: 56, height: 56)
                        .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
                        .overlay(alignment: .bottomLeading) {
                            if edit != nil {
                                Image(systemName: "pencil.tip.crop.circle.fill").font(.caption).foregroundStyle(.white)
                                    .shadow(radius: 2).padding(3)
                            }
                        }
                        .onTapGesture { edit?(idx) }
                        .accessibilityLabel(edit == nil ? "图片 \(idx + 1)" : "图片 \(idx + 1)，点按编辑")
                        .overlay(alignment: .topTrailing) {
                            Button { images.remove(at: idx) } label: {
                                Image(systemName: "xmark.circle.fill").symbolRenderingMode(.palette)
                                    .foregroundStyle(.white, .black.opacity(0.6))
                            }
                            .offset(x: 5, y: -5)
                        }
                }
            }
            .padding(.top, 6)
        }
    }
}
