#!/usr/bin/env python3
"""Run the current Apple HTML sandbox's bounded Mac probes without activating a window."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import time

REPO = Path(__file__).resolve().parents[2]


def replace_once(text, old, new):
    if text.count(old) != 1:
        raise RuntimeError("The Apple harness changed; review the Mac adaptation before running")
    return text.replace(old, new, 1)


def prepare(output):
    output.mkdir(mode=0o700)
    source_path = REPO / "apple/Persona/Views/Chat/AssistantHTMLCard.swift"
    tests_path = REPO / "apple/PersonaTests/AssistantHTMLSecurityTests.swift"
    source, tests = source_path.read_text(), tests_path.read_text()
    ranges = [(source.index("/// What a card's page asked"), source.index("/// One native confirmation")),
              (source.index("// MARK: - The sandbox"), source.index("#if os(iOS) || os(visionOS)\nstruct SandboxedHTMLView"))]
    extracted = "import SwiftUI\nimport WebKit\n\n" + "".join(source[a:b] for a, b in ranges)
    # A shared Mac login session cannot be externally destroyed like a disposable simulator/VM.
    # Remove the opt-in unbounded probe entirely rather than letting an inherited env var enable it.
    a = tests.index("    /// Run only as a separate test-without-building command")
    b = tests.index("    // MARK: Bounded, public-API WebView harness", a)
    tests = tests[:a] + tests[b:]
    tests = tests.replace("import UIKit\n", "import AppKit\n").replace("@testable import Persona", "@testable import Sandbox")
    tests = replace_once(tests, "#expect(card.root.presentedViewController == nil)", "#expect(card.window.attachedSheet == nil)")
    tests = replace_once(tests, "        let root = UIViewController()\n        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 320, height: 480))",
                         "        let root = NSViewController()\n        let window: NSWindow")
    tests = replace_once(tests, "        init(sandboxed: Bool = true) {\n            if sandboxed {", """        init(sandboxed: Bool = true) {
            let app = NSApplication.shared
            app.setActivationPolicy(.prohibited)
            #expect(app.activationPolicy() == .prohibited && !app.isActive)
            window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 320, height: 480),
                              styleMask: [.borderless], backing: .buffered, defer: false)
            window.isReleasedWhenClosed = false
            if sandboxed {""")
    tests = replace_once(tests, "            window.rootViewController = root\n            web.frame = window.bounds\n            root.view.addSubview(web)\n            window.isHidden = false", """            root.view = NSView(frame: window.contentLayoutRect)
            window.contentViewController = root
            web.frame = root.view.bounds
            root.view.addSubview(web)
            #expect(!window.isVisible && !window.isKeyWindow && !window.isMainWindow)
            #expect(!NSApplication.shared.isActive)""")
    tests = replace_once(tests, "            window.isHidden = true\n            window.rootViewController = nil", """            window.contentViewController = nil
            window.close()
            #expect(!NSApplication.shared.isActive)""")
    tests = replace_once(tests, "struct AssistantHTMLSecurityTests {", """struct AssistantHTMLSecurityTests {
    @Test(arguments: 0..<5) func idleListenerHasNoUnsolicitedConnections(_ iteration: Int) async throws {
        let server = try LocalHTTPServer()
        defer { server.stop() }
        _ = try await server.start()
        try await Task.sleep(for: .seconds(4))
        #expect(server.connections == 0, "an idle listener was reached by another process")
    }
""")
    tests = replace_once(tests, "@Test func externalScriptsFontsCSSImportsAndSVGResourcesStayOffline()", "@Test(arguments: 0..<5) func externalScriptsFontsCSSImportsAndSVGResourcesStayOffline(_ iteration: Int)")
    # Keep control traffic on a different port; cancelled media requests must not pollute the card count.
    tests = replace_once(tests, "        let before = server.connections\n\n        let card = Page()\n        defer { card.close() }\n        card.load(page)", """        try await Task.sleep(for: .milliseconds(200))
        let cardServer = try LocalHTTPServer()
        defer { cardServer.stop() }
        let cardPort = try await cardServer.start()
        let before = cardServer.connections

        let card = Page()
        defer { card.close() }
        card.load(page.replacingOccurrences(of: base + "/", with: "http://127.0.0.1:\\(cardPort)/"))""")
    tests = replace_once(tests, '#expect(server.connections == before, "an external resource escaped the sandbox")',
                         '#expect(cardServer.connections == before, "an external resource escaped the sandbox")')
    assert "while (true)" not in tests and ".activate(" not in tests and "orderFront" not in tests
    package = output / "Package"
    sources, probes = package / "Sources/Sandbox", package / "Tests/SandboxAcceptanceTests"
    sources.mkdir(parents=True); probes.mkdir(parents=True)
    (sources / "CurrentSandbox.swift").write_text(extracted)
    (probes / "AssistantHTMLSecurityTests.swift").write_text(tests)
    (probes / "MacHTMLNetworkTests.swift").write_bytes((REPO / "scripts/test/fixtures/MacHTMLNetworkTests.swift").read_bytes())
    (package / "Package.swift").write_text('''// swift-tools-version: 6.2
import PackageDescription
let package = Package(name: "MacHTMLSandboxAcceptance", platforms: [.macOS(.v14)],
    targets: [.target(name: "Sandbox", swiftSettings: [.defaultIsolation(MainActor.self)]),
              .testTarget(name: "SandboxAcceptanceTests", dependencies: ["Sandbox"])])
''')
    manifest = {"commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=REPO, text=True).strip(),
                "source": str(source_path.relative_to(REPO)), "source_sha256": hashlib.sha256(source.encode()).hexdigest(),
                "test_sha256": hashlib.sha256(tests_path.read_bytes()).hexdigest(),
                "adapted_tests_sha256": hashlib.sha256(tests.encode()).hexdigest(),
                "network_tests_sha256": hashlib.sha256((probes / "MacHTMLNetworkTests.swift").read_bytes()).hexdigest(),
                "extracted_sha256": hashlib.sha256(extracted.encode()).hexdigest(),
                "unchanged_ranges": [{"first": source[:a].count("\n") + 1, "last": source[:b].count("\n"),
                                       "sha256": hashlib.sha256(source[a:b].encode()).hexdigest()} for a, b in ranges],
                "literal_loop": "not run: requires an externally disposable Mac VM/login session",
                "os_termination": "not run: delegate callback is fault injection only",
                "focus": "prohibited activation policy; hidden windows; no ordering"}
    (output / "source-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    return package


def execute(command, output, label, env, timeout):
    with (output / (label + ".log")).open("wb") as log:
        proc = subprocess.Popen(command, cwd=output, env=env, stdout=log, stderr=subprocess.STDOUT,
                                start_new_session=True)
        (output / (label + "-process.json")).write_text(json.dumps({"pid": proc.pid, "pgid": proc.pid, "command": command}))
        started = None
        queue_deadline = time.monotonic() + 3600
        while proc.poll() is None:
            if started is None:
                # The slot wrapper and lockf are not the compiler/test host. Start its budget only after Swift starts.
                listing = subprocess.check_output(["/bin/ps", "-axo", "pgid=,comm="], text=True)
                for line in listing.splitlines():
                    fields = line.strip().split(None, 1)
                    if len(fields) == 2 and fields[0] == str(proc.pid) and Path(fields[1]).name.startswith("swift"):
                        started = time.monotonic()
                        break
            deadline = started + timeout if started is not None else queue_deadline
            if time.monotonic() >= deadline:
                os.killpg(proc.pid, signal.SIGTERM)
                try:
                    proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(proc.pid, signal.SIGKILL)
                    proc.wait(timeout=5)
                code = 124 if started is not None else 125
                break
            time.sleep(1)
        else:
            code = proc.returncode
    print(f"RESULT {label}: exit={code}", flush=True)
    return code


def write_status(path, value):
    temp = path.with_suffix(".tmp")
    temp.write_text(json.dumps(value))
    temp.replace(path)


def detached_test(command, output, env):
    status = output / "test-status.json"
    pid = os.fork()
    if pid == 0:
        os.setsid()
        if os.fork() != 0:
            os._exit(0)
        deadline = time.monotonic() + 5
        while os.getppid() != 1 and time.monotonic() < deadline:
            time.sleep(0.05)
        if os.getppid() != 1:
            write_status(status, {"exit": 2, "result": "worker not reparented"})
            os._exit(2)
        (output / "worker.json").write_text(json.dumps({"pid": os.getpid(), "ppid": os.getppid()}))
        try:
            code = execute(command, output, "test", env, 1200)
            write_status(status, {"exit": code, "result": "passed" if code == 0 else "failed"})
        except Exception as error:
            write_status(status, {"exit": 2, "result": str(error)})
        os._exit(0)
    os.waitpid(pid, 0)
    deadline = time.monotonic() + 4820
    while time.monotonic() < deadline:
        if status.exists():
            return json.loads(status.read_text())["exit"]
        time.sleep(0.5)
    # Both owned sessions are recorded: don't kill only the outer driver and leave the test group running.
    record = output / "test-process.json"
    if record.exists():
        pgid = json.loads(record.read_text())["pgid"]
        try:
            os.killpg(pgid, signal.SIGTERM)
            time.sleep(5)
            os.killpg(pgid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    print("RESULT detached test: TIMEOUT", flush=True)
    return 124


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True, help="a new directory; never overwrites evidence")
    args = parser.parse_args()
    os.umask(0o077)
    output = args.output.resolve()
    package = prepare(output)
    env = os.environ.copy()
    for key in list(env):
        if key.startswith(("PERSONA_")):
            env.pop(key)
    env.update(HOME=str(output / "home"), CFFIXED_USER_HOME=str(output / "home"),
               XDG_CACHE_HOME=str(output / "cache"), SWIFTPM_MODULECACHE_OVERRIDE=str(output / "cache/modules"))
    (output / "home").mkdir(); (output / "cache").mkdir()
    swift = subprocess.check_output(["xcrun", "--find", "swift"], env=env, text=True).strip()
    slot = str(Path.home() / ".local/bin/xcode-slot")
    common = ["--package-path", str(package), "--jobs", "2"]
    code = execute([slot, swift, "build", "--build-tests"] + common, output, "build", env, 1800)
    if code == 0:
        code = detached_test([slot, swift, "test", "--skip-build", "--no-parallel"] + common +
                             ["--filter", "AssistantHTMLSecurityTests|MacHTMLNetworkTests", "--xunit-output", str(output / "results.xml")], output, env)
    print(f"RESULT Mac HTML bounded acceptance: exit={code}; evidence={output}", flush=True)
    return code


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print(f"RESULT Mac HTML bounded acceptance: FAILED {error}", flush=True)
        raise SystemExit(2)
