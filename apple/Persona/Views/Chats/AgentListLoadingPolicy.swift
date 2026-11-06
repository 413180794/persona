import Foundation

/// A bounded window cannot implement global preference ordering or sticky/local membership. Until the hub exposes
/// those contracts, retain the complete legacy collection rather than filtering a partial window and losing rows.
nonisolated enum AgentListLoadingPolicy {
    static func runtime(grouping: DevGrouping, filter: AgentFilter, query: String, hasPins: Bool, hasMutes: Bool) -> Bool {
        grouping != .recent || filter != .all || !query.isEmpty || hasPins || hasMutes
    }

    static func personal(_ prefs: PersonalAgentPrefs, query: String) -> Bool {
        !query.isEmpty || !prefs.pins.isEmpty || !prefs.sections.isEmpty || !prefs.assign.isEmpty ||
            prefs.view.sortBy != .updated || prefs.view.showMode != .all || prefs.view.providerFilter.count > 1
    }
}
