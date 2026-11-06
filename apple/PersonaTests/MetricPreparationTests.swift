import Foundation
import SwiftUI
import Testing
@testable import Jarvis

struct MetricPreparationTests {
    @Test @MainActor func metricsEndpointPreservesTheRequestedRange() async throws {
        let hub = FakeHub()
        let path = "/api/v1/machines/m1/metrics"
        hub.on("GET", path, json: #"{"metrics":{"machine_id":"m1","online":true,"points":[{"t":0,"cpu":42}]}}"#)
        let result = try await hub.client.machineMetrics("m1", range: .day)
        #expect(result.machineID == "m1")
        #expect(result.points.first?.cpu == 42)
        #expect(hub.sent("GET", path).last?.query == ["range": "24h"])
    }

    @Test @MainActor func cancelledMetricsRequestsDoNotReturnDecodedData() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/machines/m1/metrics", json: #"{"metrics":{"machine_id":"m1","online":true,"points":[]}}"#)
        let task = Task { try await hub.client.machineMetrics("m1", range: .hour) }
        task.cancel()
        do {
            _ = try await task.value
            Issue.record("cancelled metrics request returned data")
        } catch {
            #expect(error.isCancellation)
        }
    }

    @Test @MainActor func preparedValuesMatchLegacyIncludingGapsAndMissingValues() {
        let points: [MetricPoint] = [
            .init(t: 0, cpu: 10, load: 1, mem: 20, rx: 5, tx: 2),
            .init(t: 10, cpu: 90, load: 3, mem: 80, rx: nil, tx: 7),
            .init(t: 20, gap: true), .init(t: 30, cpu: 40, load: nil, mem: nil, rx: 9, tx: 1),
        ]
        let keys: [MetricSeriesKey] = [.cpu, .memory(total: 100), .memory(total: 0), .down, .up, .load]
        for key in keys {
            let series = ChartSeries(name: "sample", color: .blue, key: key) { key.value($0) }
            let prepared = PreparedMetricSeries.build(points, key: key, name: series.name)
            let legacy = points.chartValues(series)
            #expect(prepared.values == legacy)
            #expect(prepared.peak == legacy.map(\.value).max())
            #expect(prepared.values.allSatisfy { $0.id == "\($0.series)@\($0.date.timeIntervalSince1970)" })
        }
    }

    nonisolated private static func prepareOnCallingThread() -> (isMain: Bool, series: PreparedMetricSeries) {
        (Thread.isMainThread, PreparedMetricSeries.build([MetricPoint(t: 0, cpu: 42)], key: .cpu, name: "CPU"))
    }

    @Test @MainActor func preparationRunsWithoutTheMainActor() async {
        let result = await Task.detached { Self.prepareOnCallingThread() }.value
        #expect(!result.isMain)
        #expect(result.series.peak == 42)
    }

    @Test @MainActor func chartEqualityIncludesDisplayedInputsAndSelectionSnapshot() {
        let points = [MetricPoint(t: 0, cpu: 1), MetricPoint(t: 10, cpu: 2)]
        let values = PreparedMetricSeries.build(points, key: .cpu, name: "CPU")
        let base = MetricChart(title: "CPU", points: points,
                               series: [ChartSeries(name: "CPU", color: .blue, key: .cpu) { $0.cpu }],
                               domain: Date(timeIntervalSince1970: 0)...Date(timeIntervalSince1970: 10),
                               step: 10, yDomain: 0...100, format: { MetricFormat.percent($0) }, now: [2],
                               prepared: [values], formatKey: "percent", selectionSnapshot: nil, selection: .constant(nil))
        #expect(base == base)
        var changed = base
        changed.now = [3]
        #expect(changed != base)
        changed = base
        changed.selectionSnapshot = Date(timeIntervalSince1970: 5)
        #expect(changed != base)
        changed = base
        changed.domain = Date(timeIntervalSince1970: 1)...Date(timeIntervalSince1970: 11)
        #expect(changed != base)
        changed = base
        changed.series[0].color = .red
        #expect(changed != base)
        changed = base
        changed.series[0].key = .load
        #expect(changed != base)
        changed = base
        changed.formatKey = nil
        #expect(changed != base)
    }
}
