import Foundation
import HealthKit

/// `phone_health_summary`: per-day aggregates, one HealthKit collection query
/// per metric.
enum HealthTool {
    /// The only types the app ever asks for. Read-only: the app never writes
    /// health data.
    static var readTypes: Set<HKObjectType> {
        var types: Set<HKObjectType> = []
        for identifier in quantityIdentifiers.values {
            if let type = HKQuantityType.quantityType(forIdentifier: identifier) { types.insert(type) }
        }
        if let sleep = HKCategoryType.categoryType(forIdentifier: .sleepAnalysis) { types.insert(sleep) }
        return types
    }

    private static let quantityIdentifiers: [String: HKQuantityTypeIdentifier] = [
        "steps": .stepCount,
        "distance": .distanceWalkingRunning,
        "active_energy": .activeEnergyBurned,
        "heart_rate": .heartRate,
        "resting_heart_rate": .restingHeartRate,
        "weight": .bodyMass,
    ]

    private static var metricNames: [String] { Array(quantityIdentifiers.keys) + ["sleep"] }

    static var tools: [PhoneTool] {
        [
            PhoneTool(
                spec: ToolSpec(
                    name: "phone_health_summary",
                    description: """
                    Summarize the user's health data per day. Metrics: steps, distance (meters), \
                    active_energy (kcal), heart_rate (bpm, average/min/max over the day), \
                    resting_heart_rate (bpm), sleep (hours asleep), weight (kg). Defaults to steps \
                    over the last 7 days. Only the requested metrics are read. Days run from local \
                    midnight to midnight; today's numbers are partial. Returns one row per day with \
                    only the days that have data. Read access is never reported to apps, so an empty \
                    result can also mean the user denied read access.
                    """,
                    inputSchema: Schema.object([
                        "metrics": Schema.array(
                            "Metrics to summarize. Default [\"steps\"].",
                            of: Schema.string("One metric.", values: metricNames)
                        ),
                        "days": Schema.integer("How many days back from today, default 7, max 90.", min: 1, max: 90),
                    ])
                ),
                source: .health,
                run: { args in try await summary(args) }
            )
        ]
    }

    static func summary(_ args: JSONValue) async throws -> [ToolContent] {
        try HealthAccess.requireAccess()
        let days = ToolArgs.int(args, "days", fallback: 7, min: 1, max: 90)
        var requested: [String] = []
        if let raw = args["metrics"]?.array {
            for item in raw {
                guard let name = item.string else { continue }
                guard metricNames.contains(name) else {
                    throw ToolFailure(.invalidArgument, "unknown metric \"\(name)\"; use one of \(metricNames.joined(separator: ", "))")
                }
                if !requested.contains(name) { requested.append(name) }
            }
        }
        if requested.isEmpty { requested = ["steps"] }
        if requested.contains("weight") {
            // Body mass needs read access too; asking again is harmless and
            // covers people who authorized the app before weight was supported.
            _ = try? await HKHealthStore().requestAuthorization(toShare: Set<HKSampleType>(), read: readTypes)
        }

        let calendar = Calendar.current
        let end = Date()
        let start = calendar.startOfDay(for: calendar.date(byAdding: .day, value: -(days - 1), to: end) ?? end)

        let store = HKHealthStore()
        var byDay: [String: [String: JSONValue]] = [:]
        for metric in requested {
            let rows: [String: JSONValue]
            do {
                if metric == "sleep" {
                    rows = try await sleepByDay(store, start: start, end: end, calendar: calendar)
                } else {
                    rows = try await quantityByDay(store, metric: metric, start: start, end: end, calendar: calendar)
                }
            } catch {
                // One failed metric should not sink the whole summary.
                rows = [ToolDate.day(end): .object(["error": .string(error.localizedDescription)])]
            }
            for (day, value) in rows {
                byDay[day, default: [:]][metric] = value
            }
        }

        let sorted = byDay.keys.sorted()
        let rows: [JSONValue] = sorted.map { day in
            var row: [String: JSONValue] = ["date": .string(day)]
            for (metric, value) in byDay[day] ?? [:] { row[metric] = value }
            return .object(row)
        }
        return Output.json(Output.object([
            ("start", .string(ToolDate.string(start))),
            ("end", .string(ToolDate.string(end))),
            ("days", .number(Double(days))),
            ("metrics", .array(requested.map { .string($0) })),
            ("data", .array(rows)),
        ]))
    }

    // MARK: - Queries

