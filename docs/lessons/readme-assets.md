# Lessons: product-facing README assets

## Separate the product story from deployment internals (#6218)

- Lead with what the person can do and how their work moves forward. A database name, transport acronym or process command belongs in linked technical docs, not the primary product diagram. Keep the overview aligned with real capabilities; do not turn a marketing picture into a second architecture specification.
- Agent runtimes and session engines are different categories: label them (the badges say `agent` and `engine`). Preserve public product spellings such as `Claude Code`, `Codex`, `herdr`, `Orca`, `DSH`, `pi` and `opencode`; the product pitch spells it "SuperAgent" (the maintainer's choice, #6252).
- Generate both language variants from one source. A committed SVG that looks correct today can revert on regeneration if only the artwork was edited. Test generated equality, labels, links and icon contrast, and inspect both language and appearance previews.
- A local demo is not a live integration. Verify release-mode routes and bundled samples, not only DEBUG screenshot flags; describe simulated replies and unsent actions honestly. Screenshots with one UI language must not be presented as if they were localized into another.

## Restraint reads better than more boxes (README redo after #6189 / #6230)

- The README's first screen is a pitch, not a spec: the wordmark and one tagline, one sentence, three badge rows (clients, agents, engines; one source line per row ending in `<br>`), then the hero. Features are a plain list with one doc link each; setup steps, tables of platforms and architecture diagrams live in `docs/`.
- GitHub draws every table with borders and zebra rows, and colored icon tiles add more blocks. A grid of eight bordered cells with tinted icons looked busy in both themes; a bold-lead bullet list reads cleaner and stays searchable.
- Use one neutral badge style (a dark pill, muted kind, white product name) that works on light and dark pages; a colored value segment on every badge turns the badge row into a color strip.

## Previewing a README faithfully

- `gh api markdown` with `mode=gfm` renders like a comment: every newline becomes `<br>`, so badges on separate lines stack into a column. `mode=markdown` renders like a repository file. Preview with `mode=markdown`.
- The API wraps each `<img>` inside `<picture>` in a link; github.com's `<themed-picture>` script lifts it out so `<source media="(prefers-color-scheme: dark)">` applies. A local preview has to do the same, or dark mode shows the light pictures.
- In Chinese, `**加粗。**后文` does not close the bold: a closing `**` preceded by punctuation must be followed by whitespace or punctuation. Put the full-width punctuation outside the bold (`**加粗**。后文`) instead of adding a space.
