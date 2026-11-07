# Lessons: product-facing README assets

## Separate the product story from deployment internals (#6218)

- Lead with what the person can do and how their work moves forward. A database name, transport acronym or process command belongs in linked technical docs, not the primary product diagram. Keep the overview aligned with real capabilities; do not turn a marketing picture into a second architecture specification.
- Clients, Agent runtimes and session engines are different categories. Present them as matching, independently labeled support rows and preserve public product spellings, including `SuperAgent`, `Agent`, `Claude Code`, `Codex`, `DSH`, `pi` and `opencode`.
- Generate both language variants from one source. A committed SVG that looks correct today can revert on regeneration if only the artwork was edited. Test generated equality, labels, links and icon contrast, and inspect both language and appearance previews.
- A local demo is not a live integration. Verify release-mode routes and bundled samples, not only DEBUG screenshot flags; describe simulated replies and unsent actions honestly. Screenshots with one UI language must not be presented as if they were localized into another.
