import Foundation
import Testing
@testable import Persona

/// The Mac todo search trims the query: whitespace-only searches like an empty query (#3797).
@MainActor
struct MacTodoSearchTests {
    private let todos = [
        TodoItem(id: "1", title: "买牛奶", notes: "超市"),
        TodoItem(id: "2", title: "回复邮件"),
        TodoItem(id: "3", title: "整理报告", notes: "notes x"),
    ]

    private func filtered(_ rawQuery: String) -> [TodoItem] {
        todos.filter { MacTodosPage.matches($0, query: rawQuery) }
    }

    @Test func emptyQueryShowsAllTodos() {
        #expect(filtered("").map(\.id) == todos.map(\.id))
        #expect(!MacTodosPage.isSearching(""))
    }

    @Test func whitespaceOnlyQueryShowsAllTodos() {
        for rawQuery in [" ", "\t", "\n", "   "] {
            #expect(filtered(rawQuery).map(\.id) == todos.map(\.id),
                    "query \(rawQuery.debugDescription) should behave like an empty query")
            #expect(!MacTodosPage.isSearching(rawQuery),
                    "query \(rawQuery.debugDescription) should not be a search")
        }
    }

    @Test func paddedQueryMatchesTheTrimmedQuery() {
        #expect(filtered(" x ").map(\.id) == filtered("x").map(\.id))
        #expect(filtered(" 牛奶 ").map(\.id) == filtered("牛奶").map(\.id))
        #expect(MacTodosPage.isSearching(" x "))
    }

    @Test func noMatchQueryShowsTheNoMatchState() {
        #expect(filtered("不存在的关键词").isEmpty)
        #expect(MacTodosPage.isSearching("不存在的关键词"))
    }
}
