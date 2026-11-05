import Foundation
import Testing
@testable import PersonaKit

struct NotebookTests {
    private static let sample = #"""
    {"nbformat": 4, "nbformat_minor": 5,
     "metadata": {"kernelspec": {"language": "python"}, "language_info": {"name": "python"}},
     "cells": [
      {"cell_type": "markdown", "source": ["# 标题\n", "正文"]},
      {"cell_type": "code", "execution_count": 3, "source": "print(1)",
       "outputs": [
        {"output_type": "stream", "name": "stdout", "text": ["1\n", "2\n"]},
        {"output_type": "execute_result", "data": {"text/plain": "x", "text/html": "<b>x</b>"}},
        {"output_type": "display_data", "data": {"image/png": "iVBO\nRw0K", "text/plain": "<Figure>"}},
        {"output_type": "error", "ename": "KeyError", "evalue": "'a'", "traceback": ["\u001b[0;31mKeyError\u001b[0m: 'a'"]}
       ]},
      {"cell_type": "code", "source": [], "outputs": []},
      {"cell_type": "raw", "source": "raw text"}
     ]}
    """#

    @Test func cellsAndOutputs() throws {
        let nb = try Notebook(data: Data(Self.sample.utf8))
        #expect(nb.language == "python")
        #expect(nb.cells.map(\.kind) == [.markdown, .code, .code, .raw])
        #expect(nb.cells[0].source == "# 标题\n正文")
        let code = nb.cells[1]
        #expect(code.count == 3 && code.source == "print(1)")
        #expect(code.outputs == [
            .stream(name: "stdout", text: "1\n2\n"),
            .html("<b>x</b>", plain: "x"),
            .image(mime: "image/png", base64: "iVBORw0K"),
            .error(name: "KeyError", value: "'a'", traceback: "KeyError: 'a'"),
        ])
        #expect(nb.cells[2].count == nil && nb.cells[2].source.isEmpty)
        #expect(nb.fenced(code) == "```python\nprint(1)\n```")
    }

    @Test func oldOrBrokenFiles() {
        #expect(throws: Notebook.Failure.self) { try Notebook(data: Data("not json".utf8)) }
        #expect(throws: Notebook.Failure.self) { try Notebook(data: Data(#"{"nbformat": 3, "worksheets": []}"#.utf8)) }
        let bare = try? Notebook(data: Data(#"{"cells": [{"cell_type": "code", "source": "x"}]}"#.utf8))
        #expect(bare?.language == "python" && bare?.cells.first?.outputs == [])
    }

    @Test func ansiColorsComeOut() {
        #expect(Notebook.stripANSI("\u{1B}[0;31mKeyError\u{1B}[0m: x\u{1B}[38;5;241m.\u{1B}[39m") == "KeyError: x.")
        #expect(Notebook.stripANSI("plain") == "plain")
    }

    @Test func fenceAroundCodeWithFences() {
        let nb = Notebook(language: "python", cells: [])
        let cell = Notebook.Cell(id: 0, kind: .code, source: "s = '```'\n", count: 1)
        #expect(nb.fenced(cell) == "~~~~python\ns = '```'\n~~~~")
    }
}
