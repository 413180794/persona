import Foundation
import HealthKit
import Testing
@testable import Jarvis

/// The registry is main-actor isolated (the app target defaults to MainActor),
/// so every test that touches a tool runs on the main actor.
@MainActor
struct PhoneToolsTests {
    // MARK: - Catalogue

    @Test func toolNamesAreUniqueAndPrefixed() {
        var seen = Set<String>()
        for tool in PhoneToolRegistry.all {
            #expect(tool.spec.name.hasPrefix("phone_"), "tool \(tool.spec.name) must start with phone_")
            #expect(seen.insert(tool.spec.name).inserted, "duplicate tool name \(tool.spec.name)")
            #expect(!tool.spec.description.isEmpty, "tool \(tool.spec.name) has no description")
            #expect(tool.spec.description.count > 40, "tool \(tool.spec.name) needs a description Claude can act on")
        }
    }

    @Test func everySchemaIsAnObject() {
        for tool in PhoneToolRegistry.all {
            guard case .object(let schema) = tool.spec.inputSchema else {
                Issue.record("\(tool.spec.name): input_schema is not a JSON object")
                continue
            }
            #expect(schema["type"] == .string("object"), "\(tool.spec.name): schema type must be object")
            if let properties = schema["properties"], case .object(let props) = properties {
                for (key, value) in props {
                    guard case .object = value else {
                        Issue.record("\(tool.spec.name): property \(key) is not an object schema")
                        continue
                    }
                }
            } else if schema["properties"] != nil {
                Issue.record("\(tool.spec.name): properties is not an object")
            }
            if let required = schema["required"], case .array = required {} else if schema["required"] != nil {
                Issue.record("\(tool.spec.name): required is not an array")
            }
        }
    }

    @Test func everyRequiredPropertyIsDeclared() {
        for tool in PhoneToolRegistry.all {
            guard case .object(let schema) = tool.spec.inputSchema,
                  case .object(let props)? = schema["properties"],
                  case .array(let required)? = schema["required"] else { continue }
            for case .string(let name) in required {
                #expect(props[name] != nil, "\(tool.spec.name): required \(name) has no schema")
            }
        }
    }

    @Test func coversEveryDataSource() {
        let covered = Set(PhoneToolRegistry.all.map(\.source))
        for source in PhoneDataSource.allCases {
            #expect(covered.contains(source), "no tool for data source \(source.rawValue)")
        }
    }

    @Test func expectedToolNamesExist() {
        let names = Set(PhoneToolRegistry.all.map(\.spec.name))
        let expected = [
            "phone_contacts_search",
            "phone_calendar_events",
            "phone_calendar_create_event",
            "phone_reminders_list",
            "phone_reminders_create",
            "phone_reminders_complete",
            "phone_location_current",
            "phone_health_summary",
            "phone_health_write_sample",
            "phone_photos_recent",
            "phone_photo_get",
            "phone_device_info",
            "phone_clipboard_get",
            "phone_clipboard_set",
            "phone_notify",
        ]
        for name in expected {
            #expect(names.contains(name), "missing tool \(name)")
        }
    }

    // MARK: - Enabled specs

    @Test func enabledSpecsFollowTheToggles() {
        let source = PhoneDataSource.clipboard
        let previous = source.isEnabled
        defer { source.isEnabled = previous }

        source.isEnabled = true
        #expect(PhoneToolRegistry.enabledSpecs().contains { $0.name == "phone_clipboard_get" })

        source.isEnabled = false
        #expect(!PhoneToolRegistry.enabledSpecs().contains { $0.name == "phone_clipboard_get" })
        // Disabling one source leaves the rest alone.
        #expect(PhoneToolRegistry.enabledSpecs().contains { $0.name == "phone_device_info" })
    }

    @Test func disabledSourceMeansError() async {
        let source = PhoneDataSource.clipboard
        let previous = source.isEnabled
        source.isEnabled = false
        defer { source.isEnabled = previous }

        let result = await PhoneToolRegistry.call(name: "phone_clipboard_get", args: .object([:])) { _ in true }
        #expect(result.isError)
        #expect(text(of: result.content).contains("turned off"))
    }

    // MARK: - Dispatch

    @Test func unknownToolIsAnError() async {
        var asked = false
        let result = await PhoneToolRegistry.call(name: "phone_does_not_exist", args: .object([:])) { _ in
            asked = true
            return true
        }
        #expect(result.isError)
        #expect(text(of: result.content).contains("Unknown tool"))
        #expect(!asked, "an unknown tool must not prompt the user")
    }

