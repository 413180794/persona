#!/usr/bin/env python3
"""Replay the unchanged Foundation catalog predicate from a clean baseline."""
import argparse
import os
from pathlib import Path
import re
import subprocess
import tempfile

parser = argparse.ArgumentParser()
parser.add_argument("root", type=Path, help="Clean baseline worktree, not the feature worktree")
args = parser.parse_args()
root = args.root.resolve()
if subprocess.check_output(["git", "-C", str(root), "status", "--porcelain"], text=True):
    raise SystemExit("Baseline worktree must be clean")
source = (root / "apple/PersonaTests/StateNamesTests.swift").read_text()
constants = source[source.index("    private static let suffix"):source.index("    /// The catalog")]

def method(pattern):
    found = re.search(pattern + r".*?^    }", source, re.M | re.S)
    if found is None:
        raise SystemExit("Baseline method shape changed; inspect it before adapting the replay")
    return found.group(0)

reader = method(r"    private static func zh\(\) throws -> \[String: String\] \{")
old = '        let strings = try #require((try JSONSerialization.jsonObject(with: data) as? [String: Any])?["strings"] as? [String: Any])'
replacement = '        guard let strings = (try JSONSerialization.jsonObject(with: data) as? [String: Any])?["strings"] as? [String: Any] else { throw ScanError.invalidCatalog }'
if old not in reader:
    raise SystemExit("Baseline unwrap changed")
reader = reader.replace(old, replacement)
scan = method(r"    @Test func areDefinedOnce\(\) throws \{").replace("    @Test func areDefinedOnce()", "    func scan()")
old = '        #expect(again.isEmpty, "take these from StateName (Shared/StateNames.swift):\\n\\(again.joined(separator: "\\n"))")'
if old not in scan:
    raise SystemExit("Baseline assertion changed")
scan = scan.replace(old, '        print(again.joined(separator: "\\n"))\n        exit(again.isEmpty ? 0 : 1)')
# Keep all constants, catalog extraction and the scan predicate unchanged. Only XCTest wrappers are replaced.
swift = ('import Foundation\nimport Darwin\nenum ScanError: Error { case invalidCatalog }\n'
         'struct StateNamesBaseline {\n    private static let apple = URL(fileURLWithPath: CommandLine.arguments[1])\n'
         + constants + reader + '\n' + scan + '\n}\ntry StateNamesBaseline().scan()\n')
os.umask(0o077)
with tempfile.TemporaryDirectory(prefix="persona-state-baseline-") as folder:
    folder = Path(folder)
    file, binary = folder / "scan.swift", folder / "scan"
    file.write_text(swift)
    subprocess.run(["xcrun", "swiftc", str(file), "-o", str(binary)], check=True, timeout=90)
    result = subprocess.run([str(binary), str(root / "apple")], timeout=30)
if subprocess.check_output(["git", "-C", str(root), "status", "--porcelain"], text=True):
    raise SystemExit("Baseline worktree was modified")
raise SystemExit(result.returncode)
