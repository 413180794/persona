# Source anonymization

## Preserve contracts and syntax while removing private data

A scanner match is evidence to inspect, not an instruction to replace an arbitrary substring. Credential-like words also occur in serialization keys, RPC and enum values, environment-variable names, localization messages, and published signature-verification pins. Preserve those contracts; distinguish public verification material from private keys.

- Verify contextual exceptions against the original objects. A test directory alone does not establish that a token is synthetic.
- Replace unproven credentials and their asserted excerpts together. Keep recognizer grammar, distinct identities, source prefixes, automated-sender roles, and address families covered by the tests.
- Preserve JSON escaping and validate each JSON/JSONL record. Never regenerate a golden from malformed input that a parser silently skipped.
- Check resource keys and escaped Unicode as well as values when removing an alphabet; replacing only its first word can leave the private remainder intact. Historical identifiers are not alphabets and should not become invalid syntax.
- Distinguish comparisons from assignments in generated bundles and test the bundled runtime, not only the source. An unrelated public constant elsewhere in a file never exempts a matched secret.
- Decode XMP and image descriptions before scanning. Scan archive members and SQLite cells, and state whether video frames or only metadata were covered.
- Generate TLS test private keys at runtime rather than embedding them or deleting security coverage.
- Removing a presentation codec must not alter transport encryption, authorization headers, persisted identities, or unrelated messaging capabilities.
- Verify retained commit trees against deterministic transformations and carry every file operation when squashing commits. Compilation and behavioral tests remain separate checks.
- Use a short isolated test home for Unix sockets, a dedicated database, and bounded processes. Review logs only after redacting complete secrets and excerpts, including raw string literals.
- Keep backups and audit inputs private. Do not create helper worktrees in a repository that must remain untouched; tool-level isolation can still modify its Git metadata.

## Prove the role of a suspicious fixture value

A hex string may be a public cryptographic test vector, and a token-shaped string may be a deterministic filename rather than a credential. Trace the value to its assertion before replacing it. Preserve published AES compliance vectors; if another fixture reuses one as a device token, give that fixture a separate constructed value without changing the compliance tests. For upload names, reproduce the input sanitization, extension splitting, and fixed timestamp before classifying the expected filename.

Raw SQLite bytes can concatenate a reserved example address with the next field or record header and resemble a different email. Deserialize the database, inspect complete cells, and regenerate it in an isolated directory. Compare every decoded cell and run `integrity_check`; different physical database hashes do not imply different logical content, and a binary-path exemption alone proves nothing.

Transcript anonymization must preserve byte lengths when parser snapshots include full-size counters or truncated text. Keep replacement filenames the same UTF-8 byte length and extension, preserve JSON newline escapes, and replace corresponding input and golden strings together. Compare decoded structures and actual parser output, including incremental and paging behavior. Do not use the golden-update flag to hide a changed counter, truncation boundary, or silently dropped record.

## Inspect metadata provenance, not only known-pattern matches

A zero regex count does not establish the provenance of an opaque XMP creator or export identifier. Inspect author/source fields as well as decoding their packets. When unnecessary PNG text metadata has no established public provenance, remove it rather than infer that an asset identifier cannot link to a private account.

Strip `tEXt`, `zTXt`, and `iTXt` chunks without re-encoding the image. Validate chunk checksums and compare every retained chunk byte-for-byte, including `IDAT`, color profiles, gamma and pixel density; also compare decoded RGBA pixels. This keeps the icon's appearance unchanged. Update current assets through the source PR and remove the same metadata from all matching historical blobs before claiming synchronized clean-history publication.
