# Lark encrypted-message migration (#689)

## Integrated: reading and display

The Lark gateway Agent API retains old `parts` / `textPreview`, adding a versioned `olk` display projection. Hub forwards it and rewrites media URLs; iOS distinguishes decrypted body, public display content, and group-key distribution notices. If decryption fails, show public content labeled "Encrypted message · Public display"; before cache revalidation, label "Encrypted message · Public content only," never presenting public display as the body. Ordinary text containing `加密群密钥分发消息` (encrypted group-key distribution message) is not mistaken for a system notice.

Uses existing vault and Agent plaintext-access authorization, no new decryption permission. If authorization is revoked or account changes, upstream omits plaintext display fields. Unknown / missing protocol versions never use plaintext fields. Missing / null public content becomes an empty array; an incomplete optional projection in one message does not block an entire ordinary-message page. Images still use upstream covers; encrypted-image keys / decrypting downloads are out of scope here.

Quoted messages carry the same display information. Chat / composer quotes label "Public display" or "Public content only" while locked, offline-cached, or restoring drafts, never treating public display as quoted original text. Copy uses only currently visible body; encrypted messages do not auto-translate. When iOS persists messages / quotes, encoding retains only public content, clearing old text fields, body parts, and plaintext inside structured projections. Reopening rechecks permission on the server before reading. **This protects only iOS message-snapshot and quote-draft encoding paths**, not a claim that all storage is plaintext-free. Lark Inbox sources and Agent context pipelines may still flatten old `textPreview` into persisted bodies (formerly hub lark*.go / lark_agent.go; since #1571 both paths live in the Lark extension, and open-source server code no longer reads `textPreview`). Audit other consumers separately. This applies to messages carrying new projections; encryption cannot be inferred from plain text already flattened by old servers.

## Validation

- After latest review fixes on 2026-11-03: 71 unit tests / 5 suites covering encryption, persistence, attachments, outbox, pagination passed. Includes locked / unauthorized / missing key / unknown version, no decrypted bodies in quote drafts or snapshots, explicit public-display quote labels. Image-only / emoji-only public-display quotes retain one status label, without repeating "Encrypted message."
- iPhone 18 Pro: real chat flows once in light Liquid Glass and dark BeautifulUI, inspecting bodies, quotes, public display, standalone / accompanying key notices, locked public content, and identical ordinary text. Light / dark screenshots inspected individually; added light quote / dark quote, with UI tests verifying public-content labels in chat / composer quotes.
- Xcode 26.0: iOS (Watch / extensions included) and Mac built after syncing mainline with #713 and these fixes. Mac signing disabled; compilation compatibility only.
- Hub: `go test ./internal/lark`, covering projection, quotes, media URL forwarding; disabled state / unknown versions discard unexpected bodies.
- Upstream independent branch after mainline sync: 30 reading / actual-route tests passed; old-version deployment patch has another 18 reading tests. Format, lint, type checks passed.
- UI data and key results are mocks; no test messages sent to real chats. Real account used only for read verification.

Review addition: Go / Swift test missing, null, and wrong-type optional projections, plus full-page decoding with normal messages alongside invalid projections. Invalid individual projections display as disabled; never fall back to old-field plaintext.

## Phone vault unlock and encrypted text sending (#689)

Only chats entered through home Messages gain "Ordinary / Encrypted message" selection. After choosing encryption, select historical content for public display; candidates match the Lark gateway web UI (historical text within one day two weeks earlier). Without candidates, retain the draft, never silently substitute a fixed placeholder. If the vault is locked on first send, enter a password on phone: gateway login password when unified password is enabled, otherwise vault password. Password exists only briefly in the unlock request, never drafts, outbox, logs, configuration.

Inbox keeps the original composer. Both entries share drafts / outbox; opening an unsent encrypted draft from Inbox retains it and guides back to Messages, never sending plaintext. First version supports text / quoted replies in ordinary private and group chats; encrypted attachments, topic groups, thread composers remain future scope.

### Deployment and authorization

Upgrade both the Lark gateway with native encryption APIs and Persona Hub; a new app alone does not enable it. Verify deployment separately, not from app installation or merged PRs.

1. Create a vault in the gateway web UI. Agent authorization for decrypted-body reads stays in existing web settings; native-send authorization does not enable it.
2. Set independent native-encryption credentials / account in gateway deployment (variable names in the Lark extension docs). Generate new 43–128-character base64url credentials; never reuse ordinary Agent tokens. Account is the actual Lark user ID, not a gateway slot number.
3. Set extension secret `native_olk_token` to the same independent credential (since #1571 hub no longer reads `lark.native_olk_token` from `hub.json`; see `docs/operations/config.md`). `lark.account` still selects a logged-in gateway slot. Restart both services. Never put credentials in app, repository, PR, assistant environment, or QR codes.
4. Revoke by removing / rotating gateway native credentials and restarting. Old hubs can no longer unlock, prepare ciphertext, or submit new encrypted sends. Upstream-accepted messages are unaffected. Ordinary Agent access and decrypted-reading authorization are independent.

Missing independent authorization, account changes, unsupported interfaces, wrong passwords, or encryption failures explicitly fail and retain input, never fallback to plaintext. Vault creation, recovery after password resets, authorization configuration remain web / deployment tasks. Phone only unlocks / sends, never exports private keys. Public access follows Hub TLS requirements.

### Unsupported versions

Before selecting encryption or opening historical public-display content, fetch native encryption state for the current chat's hub / account, without version-string comparison. Hub advertises `lark_native_encryption` in handshake, but clients still request state for versions supporting the endpoint without advertising it. Missing hub endpoint prompts a Hub upgrade; supported hub with missing gateway endpoint returns `lark_gateway_upgrade_required` (426), prompting a gateway upgrade. Missing authorization remains `VAULT_NATIVE_NOT_AUTHORIZED` (403), not a version error. Network / login / session errors do not masquerade as upgrade requirements.

Offer "Check again." Preserve encryption intent and input; failures never clear drafts or silently switch to ordinary sending. Normal sending rechecks account / authorization too. Old-interface errors in historical candidates, unlock, ciphertext preparation / submission all map to readable notices, avoiding `not found` when restoring old drafts or switching deployments.

### Retries and local storage

Generate ciphertext in the vault, atomically persist draft clearing and pending-send record, then submit. Each ciphertext has a stable UUID. A logical message may include preceding group-key distribution; the final message uses the original UUID and preceding IDs derive deterministically from it. Retry after interruption with identical ciphertext / IDs, never reencrypting and duplicating. Recheck account at send boundary; gateway `expectedSenderId` guards login switches. Ordinary Inbox sending unchanged.

If any encrypted draft / pending send exists, encrypt the entire local outbox snapshot with AES-GCM, key only in device keychain. Temporarily unreadable keychain must not overwrite an old snapshot. Ordinary drafts retain old-format compatibility. This protects user-entered pending bodies, not all server consumers (reading boundaries above). Encrypted-send echo merging uses only server message ID / client ID, never same-body matching or timeout removal.

### Native sending validation

- 62 Swift tests / 8 suites passed: unavailable device key, whole-snapshot encryption, old drafts, stable multi-ciphertext IDs / quote positions, account / session-change rejection, no timeout-based echo disappearance, type scale, icons, design tokens.
- `go test ./internal/lark ./internal/hub -count=1` passed against isolated Postgres: native authorization, no credential redirects, no password echoes in errors, read-only write rejection, no plaintext fallback, idempotent receipts after Hub reconstruction, account-change rejection. `go vet` and server build passed.
- Additional whole-package `-race` was not fully green: 5 existing Link / Update tests reported races, reproduced individually on unchanged baseline `7a519db7`. Targeted feature tests passed; do not report whole-package race success. macOS Unix-socket failure from long temporary paths was ruled out with short isolated-runner scratch paths, without product-path changes.
- Xcode 26.0 iOS (Watch / extensions included) / Mac built, Mac signing disabled. All sends, unlocks, passwords used isolated mocks, no real-chat sends; deployment and real end-to-end encrypted sending remain unverified.

- iPhone 18 Pro: 3 UI flows each in light / dark passed; 16 final screenshots inspected individually. Covers unlock failure then success, retry, cross-entry / force-quit recovery, ordinary Inbox composer unchanged.

Version compatibility regression (#1131): 7 Swift tests passed (old routes 404 / 405 and 4 error-code representations); 4 UI flows passed light / dark, with 8 screenshots individually inspected. Go lark / proto tests, isolated-Postgres native-encryption / handshake tests, and matching `go vet` passed. Initial UI regression found mock generic `not_found` unconverted; added product handling and parameterized cases, rerun passed.