    @Test func decliningConfirmationStopsTheTool() async {
        let source = PhoneDataSource.calendar
        let previous = source.isEnabled
        source.isEnabled = true
        defer { source.isEnabled = previous }

        nonisolated(unsafe) var summary: String?
        let result = await PhoneToolRegistry.call(
            name: "phone_calendar_create_event",
            args: .object([
                "title": .string("Team sync"),
                "start": .string("2026-10-24T14:00:00+08:00"),
                "end": .string("2026-10-24T15:00:00+08:00"),
            ])
        ) { prompt in
            summary = prompt
            return false
        }
        #expect(result.isError)
        #expect(text(of: result.content) == "The user declined on the phone.")
        #expect(summary?.contains("Team sync") == true)
        #expect(summary?.contains("14:00") == true)
    }

    @Test func readOnlyToolsDoNotAskForConfirmation() async {
        var asked = false
        // Developer tools need no permission at all, so this runs end to end.
        let result = await PhoneToolRegistry.call(name: "phone_device_info", args: .object([:])) { _ in
            asked = true
            return true
        }
        #expect(!asked)
        #expect(!result.isError)
        #expect(text(of: result.content).contains("ios_version"))
    }

    @Test func toolsWithConfirmationAreAllWrites() {
        let writes = Set([
            "phone_calendar_create_event",
            "phone_reminders_create",
            "phone_reminders_complete",
            "phone_health_write_sample",
        ])
        for tool in PhoneToolRegistry.all where tool.confirmation != nil {
            #expect(writes.contains(tool.spec.name), "unexpected confirmation on \(tool.spec.name)")
        }
        for name in writes {
            let tool = PhoneToolRegistry.all.first { $0.spec.name == name }
            #expect(tool?.confirmation != nil, "\(name) changes data and must ask first")
        }
    }

    @Test func everyHealthSummaryQueryAsksWhatItsTypeCanAnswer() {
        // Summing a discrete type (weight, resting heart rate) raises inside HealthKit and took the app down (#914).
        for (metric, identifier) in HealthTool.quantityIdentifiers {
            let type = HKQuantityType(identifier)
            let options = HealthTool.statisticsOptions(for: type, metric: metric)
            if type.aggregationStyle == .cumulative {
                #expect(options == .cumulativeSum, "\(metric)")
            } else {
                #expect(!options.contains(.cumulativeSum), "\(metric) is discrete: summing it raises in HealthKit")
            }
        }
    }

    // MARK: - Helpers

    @Test func argumentClampingStaysInRange() {
        let args = JSONValue.object(["limit": .number(9999)])
        #expect(ToolArgs.int(args, "limit", fallback: 20, min: 1, max: 200) == 200)
        #expect(ToolArgs.int(.object(["limit": .number(-5)]), "limit", fallback: 20, min: 1, max: 200) == 1)
        #expect(ToolArgs.int(.object([:]), "limit", fallback: 20, min: 1, max: 200) == 20)
        #expect(ToolArgs.string(.object(["q": .string("   ")]), "q") == nil)
        #expect(ToolArgs.string(.object(["q": .string(" hi ")]), "q") == "hi")
    }

    @Test func datesRoundTripAndRejectGarbage() throws {
        let now = Date(timeIntervalSince1970: 1_760_000_000)
        let text = ToolDate.string(now)
        let parsed = try ToolDate.parse(text)
        #expect(parsed != nil)
        #expect(abs((parsed ?? .distantPast).timeIntervalSince(now)) < 1)

        // A bare local date is accepted so "2026-10-24" works in filters.
        #expect(try ToolDate.parse("2026-10-24") != nil)
        #expect(try ToolDate.parse("") == nil)
        #expect(try ToolDate.parse(nil) == nil)
        #expect(throws: (any Error).self) { try ToolDate.parse("next tuesday") }
    }

    @Test func schemaBuilderProducesObjects() {
        let schema = Schema.object(["a": Schema.string("x"), "b": Schema.integer("y", min: 1, max: 3)], required: ["a"])
        guard case .object(let dict) = schema else {
            Issue.record("schema is not an object")
            return
        }
        #expect(dict["type"] == .string("object"))
        #expect(dict["additionalProperties"] == .bool(false))
        #expect(dict["required"] == .array([.string("a")]))
    }

    private func text(of content: [ToolContent]) -> String {
        content.compactMap(\.text).joined(separator: "\n")
    }
}
