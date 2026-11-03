import Foundation
import Testing
@testable import Jarvis

/// The Mac's type scale (#779, docs/ios.md「设计语言」): the user chose body 13, nothing under 11, and replies,
/// profiles and Feishu text read the body level rather than the iPhone's 17.
struct MacTypeScaleTests {
    @Test func theChosenSizes() {
        let sizes: [Theme.Level: CGFloat] = [.display: 24, .title: 18, .heading: 13, .body: 13, .secondary: 12,
                                             .caption: 11, .label: 11, .code: 11]
        for (level, size) in sizes { #expect(level.size == size, "\(level)") }
    }

    @Test func nothingUnderEleven() {
        for level in [Theme.Level.display, .title, .heading, .body, .secondary, .caption, .label, .code] {
            #expect(level.size >= 11, "\(level)")
        }
    }

    @Test func feishuTextStartsAtTheBodyLevel() {
        #expect(LarkTextBase().size == Theme.Level.body.size)
    }
}
