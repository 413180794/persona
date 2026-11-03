import SwiftUI

/// An empty item that holds the middle of the window's toolbar, without the system's glass (#830). A toolbar of only
/// `.primaryAction` buttons keeps them toward the middle, where at the narrowest window they cover the page's title at
/// the row's leading end; with the middle taken they sit by the search field.
struct MacToolbarMiddle: ToolbarContent {
    var body: some ToolbarContent {
        ToolbarItem(placement: .principal) { Color.clear.frame(width: Theme.Size.hairline) }
            .sharedBackgroundVisibility(.hidden)
    }
}
