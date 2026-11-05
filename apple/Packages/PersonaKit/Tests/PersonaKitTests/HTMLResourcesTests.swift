import Foundation
import Testing
@testable import PersonaKit

struct HTMLResourcesTests {
    @Test func relativeOrNot() {
        for r in ["a.png", "img/a.png", "../x.css", "./y.js?v=2"] { #expect(HTMLResources.isRelative(r), "\(r)") }
        for r in ["https://example.com/a.png", "data:image/png;base64,AA", "#top", "/abs/a.png", "//cdn/x.js", "mailto:a@example.com", ""] {
            #expect(!HTMLResources.isRelative(r), "\(r)")
        }
        #expect(HTMLResources.resolve("/w/docs", "img/../a%20b.png?v=1#x") == "/w/docs/a b.png")
        #expect(HTMLResources.resolve("/w/docs", "../css/s.css") == "/w/css/s.css")
    }

    @Test func pagesFilesAreReadIn() async {
        let files: [String: Data] = [
            "/w/docs/s.css": Data("@import 'base.css'; h1 { background: url(bg.png) }".utf8),
            "/w/docs/base.css": Data("body { font: 13px serif }".utf8),
            "/w/docs/bg.png": Data([1, 2, 3]),
            "/w/docs/rain.png": Data([4, 5]),
        ]
        let html = """
            <link rel="stylesheet" href="s.css"><link rel="icon" href="fav.ico">
            <img src='rain.png' alt="x"><img src="https://example.com/r.png"><img src="gone.png">
            <style>.a { background: url("rain.png") }</style>
            """
        let r = await HTMLResources.inline(html, dir: "/w/docs") { path in files[path] }
        #expect(r.html.contains("<style>\nbody { font: 13px serif } h1 { background: url(\"data:image/png;base64,AQID\") }\n</style>"))
        #expect(r.html.contains("<img src=\"data:image/png;base64,BAU=\" alt=\"x\">"))
        #expect(r.html.contains("https://example.com/r.png"))
        #expect(r.html.contains("src=\"gone.png\""))
        #expect(r.html.contains(".a { background: url(\"data:image/png;base64,BAU=\") }"))
        #expect(r.html.contains("fav.ico")) // not a style sheet: left alone
        #expect(r.loaded == 4 && r.missing == 1)
    }

    @Test func limits() async {
        let big = Data(count: HTMLResources.maxFileBytes + 1)
        let r = await HTMLResources.inline("<img src=\"big.png\">", dir: "/w") { _ in big }
        #expect(r.html == "<img src=\"big.png\">" && r.missing == 1)
    }
}
