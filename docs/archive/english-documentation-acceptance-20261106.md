# English documentation acceptance (2026-11-06)

Acceptance record for [#5054](https://github.com/nwverse/persona/issues/5054) and [#5056](https://github.com/nwverse/persona/issues/5056). This is a dated record, not a living design document.

## Scope and outcome

- [CLAUDE.md](../../CLAUDE.md) is English. Its [Language](../../CLAUDE.md#language) section requires English code comments, documentation, commits, issues and PRs; synchronized English/Chinese READMEs; user-facing strings in localization resources with at least en, zh-Hans and ja; and resource-only language additions.
- [README.md](../../README.md) is English; [README.zh-CN.md](../../README.zh-CN.md) is Chinese. Both have reciprocal switches at the top and matching content. Their banner and architecture SVGs are generated in both languages.
- All 174 existing Markdown documents under docs/ are English (175 including this record), including lessons, archive records, redesign research and screenshot notes. The HTML prototype and standalone architecture SVG are also translated. File names remain unchanged.
- Guides added while the translation was running (CarPlay and managers) are covered. The retired connector-specific guide and its README screenshot assets stay deleted; the translation does not restore upstream removals.
- Changes to code comments and app localization implementation belong to separate work. This task changed documentation, the README illustration generator, and documentation/README regression tests, not app runtime code.

## Merged work

Every PR below was squash-merged through the repository's private-value-scanning merge hook. Concurrent upstream changes were reconciled in isolated integration worktrees rather than replacing whole conflicted files.

| PR | Scope |
| --- | --- |
| [#5130](https://github.com/nwverse/persona/pull/5130) | English CLAUDE.md, language policy, bilingual README and localized illustrations |
| [#5205](https://github.com/nwverse/persona/pull/5205) | Protocol and web guides, preserving the latest manager/channel/Inbox/health contracts |
| [#5232](https://github.com/nwverse/persona/pull/5232) | Testing lessons, design guides, prompts and the unified-entry HTML prototype |
| [#5240](https://github.com/nwverse/persona/pull/5240) | To-dos, hub updates, health, Office and related archive records |
| [#5261](https://github.com/nwverse/persona/pull/5261) | CarPlay and remaining manager-guide labels |
| [#5274](https://github.com/nwverse/persona/pull/5274) | Launchers, model configuration, Mac/Windows lessons and redesign notes |
| [#5276](https://github.com/nwverse/persona/pull/5276) | Extensions, architecture and related acceptance/screenshot notes |
| [#5285](https://github.com/nwverse/persona/pull/5285) | Naming migration, workspace board and node/Lark-account notes |
| [#5287](https://github.com/nwverse/persona/pull/5287) | Agent work, Codex, networking, voice/transcript lessons and multi-hub |
| [#5288](https://github.com/nwverse/persona/pull/5288) | Mac, messaging, visionOS, Mermaid flows and architecture SVG |
| [#5289](https://github.com/nwverse/persona/pull/5289) | Testing, releases, runtimes, Agent states and bot notes |
| [#5294](https://github.com/nwverse/persona/pull/5294) | Phone tools, recording, assistant rich UI and redesign research |
| [#5303](https://github.com/nwverse/persona/pull/5303) | Knowledge base, admin, iOS lessons and runtime notes |
| [#5317](https://github.com/nwverse/persona/pull/5317) | Inbox, unified entry, Android lessons and related research |
| [#5324](https://github.com/nwverse/persona/pull/5324) | Android guide |
| [#5335](https://github.com/nwverse/persona/pull/5335) | Connectors, Android design, workflow and update guides |
| [#5343](https://github.com/nwverse/persona/pull/5343) | Link, bots, machines, collaboration, migration and parity notes |
| [#5364](https://github.com/nwverse/persona/pull/5364) | Voice, config, mesh, mail, goals and browser plan |
| [#5383](https://github.com/nwverse/persona/pull/5383) | Terminal, computer use, Live Activities, Runtime CLI and getting started |
| [#5384](https://github.com/nwverse/persona/pull/5384) | iOS, hub restart, usage sources and Lark agent guides |
| [#5415](https://github.com/nwverse/persona/pull/5415) | Assistant, channels, Docker, push relay and script CLI |
| [#5416](https://github.com/nwverse/persona/pull/5416) | Server, Claude integration, connector protocol and remaining service notes |
| [#5419](https://github.com/nwverse/persona/pull/5419) | Onboarding, i18n, network, engines and site |
| [#5434](https://github.com/nwverse/persona/pull/5434) | State, server lessons, avatar, shell and remaining design research |

The closing PR for #5056 also fixes the final audit's omitted menu labels, obsolete issue-language guidance, stale iOS language statements and internal product names in prose, while preserving real protocol identifiers. The prototype's local task matcher accepts English input as well as its original Chinese examples, so the translated demo still generates plan cards.

## Verification

The final candidate was verified from a clean integration checkout with the latest merged translations.

```sh
python3 -m unittest scripts.test.test_doc_paths scripts.test.test_doc_go_paths scripts.test.test_readme_languages
```

```text
Ran 16 tests
OK
```

The checks cover repository paths and file link targets, the translated CLAUDE docs-index heading, both READMEs, reciprocal language switches, and reproducible English/Chinese SVG text. The separate Markdown-fragment audit covers all docs subdirectories and the entry documents, including historical notes excluded from current-path checks. Required old heading aliases remain valid.

```text
90 local Markdown fragment links checked; 0 unresolved
```

`git diff --check`, Python syntax checks for the illustration generator, staged/range private-value scans and exact-head merge-hook scanning passed. Each batch also compared headings, tables, lists, fences, links, issue references and technical literals with its source; completed translation chunks survived the session interruption and were not redone.

### Screenshots actually inspected

- [English README illustrations, light and dark](https://github.com/user-attachments/assets/7a55863c-872c-45fe-9709-a69a26676b73)
- [Chinese README illustrations, light and dark](https://github.com/user-attachments/assets/1e5f48bb-fed8-40a4-acab-1f66fe702af7)
- [Unified-entry prototype, light](https://github.com/user-attachments/assets/531ab221-99e0-443f-b17b-7dc5831d779a)
- [Unified-entry prototype, dark](https://github.com/user-attachments/assets/a1393209-82c5-4d52-aa0e-75b20ea72ce6)

Eight README illustration variants were rendered and inspected, without clipping or overflowing labels. The translated prototype was checked in 22 light/dark states. The final English task-input fix passed four cases (English fix/write requests, a Chinese request, and a neutral acknowledgment), with zero JavaScript errors; its plan card was also inspected in light and dark. The translated standalone architecture SVG was rendered and visually inspected. All pictured app data is synthetic.

### Remote verification

GitHub REST reads confirmed the merged PRs and the actual master versions, not only local branches:

```sh
gh api repos/nwverse/persona/contents/CLAUDE.md --jq .content | base64 -d
gh api repos/nwverse/persona/contents/README.md --jq .content | base64 -d
gh api repos/nwverse/persona/contents/README.zh-CN.md --jq .content | base64 -d
```

The returned files contain `### Language`, `English · [简体中文](README.zh-CN.md)` and `[English](README.md) · 简体中文`, respectively. Batch merge verification also checked the submitted document blobs against their merge commits.

## Deliberate non-English data

English prose does not mean rewriting data that has to match the application. The following remain intentionally:

- The en / zh-Hans / ja glossary and native language names in the i18n guide.
- Exact emitted/matched protocol strings, prompt prefixes, regex terms, synthetic fixtures and CJK-width examples, with English glosses when needed.
- Explicit compatibility anchor IDs for existing links.
- Chinese illustrations used only by README.zh-CN.md, and existing screenshots of localized app UI.

Internal names in ordinary prose are generalized. Real source paths, persistent identifiers, protocol keys and runtime compatibility identifiers are not blindly renamed.

## What was not verified

- CI remained queued and was not awaited under the user's documentation-only merge authorization. Queued checks are not passed checks.
- No full app builds, simulator UI suites, deployment or TestFlight release were part of this documentation task.
- An extra server-suite check during #5287 reproduced pre-existing `TestHardcodedChineseBaseline` and `TestNoMachineTimeZone` failures on the pristine base. Relevant packages passed, but the full suite is not claimed as passed; the PR records the details.
- The additional full Go i18n check in #5419 reported concurrent upstream ratchet failures, without changing code or baselines:

  | File | Observed hard-coded Chinese / permitted baseline |
  | --- | --- |
  | `server/internal/extension/market_git.go` | 11 / 10 |
  | `server/internal/extension/sandbox/sandbox.go` | 12 / 10 |

  Targeted i18n tests, Android i18n guards and the Apple catalog check passed. These results do not turn the full failing Go i18n run into a pass.
