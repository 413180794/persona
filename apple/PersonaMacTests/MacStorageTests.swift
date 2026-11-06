import Testing
@testable import Persona

/// Settings › Storage on a Mac build without a sandbox (an ad-hoc debug build): ~/Library/Caches is every app's, so only
/// Persona's own folders are counted and cleared there; in a sandbox the whole folder is the app's.
struct MacStorageTests {
    @Test func outsideASandboxOnlyPersonaFolders() {
        // chats-v<N>: ChatCache's folder goes up a version whenever its items change (chats-v3 today).
        for name in ["chats-v2", "chats-v3", "chats-v9", "chats", "chat-images", "lark-media", "state", "me.example.persona"] {
            #expect(StorageUsage.owned(cachesEntry: name, sandboxed: false, bundleID: "me.example.persona"), "\(name)")
        }
        for name in ["com.apple.Safari", "Homebrew", "com.apple.WebKit.Networking", "pip"] {
            #expect(!StorageUsage.owned(cachesEntry: name, sandboxed: false, bundleID: "me.example.persona"), "\(name)")
        }
    }

    /// $TMPDIR outside a sandbox is the user's: other programs' lark- folders could be there, so none is touched.
    @Test func outsideASandboxNoTempFiles() {
        #expect(!StorageUsage.owned(tempEntry: "lark-abc", sandboxed: false))
        #expect(StorageUsage.owned(tempEntry: "lark-abc", sandboxed: true))
    }

    /// ChatCache's folder (whatever version it is at) counts under 「Agent 对话」, not 「其他缓存」.
    @Test func chatCacheFolderIsAgentChats() {
        #expect(StorageUsage.owned(cachesEntry: ChatCache.folder, sandboxed: false, bundleID: "me.example.persona"))
        for name in [ChatCache.folder, "chats-v2", "chats-v3", "chats-v9", "chats"] {
            #expect(StorageUsage.kind(cachesEntry: name) == .agentChats, "\(name)")
        }
        // Lookalikes stay another program's outside a sandbox.
        for name in ["chats-v", "chats-vx", "chats-v3.bak"] {
            #expect(!StorageUsage.owned(cachesEntry: name, sandboxed: false, bundleID: "me.example.persona"), "\(name)")
        }
    }

    @Test func inASandboxEverythingIsTheApps() {
        #expect(StorageUsage.owned(cachesEntry: "Homebrew", sandboxed: true, bundleID: "me.example.persona"))
        #expect(StorageUsage.owned(cachesEntry: "com.apple.WebKit.Networking", sandboxed: true, bundleID: "me.example.persona"))
    }
}
