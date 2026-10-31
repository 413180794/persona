#if os(macOS)
import AppKit

// UIKit's menu model (`UIMenu`, `UIAction`, `UIMenuElement`) for the shared code that builds context menus
// (`CodeLineSource.menu(_:)`, link and message menus). Mac views turn it into an `NSMenu` with `nsMenu()`; each
// element also gives its own `nsMenuItem()`. Only the parts the shared code uses are modelled.

/// The base of `UIAction` and `UIMenu`, like UIKit's.
@MainActor class UIMenuElement: NSObject {
    enum State: Sendable { case off, on, mixed }

    struct Attributes: OptionSet, Sendable {
        let rawValue: UInt
        init(rawValue: UInt) { self.rawValue = rawValue }
        static let disabled = Attributes(rawValue: 1 << 0)
        static let destructive = Attributes(rawValue: 1 << 1)
        static let hidden = Attributes(rawValue: 1 << 2)
        static let keepsMenuPresented = Attributes(rawValue: 1 << 3)
    }

    var title: String
    var subtitle: String?
    var image: UIImage?

    init(title: String, subtitle: String? = nil, image: UIImage?) {
        self.title = title
        self.subtitle = subtitle
        self.image = image
    }

    /// One menu item (a submenu for a `UIMenu`).
    func nsMenuItem() -> NSMenuItem { NSMenuItem(title: title, action: nil, keyEquivalent: "") }

    /// The items this element puts into a parent menu: one, or an inline menu's children between separators.
    func nsMenuItems() -> [NSMenuItem] { [nsMenuItem()] }
}

/// `UIAction`: a titled item with an optional SF Symbol image that calls its handler when chosen.
@MainActor final class UIAction: UIMenuElement {
    struct Identifier: Hashable, RawRepresentable, Sendable {
        let rawValue: String
        init(rawValue: String) { self.rawValue = rawValue }
        init(_ rawValue: String) { self.rawValue = rawValue }
    }

    let identifier: Identifier
    var discoverabilityTitle: String?
    var attributes: Attributes
    var state: State
    private let handler: (UIAction) -> Void

    init(title: String = "", subtitle: String? = nil, image: UIImage? = nil, identifier: Identifier? = nil,
         discoverabilityTitle: String? = nil, attributes: Attributes = [], state: State = .off,
         handler: @escaping (UIAction) -> Void) {
        self.identifier = identifier ?? Identifier(UUID().uuidString)
        self.discoverabilityTitle = discoverabilityTitle
        self.attributes = attributes
        self.state = state
        self.handler = handler
        super.init(title: title, subtitle: subtitle, image: image)
    }

    /// Runs the handler, as choosing the item does.
    func perform() { handler(self) }

    @objc private func chosen(_ sender: NSMenuItem) { perform() }

    override func nsMenuItem() -> NSMenuItem {
        let item = NSMenuItem(title: title, action: #selector(chosen(_:)), keyEquivalent: "")
        // NSMenuItem's target is weak: the item keeps the action alive through representedObject.
        item.target = self
        item.representedObject = self
        item.image = image
        item.subtitle = subtitle
        item.toolTip = discoverabilityTitle
        item.isEnabled = !attributes.contains(.disabled)
        item.isHidden = attributes.contains(.hidden)
        item.state = switch state {
        case .off: .off
        case .on: .on
        case .mixed: .mixed
        }
        if attributes.contains(.destructive) {
            item.attributedTitle = NSAttributedString(string: title, attributes: [.foregroundColor: NSColor.systemRed])
        }
        return item
    }
}

/// `UIMenu`: a titled group of elements. Inline menus (`.displayInline`) appear as a section of their parent;
/// the others as a submenu.
@MainActor final class UIMenu: UIMenuElement {
    struct Identifier: Hashable, RawRepresentable, Sendable {
        let rawValue: String
        init(rawValue: String) { self.rawValue = rawValue }
        init(_ rawValue: String) { self.rawValue = rawValue }
    }

    struct Options: OptionSet, Sendable {
        let rawValue: UInt
        init(rawValue: UInt) { self.rawValue = rawValue }
        static let displayInline = Options(rawValue: 1 << 0)
        static let destructive = Options(rawValue: 1 << 1)
        static let singleSelection = Options(rawValue: 1 << 5)
        static let displayAsPalette = Options(rawValue: 1 << 7)
    }

    let identifier: Identifier
    let options: Options
    private(set) var children: [UIMenuElement]

    init(title: String = "", subtitle: String? = nil, image: UIImage? = nil, identifier: Identifier? = nil,
         options: Options = [], children: [UIMenuElement] = []) {
        self.identifier = identifier ?? Identifier(UUID().uuidString)
        self.options = options
        self.children = children
        super.init(title: title, subtitle: subtitle, image: image)
    }

    func replacingChildren(_ newChildren: [UIMenuElement]) -> UIMenu {
        UIMenu(title: title, subtitle: subtitle, image: image, identifier: identifier, options: options, children: newChildren)
    }

    /// The whole menu as an `NSMenu`, for `NSView.menu(for:)` or `NSMenu.popUpContextMenu`. A title becomes the
    /// menu's section header (context menus on a Mac show no title of their own).
    func nsMenu() -> NSMenu {
        let menu = NSMenu(title: title)
        menu.autoenablesItems = false // keep `.disabled` as given
        if !title.isEmpty { menu.addItem(.sectionHeader(title: title)) }
        for item in childItems() { menu.addItem(item) }
        return menu
    }

    override func nsMenuItem() -> NSMenuItem {
        let item = NSMenuItem(title: title, action: nil, keyEquivalent: "")
        item.image = image
        item.subtitle = subtitle
        let sub = NSMenu(title: title)
        sub.autoenablesItems = false
        for child in childItems() { sub.addItem(child) }
        item.submenu = sub
        return item
    }

    override func nsMenuItems() -> [NSMenuItem] {
        guard options.contains(.displayInline) else { return [nsMenuItem()] }
        var items: [NSMenuItem] = [.separator()]
        if !title.isEmpty { items.append(.sectionHeader(title: title)) }
        items += childItems()
        items.append(.separator())
        return items
    }

    /// The children's items, without separators at the ends or two in a row.
    private func childItems() -> [NSMenuItem] {
        var out: [NSMenuItem] = []
        for item in children.flatMap({ $0.nsMenuItems() }) {
            if item.isSeparatorItem, out.isEmpty || out.last?.isSeparatorItem == true { continue }
            out.append(item)
        }
        while out.last?.isSeparatorItem == true { out.removeLast() }
        return out
    }
}
#endif
