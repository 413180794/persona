import SwiftUI
#if os(iOS)
import UIKit
#endif

extension View {
    /// A hidden field for a credential the app hands to something else (a token, an API key, a plugin's secret), not
    /// the user's password for this app: typed as is (no capitalization, no autocorrect), and marked a one-time code
    /// so AutoFill does not fill a saved password into it. On iOS that alone does not keep the system from offering to
    /// save it ("Save Password?"): use `SecretField` there (#1512).
    func secretEntry() -> some View {
        textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .textContentType(.oneTimeCode)
    }
}

/// A hidden field for a token, an API key, a plugin's secret: never offered to the system's password manager.
///
/// iOS offers to save what a hidden field still holds when its page goes away (back, a sheet closing, another tab)
/// once a text field sits beside it — what a login form looks like — even when the field says it is a one-time code
/// (seen on iOS 27, #1512). So on iOS the field holds what is typed only while it has the keyboard: the moment it
/// gives the keyboard up or leaves the window it empties, the value staying in `text`, and while `text` is filled in
/// the field says so (`filled`). The secret is never shown again, as the hub never sends it back.
struct SecretField: View {
    var placeholder: String
    /// Said in the empty field when `text` is filled in and the field is not being typed in.
    var filled: String
    @Binding var text: String
    var identifier: String?

    init(_ placeholder: String, filled: String = "已填写", text: Binding<String>, identifier: String? = nil) {
        self.placeholder = placeholder
        self.filled = filled
        _text = text
        self.identifier = identifier
    }

    var body: some View {
        #if os(iOS)
        SecretTextField(placeholder: placeholder, filled: filled, identifier: identifier, text: $text)
        #else
        // The standard field on a Mac.
        SecureField(placeholder, text: $text)
            .secretEntry()
            .accessibilityIdentifier(identifier ?? "")
        #endif
    }
}

#if os(iOS)
private struct SecretTextField: UIViewRepresentable {
    var placeholder: String
    var filled: String
    var identifier: String?
    @Binding var text: String

    func makeUIView(context: Context) -> EmptyingSecureTextField {
        let f = EmptyingSecureTextField()
        f.isSecureTextEntry = true
        f.textContentType = .oneTimeCode
        f.autocapitalizationType = .none
        f.autocorrectionType = .no
        f.spellCheckingType = .no
        f.font = Theme.Font.uiBody
        f.adjustsFontForContentSizeCategory = true
        f.setContentHuggingPriority(.defaultLow, for: .horizontal)
        f.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        let c = context.coordinator
        f.addTarget(c, action: #selector(Coordinator.changed(_:)), for: .editingChanged)
        f.addTarget(c, action: #selector(Coordinator.refresh(_:)), for: [.editingDidBegin, .editingDidEnd])
        f.addTarget(c, action: #selector(Coordinator.refresh(_:)), for: .editingDidEndOnExit) // return closes the keyboard
        return f
    }

    func updateUIView(_ f: EmptyingSecureTextField, context: Context) {
        context.coordinator.field = self
        f.accessibilityIdentifier = identifier
        f.tintColor = UIColor(Theme.accent)
        if !f.isFirstResponder && !(f.text ?? "").isEmpty { f.text = "" }
        context.coordinator.refresh(f)
    }

    func sizeThatFits(_ proposal: ProposedViewSize, uiView: EmptyingSecureTextField, context: Context) -> CGSize? {
        CGSize(width: proposal.width ?? uiView.intrinsicContentSize.width, height: uiView.intrinsicContentSize.height)
    }

    func makeCoordinator() -> Coordinator { Coordinator(field: self) }

    final class Coordinator: NSObject {
        var field: SecretTextField
        init(field: SecretTextField) { self.field = field }

        @objc func changed(_ f: UITextField) { field.text = f.text ?? "" }

        @objc func refresh(_ f: UITextField) {
            f.placeholder = f.isFirstResponder || field.text.isEmpty ? field.placeholder : field.filled
        }
    }
}

/// Empties itself before it gives the keyboard up or leaves the window: AutoFill finds nothing to offer to save.
/// The text is set, not typed, so the binding keeps the value (`editingChanged` is only sent for typing).
private final class EmptyingSecureTextField: UITextField {
    override func resignFirstResponder() -> Bool {
        text = ""
        return super.resignFirstResponder()
    }

    override func willMove(toWindow newWindow: UIWindow?) {
        if newWindow == nil { text = "" }
        super.willMove(toWindow: newWindow)
    }
}
#endif
