# Extension model web acceptance (#1854, 2026-11-05)

## This round

- Settings › Extension details: per-contribution toggles, non-clickable connector-management entries, dependency status, and pack members.
- Disabling dependencies: the server lists the full dependency chain; the web client sends `cascade: true` after confirmation. Dependents are not uninstalled.
- Marketplace installation / upgrade: both the regular web client and admin UI show permissions for every `bundle` item, returning each preview ID / permission summary. Confirmation is blocked if any preview expires or contains `unresolved`.
- Uninstalling a pack lets the user select members; the server first checks members' external dependencies, then disables them as confirmed. Data is retained by default.
- Older hubs without model fields retain the old detail view and request format.

## Validation

- Web `npm test`: 141 files, 1361 tests passed; `npm run build` passed (existing large-chunk / pptx static-and-dynamic-import warnings remain).
- Targeted hub regressions: `TestExtensionDependenciesAndPacks|TestContribution|TestDeclarativeKit` passed, covering that no package is deleted before a pack member's external dependency is rejected.
- Privacy and documentation-path checks passed.
- Browser: a separate fake hub + CDP interception of model APIs, with actual clicks through details, cascade-disable confirmation, and pack previews using demo data. Light and dark screenshots were inspected for both regular and admin pages. Pages were retained; user pages were not closed.
- The fake admin APIs did not simulate full metrics, so the runtime statistics behind the screenshot showed expected mock-data errors. The installation preview itself displayed correctly; this browser run's log must not be described as error-free.

## Published examples

- `persona-extensions` #5: `39ab940`, starter kit / pack 0.1.0. Original CI passed; after integration with main, CI remained queued and did not run. Local `scripts/ci.sh` passed in full, and the PR was merged under the runner-outage policy.
- Two Releases were created; #10 unified the two indexes and signatures. Published package digests come from the actual assets, not from a repacked tar archive.
- The hub installed a pack from the signed marketplace: the existing `linuxdo`, already meeting the version requirement, was not reinstalled; only `starter-kit` was added.
- APIs for three quick prompts, one theme with no problems, and two rule templates all returned 200. After disabling `prompts/today`, only that item disappeared from the API; enabling it restored it.
- Uninstall removed only the pack and kit added in this round, retaining their data. Existing `linuxdo`, other extensions, and the Lark version were untouched; the legacy list API still returned 200.

## Declarative client wrap-up

- #2285 (79974cd5): quick prompts were connected to the existing composer menu. Actual clicks in light and dark mode confirmed that selection only fills a draft; no send / session-creation API was called.
- #2316 (4c30b5f1): `triage_rules` uses the design system's multiline field. Template selection asks for confirmation first, replaces only the draft, and does not save automatically. Light and dark confirmation screenshots were inspected.
- Extension themes: allowed role colors are validated against the actual design-system backgrounds. Role tokens are written only after explicit selection; switching back to default or disabling the extension precisely restores the previous inline tokens. Existing stylesheets are unchanged. Screenshots of actual selection in light and dark mode were inspected; problematic colors are not listed. Pixel-by-pixel PNG diffs between the default “No theme” and “Added but not selected” were empty in both modes. Tokens matched after a round-trip color switch, but the original PNGs still had a few raster-edge differences; do not report zero differences.
- #2306 (73e8fe5c, Fixes #2288): pack-member deletion failures return per-item results, retaining the pack for retry. Local / admin uninstall permissions are not mixed with marketplace-device permissions. Fake-storage fault regressions and full hub / extension package tests passed; no faults were injected into a real hub.
- Android #2300 was completed by its owner, with test / APK / Robolectric pixel regressions and unrun items recorded separately. Apple #1861 remains with its original owner; it was neither taken over nor claimed complete.

## Acceptance boundaries

Cross-extension cycle detection in the dependency model, full bundle-plan validation and failure rollback, exported contributions and multilevel readiness propagation, and preserving custom export points in generated types remain limits of the early model implementation. They were not completed in this client wrap-up. The existence of APIs or web pages does not mean the full flow works on all four clients; overall acceptance is still governed by #1843.
