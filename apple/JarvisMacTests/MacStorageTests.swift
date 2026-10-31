import Testing
@testable import Jarvis

/// 设置 › 存储空间 on a Mac build without a sandbox (an ad-hoc debug build): ~/Library/Caches is every app's, so only
/// Jarvis's own folders are counted and cleared there; in a sandbox the whole folder is the app's.
struct MacStorageTests {
    @Test func outsideASandboxOnlyJarvisFolders() {
        for name in ["chats-v2", "chats", "chat-images", "lark-media", "state", "me.example.jarvis"] {
            #expect(StorageUsage.owned(cachesEntry: name, sandboxed: false, bundleID: "me.example.jarvis"), "\(name)")
        }
        for name in ["com.apple.Safari", "Homebrew", "com.apple.WebKit.Networking", "pip"] {
            #expect(!StorageUsage.owned(cachesEntry: name, sandboxed: false, bundleID: "me.example.jarvis"), "\(name)")
        }
    }

    @Test func inASandboxEverythingIsTheApps() {
        #expect(StorageUsage.owned(cachesEntry: "Homebrew", sandboxed: true, bundleID: "me.example.jarvis"))
        #expect(StorageUsage.owned(cachesEntry: "com.apple.WebKit.Networking", sandboxed: true, bundleID: "me.example.jarvis"))
    }
}
