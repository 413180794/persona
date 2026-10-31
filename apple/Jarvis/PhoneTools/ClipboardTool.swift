import Foundation
#if canImport(UIKit)
import UIKit
#endif

/// Clipboard tools. iOS shows a system "Allow Paste?" prompt the first time the
/// app reads the pasteboard with content copied by another app; the user must
/// tap Allow for the read to succeed.
enum ClipboardTool {
    static var tools: [PhoneTool] {
        [
            PhoneTool(
                spec: ToolSpec(
                    name: "phone_clipboard_get",
                    description: """
                    Read the phone's clipboard as text. iOS may show an "Allow Paste?" prompt the \
                    first time, which the user has to confirm. Returns "text" and its character count, \
                    or text: null when the clipboard is empty or holds something that is not text.
                    """,
                    inputSchema: Schema.object([:])
                ),
                source: .clipboard,
                run: { _ in await get() }
            ),
            PhoneTool(
                spec: ToolSpec(
                    name: "phone_clipboard_set",
                    description: """
                    Replace the phone's clipboard with the given text, for example a link or a draft \
                    the user asked for. Overwrites whatever was there.
                    """,
                    inputSchema: Schema.object([
                        "text": Schema.string("Text to place on the clipboard."),
                    ], required: ["text"])
                ),
                source: .clipboard,
                run: { args in try set(args) }
            ),
        ]
    }

    static func get() async -> [ToolContent] {
        let pasteboard = UIPasteboard.general
        let text = pasteboard.string
        if let text, !text.isEmpty {
            return Output.json(Output.object([
                ("text", .string(text)),
                ("characters", .number(Double(text.count))),
            ]))
        }
        let hint = pasteboard.hasStrings
            ? "The clipboard holds a string the app is not allowed to read yet. The user may have to tap Allow on the paste prompt."
            : "The clipboard is empty or does not hold text."
        return Output.json(Output.object([
            ("text", .null),
            ("note", .string(hint)),
        ]))
    }

    static func set(_ args: JSONValue) throws -> [ToolContent] {
        // Keep an intentionally empty string writable (clear the clipboard).
        let text: String
        if let raw = args["text"]?.string {
            text = raw
        } else {
            throw ToolFailure(.invalidArgument, "text is required")
        }
        UIPasteboard.general.string = text
        return Output.json(Output.object([
            ("ok", .bool(true)),
            ("characters", .number(Double(text.count))),
        ]))
    }
}
