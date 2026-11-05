import Foundation
import Observation
import SwiftUI

/// `@AppStorage` that redraws its view only when its own key's value changes (#1337).
///
/// `@AppStorage` invalidates its view on any write to its UserDefaults, whatever the key: on the Mac a window being
/// resized writes the split view's column frames (`NSSplitView Subview Frames …`) on every frame, and opening a chat
/// writes `dev.openChat`; every view holding an `@AppStorage` (the shell, the Agents list with its 137 rows diffed
/// again) recomputed each time — about half the main thread while resizing. Same use as `@AppStorage`:
/// `@StoredValue("dev.compact") private var compact = false`, `$compact` for a binding.
@MainActor @propertyWrapper
struct StoredValue<Value: Equatable> {
    private let box: StoredValueBox<Value>

    var wrappedValue: Value {
        get { box.value }
        nonmutating set { box.set(newValue) }
    }

    var projectedValue: Binding<Value> {
        let box = box
        return Binding(get: { box.value }, set: { box.set($0) })
    }

    private init(key: String, store: UserDefaults, read: @escaping (UserDefaults) -> Value, write: @escaping (UserDefaults, Value) -> Void) {
        box = StoredValueBox.shared(key: key, store: store, read: read, write: write)
    }
}

// Booleans and numbers read as `@AppStorage` reads them, with `bool(forKey:)` and the like: a launch argument's value
// is a string in the argument domain ("-dev.compact YES"), which `as? Bool` turned into the default (#2147).
extension StoredValue where Value == Bool {
    init(wrappedValue: Bool, _ key: String, store: UserDefaults = .standard) {
        self.init(key: key, store: store, read: { $0.object(forKey: key) == nil ? wrappedValue : $0.bool(forKey: key) },
                  write: { $0.set($1, forKey: key) })
    }
}

extension StoredValue where Value == Int {
    init(wrappedValue: Int, _ key: String, store: UserDefaults = .standard) {
        self.init(key: key, store: store, read: { $0.object(forKey: key) == nil ? wrappedValue : $0.integer(forKey: key) },
                  write: { $0.set($1, forKey: key) })
    }
}

extension StoredValue where Value == Double {
    init(wrappedValue: Double, _ key: String, store: UserDefaults = .standard) {
        self.init(key: key, store: store, read: { $0.object(forKey: key) == nil ? wrappedValue : $0.double(forKey: key) },
                  write: { $0.set($1, forKey: key) })
    }
}

extension StoredValue where Value == String {
    init(wrappedValue: String, _ key: String, store: UserDefaults = .standard) {
        self.init(key: key, store: store, read: { $0.string(forKey: key) ?? wrappedValue }, write: { $0.set($1, forKey: key) })
    }
}

extension StoredValue where Value == Data {
    init(wrappedValue: Data, _ key: String, store: UserDefaults = .standard) {
        self.init(key: key, store: store, read: { $0.data(forKey: key) ?? wrappedValue }, write: { $0.set($1, forKey: key) })
    }
}

extension StoredValue where Value: RawRepresentable, Value.RawValue == String {
    init(wrappedValue: Value, _ key: String, store: UserDefaults = .standard) {
        self.init(key: key, store: store, read: { $0.string(forKey: key).flatMap(Value.init(rawValue:)) ?? wrappedValue },
                  write: { $0.set($1.rawValue, forKey: key) })
    }
}

extension StoredValue where Value: RawRepresentable, Value.RawValue == Int {
    init(wrappedValue: Value, _ key: String, store: UserDefaults = .standard) {
        self.init(key: key, store: store,
                  read: { $0.object(forKey: key) == nil ? wrappedValue : Value(rawValue: $0.integer(forKey: key)) ?? wrappedValue },
                  write: { $0.set($1.rawValue, forKey: key) })
    }
}

/// One key's value, shared by every view that stores it: it changes (and so redraws its readers) only when a write
/// leaves the key with another value.
@MainActor @Observable
final class StoredValueBox<Value: Equatable> {
    private(set) var value: Value
    @ObservationIgnored private let store: UserDefaults
    @ObservationIgnored private let read: (UserDefaults) -> Value
    @ObservationIgnored private let write: (UserDefaults, Value) -> Void
    @ObservationIgnored private var token: (any NSObjectProtocol)?

    @ObservationIgnored private static var boxes: [String: AnyObject] { get { StoredValueBoxes.all } set { StoredValueBoxes.all = newValue } }

    static func shared(key: String, store: UserDefaults, read: @escaping (UserDefaults) -> Value,
                       write: @escaping (UserDefaults, Value) -> Void) -> StoredValueBox {
        let id = "\(ObjectIdentifier(store).hashValue)|\(key)|\(Value.self)"
        if let box = boxes[id] as? StoredValueBox { return box }
        let box = StoredValueBox(store: store, read: read, write: write)
        boxes[id] = box
        return box
    }

    private init(store: UserDefaults, read: @escaping (UserDefaults) -> Value, write: @escaping (UserDefaults, Value) -> Void) {
        self.store = store
        self.read = read
        self.write = write
        value = read(store)
        // Writes made elsewhere (an `@AppStorage` of the same key, a settings page): picked up when they change it.
        token = NotificationCenter.default.addObserver(forName: UserDefaults.didChangeNotification, object: store, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.refresh() }
        }
    }

    func set(_ new: Value) {
        write(store, new)
        if new != value { value = new }
    }

    private func refresh() {
        let now = read(store)
        if now != value { value = now }
    }
}

/// The boxes, by store, key and type (a generic class cannot hold a static stored property of its own).
@MainActor private enum StoredValueBoxes {
    static var all: [String: AnyObject] = [:]
}