    private static func quantityByDay(
        _ store: HKHealthStore,
        metric: String,
        start: Date,
        end: Date,
        calendar: Calendar
    ) async throws -> [String: JSONValue] {
        guard let identifier = quantityIdentifiers[metric],
              let type = HKQuantityType.quantityType(forIdentifier: identifier) else {
            throw ToolFailure(.invalidArgument, "this iPhone does not support the metric \"\(metric)\"")
        }
        let options: HKStatisticsOptions = metric == "heart_rate" ? [.discreteAverage, .discreteMin, .discreteMax] : .cumulativeSum
        let units: HKUnit = switch metric {
        case "steps": .count()
        case "distance": .meter()
        case "active_energy": .kilocalorie()
        case "weight": .gramUnit(with: .kilo)
        default: HKUnit.count().unitDivided(by: .minute())
        }

        let query = HKStatisticsCollectionQuery(
            quantityType: type,
            quantitySamplePredicate: HKQuery.predicateForSamples(withStart: start, end: end, options: .strictStartDate),
            options: options,
            anchorDate: start,
            intervalComponents: DateComponents(day: 1)
        )
        let collection: HKStatisticsCollection = try await withCheckedThrowingContinuation { continuation in
            let gate = SingleResume(continuation)
            query.initialResultsHandler = { _, collection, error in
                if let error {
                    gate.fail(error)
                } else if let collection {
                    gate.succeed(collection)
                } else {
                    gate.fail(ToolFailure(.unavailable, "HealthKit returned no statistics for \(metric)"))
                }
            }
            store.execute(query)
        }

        var out: [String: JSONValue] = [:]
        collection.enumerateStatistics(from: start, to: end) { statistics, _ in
            let day = ToolDate.day(statistics.startDate)
            switch metric {
            case "heart_rate":
                guard let average = statistics.averageQuantity()?.doubleValue(for: units) else { return }
                let minValue = statistics.minimumQuantity().map { $0.doubleValue(for: units) }
                let maxValue = statistics.maximumQuantity().map { $0.doubleValue(for: units) }
                out[day] = Output.object([
                    ("average", .number(round1(average))),
                    ("min", minValue.map { JSONValue.number(round1($0)) }),
                    ("max", maxValue.map { JSONValue.number(round1($0)) }),
                ])
            default:
                guard let sum = statistics.sumQuantity()?.doubleValue(for: units) else { return }
                out[day] = .number(round1(sum))
            }
        }
        return out
    }

    /// Sleep is a category type: sum every sample whose value counts as asleep,
    /// clipped to the day so a night that crosses midnight splits correctly.
    private static func sleepByDay(
        _ store: HKHealthStore,
        start: Date,
        end: Date,
        calendar: Calendar
    ) async throws -> [String: JSONValue] {
        guard let type = HKCategoryType.categoryType(forIdentifier: .sleepAnalysis) else {
            throw ToolFailure(.unavailable, "此设备不支持睡眠数据 / Sleep analysis is not available")
        }
        let predicate = HKQuery.predicateForSamples(withStart: start, end: end, options: [])
        let samples: [HKCategorySample] = try await withCheckedThrowingContinuation { continuation in
            let gate = SingleResume(continuation)
            let query = HKSampleQuery(sampleType: type, predicate: predicate, limit: HKObjectQueryNoLimit, sortDescriptors: nil) { _, result, error in
                if let error {
                    gate.fail(error)
                } else {
                    gate.succeed((result as? [HKCategorySample]) ?? [])
                }
            }
            store.execute(query)
        }

        let asleepValues = Set(HKCategoryValueSleepAnalysis.allAsleepValues.map(\.rawValue))
        var seconds: [String: TimeInterval] = [:]
        for sample in samples where asleepValues.contains(sample.value) {
            var cursor = max(sample.startDate, start)
            let sampleEnd = min(sample.endDate, end)
            while cursor < sampleEnd {
                let dayStart = calendar.startOfDay(for: cursor)
                let dayEnd = calendar.date(byAdding: .day, value: 1, to: dayStart) ?? sampleEnd
                let chunkEnd = min(dayEnd, sampleEnd)
                let day = ToolDate.day(dayStart)
                seconds[day, default: 0] += chunkEnd.timeIntervalSince(cursor)
                cursor = chunkEnd
            }
        }
        var out: [String: JSONValue] = [:]
        for (day, total) in seconds where total > 0 {
            out[day] = .number(round1(total / 3600))
        }
        return out
    }

    private static func round1(_ value: Double) -> Double {
        (value * 10).rounded() / 10
    }
}
