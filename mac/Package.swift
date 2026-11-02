// swift-tools-version: 6.0
// Jarvis Link: the menu bar app around jarvis-link on a Mac (docs/link.md). scripts/app/mac-link.sh builds the .app.
import PackageDescription

let package = Package(
    name: "JarvisLink",
    platforms: [.macOS(.v15)],
    targets: [
        .executableTarget(name: "JarvisLink", path: "Sources/JarvisLink")
    ],
    swiftLanguageModes: [.v5]
)
