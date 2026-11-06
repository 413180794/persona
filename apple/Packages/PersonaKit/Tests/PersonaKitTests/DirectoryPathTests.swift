import Testing
@testable import PersonaKit

/// The new chat's directory browser: a typed path goes where it says, and a directory the hub can't list says why
/// (#1402: an unreadable directory used to do nothing when tapped).
struct DirectoryPathTests {
    @Test func absoluteAndHomePathsGoAsTyped() {
        #expect(DirectoryPath.resolve("/srv/work", from: "/home/me") == "/srv/work")
        #expect(DirectoryPath.resolve("~", from: "/home/me/code") == "~")
        #expect(DirectoryPath.resolve("~/eval/run1", from: "/srv") == "~/eval/run1")
        #expect(DirectoryPath.resolve("  /srv/work \n", from: "/home/me") == "/srv/work")
    }

    @Test func otherPathsAreUnderTheCurrentOne() {
        #expect(DirectoryPath.resolve("run1/work", from: "/home/me/eval") == "/home/me/eval/run1/work")
        #expect(DirectoryPath.resolve("run1", from: "/") == "/run1")
        #expect(DirectoryPath.resolve("run1", from: "") == "~/run1")
    }

    @Test func nothingTypedGoesNowhere() {
        #expect(DirectoryPath.resolve("", from: "/home/me") == nil)
        #expect(DirectoryPath.resolve("   ", from: "/home/me") == nil)
    }

    @Test func theHubsErrorsInWords() {
        let denied = DirectoryPath.problem("open /home/me/eval: permission denied", path: "/home/me/eval")
        // `swift test` has no app catalog, so these come back as their English development keys (#5758).
        #expect(denied.contains("No permission to list the folders in “eval”"))
        #expect(denied.contains("type the full path above"))
        #expect(DirectoryPath.problem("open /x/y: no such file or directory", path: "/x/y") == "“/x/y” doesn't exist")
        #expect(DirectoryPath.problem("readdirent /x/f: not a directory", path: "/x/f") == "“/x/f” isn't a folder")
        #expect(DirectoryPath.problem("machine mac-a is offline", path: "/x") == "machine mac-a is offline")
        #expect(DirectoryPath.problem("", path: "/x") == "Can't open “/x”")
    }
}
