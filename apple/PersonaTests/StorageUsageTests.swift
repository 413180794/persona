import Testing
@testable import Persona

/// Settings › Storage: which caches folder counts as which kind.
struct StorageUsageTests {
    /// ChatCache's folder goes up a version whenever its items change; every version is the Agents' chats.
    @Test func everyChatCacheVersionIsAgentChats() {
        for name in ["chats", "chats-v2", "chats-v3", "chats-v9", "chats-v12", ChatCache.folder] {
            #expect(StorageUsage.kind(cachesEntry: name) == .agentChats, "\(name)")
        }
    }

    /// Only "chats-v" followed by digits is a ChatCache folder.
    @Test func lookalikesAreNotAgentChats() {
        for name in ["chats-v", "chats-vx", "chats-v3.bak", "chatsv3", "other"] {
            #expect(StorageUsage.kind(cachesEntry: name) != .agentChats, "\(name)")
        }
    }

    @Test func otherFolders() {
        #expect(StorageUsage.kind(cachesEntry: "chat-images") == .chatImages)
        #expect(StorageUsage.kind(cachesEntry: "lark-media") == .feishuMedia)
        #expect(StorageUsage.kind(cachesEntry: "state") == .pageState)
        #expect(StorageUsage.kind(cachesEntry: "WebKit") == .web)
        #expect(StorageUsage.kind(cachesEntry: "com.apple.WebKit.Networking") == .web)
    }
}
