# #277: rename-residue audit of all open PRs

## Fixed scope and findings

This record archives the **25 open PRs** queried at 2026-11-03 06:41:09 UTC. The audit baseline is fixed at `09584f34fe6eb29f91ddc2a3c2a8df94017cb3ab`, and each PR uses the fixed head below. This is a snapshot of that audit, not ongoing acceptance of all later open PRs.

The original read-only audit compared each PR's own diff and simulated merging it with the fixed baseline, finding **11 migration issues requiring coordination across 9 PRs**. Stacked PRs' own changes and inherited content are attributed separately: #713 inherits #566, #714 inherits #712, and #764 inherits #713. Parent-PR issues are not counted twice.

This update archives only classified findings and public PR links. It does not copy author accounts, personal names, old personal module paths, specific user data, internal values, or credentials from the original report. Organizing this document did not rerun the full PR audit and does not replace each PR's functional review, build, physical-device testing, or release acceptance.

## Classification rules

- **Automatic rename**: Git has mapped old Apple files to the current source root; old paths in the original diff do not mean the old directory is recreated after merging.
- **Preserved identity**: Old modules, Intent / Control / ActivityAttributes, protocol / MCP names, serialized values, cache keys, service identities, and generated links retaining legacy forms remain under the compatibility contract. Zero old-brand strings is not required.
- **Migration required**: Actual new code or text bypasses new/old environment compatibility, references migrated source / imports / artifacts, or reintroduces the old visible brand. Old-branch content in conflicts needs correct resolution, but a simulated conflict tree is not releasable code.

Classifications may coexist. “No independent new migration issue found” describes only the rename scope of this fixed snapshot, not whether the feature PR passed quality acceptance.

## Fixed findings for all 25 PRs

All heads below are full SHAs from the audit snapshot. Keep each original row even if the PR later merges or changes; do not replace the historical audit object with a new head.

| PR | Fixed head | Classification | Fixed-snapshot finding |
| --- | --- | --- | --- |
| [#90](https://github.com/nwverse/persona/pull/90) | `ab8218172a0f3cb368fe61e403918ec1a20c1e42` | Migration required; preserved identity | New bridge hardcodes the old default storage and cannot find the program correctly with a custom-root install; service labels and log identity remain. |
| [#140](https://github.com/nwverse/persona/pull/140) | `54060f79fb33b147ce0277566b4b174690dcdc78` | Preserved identity | Only registry / lock documentation changes; no independent new migration issue found. Old context is not newly introduced residue. |
| [#241](https://github.com/nwverse/persona/pull/241) | `3289b60adac145276cc8ac8c6a9eda3feff064d0` | Migration required; preserved identity | New device-connection page has the old visible brand; product prompts need updating. Pairing-link generation retains the legacy form. |
| [#254](https://github.com/nwverse/persona/pull/254) | `47928b72a386ca42fcd3a8baa88b489417cc40fa` | Migration required; preserved identity | New machine descriptions still use the old product brand; host and protocol identities stay unchanged. |
| [#291](https://github.com/nwverse/persona/pull/291) | `a28c7ff81e7f2622b80abf5a74a922f37b435683` | Migration required; preserved identity | New production entry points recognize only old prefixes; two files retaining system types coexist, and the old source root reappears. Resolve conflicts by merging actual differences, not restoring the old root or changing system-type identities. |
| [#292](https://github.com/nwverse/persona/pull/292) | `29433aae8420aac8402bd88110ab337e647677e9` | Migration required; automatic rename; preserved identity | Two new Go files need current-repository imports; new Apple files move automatically into the new root, with module / test / wire identities preserved. |
| [#306](https://github.com/nwverse/persona/pull/306) | `3af57f29f2d9792955a83cf20bf2f07fe30dd7b1` | Preserved identity | No independent new migration issue found; old assistant recognition is diff context. Keep baseline identity compatibility when merging. |
| [#415](https://github.com/nwverse/persona/pull/415) | `752e9133f733d2b5bd6c9e946248e400af6c4a67` | Preserved identity | Old imports occur only in a conflicting branch awaiting resolution; keep current-repository imports. No clean additions of the old root or public environment entry points were found. |
| [#521](https://github.com/nwverse/persona/pull/521) | `3eda6479db71b3d978912de5a343638ea6e4c501` | Automatic rename; preserved identity | New Swift files move automatically to the current root; the test module remains. Test-owned environment variables are not automatically production-compatibility omissions. |
| [#530](https://github.com/nwverse/persona/pull/530) | `de257b96130cfde61b5f7b275082d3d750be6ac1` | Automatic rename; preserved identity | New pages and tests migrate automatically; existing demo fields and content are not new product text merely because the entire line changes. |
| [#566](https://github.com/nwverse/persona/pull/566) | `e370599b32ffa6f137bef44ea27e6e701478a9f5` | Migration required; automatic rename | New attachment Go imports still point to the old repository path; a new Release guard points to the old binary and misses the current artifact; Apple source migrates automatically. |
| [#581](https://github.com/nwverse/persona/pull/581) | `30974c724ebb4bac5aebce28860ec646fd28c034` | Preserved identity | No independent new migration issue found; temporary filenames and compatible CLI names are not missed visible-brand changes. |
| [#679](https://github.com/nwverse/persona/pull/679) | `06b9f7abff712097fa8af008829bcf3756dfb2e2` | Automatic rename; preserved identity | New tests migrate automatically; MCP tools and modules remain, without changing tool identity. |
| [#712](https://github.com/nwverse/persona/pull/712) | `5d8ca27b76098fbea38161b73768ba004d45a42b` | Preserved identity | Current source root and targets remain; old module / bundle identities remain. No independent new migration issue found. |
| [#713](https://github.com/nwverse/persona/pull/713) | `9ec806be75285f26bf367dc66bb81200acd5d654` | Migration required; automatic rename | Its own new button still uses the old visible brand; imports / Release guard issues are inherited from #566 and not attributed twice; new files migrate automatically. |
| [#714](https://github.com/nwverse/persona/pull/714) | `fdc123586c9fc5e4fcab771c099f0b747864c071` | Preserved identity | New palette source is under the current root, and the test module is correctly preserved; inherited #712 content is listed separately. No independent new migration issue found. |
| [#717](https://github.com/nwverse/persona/pull/717) | `48f8305f127f0711295ee1c480aaacc98cb149df` | Automatic rename; preserved identity | New video source and tests migrate automatically; internal temporary-cache names and video-test environments are not production-brand residue. |
| [#721](https://github.com/nwverse/persona/pull/721) | `c84b005028cd066edc1dd6419c62559ec50ee5f2` | Migration required; preserved identity | New MCP error text still uses the old visible brand; simulated merge is conflict-free but would reintroduce it. Server / tool identities remain. |
| [#738](https://github.com/nwverse/persona/pull/738) | `93acf9c74e0a8764b8e5e663f46785a5b76770a2` | Preserved identity | Compilation-condition changes do not alter the current source root or system-type identities; no independent new migration issue found. |
| [#749](https://github.com/nwverse/persona/pull/749) | `d6182f90da10a0108e342c490bc35ae9412a219f` | Migration required (fixed by #809) | A new source reference in current documentation points to a nonexistent source root; it merged during the audit and was later fixed with a single documentation correction. It is not a blocker for a pending PR. |
| [#757](https://github.com/nwverse/persona/pull/757) | `d38f87ba8c957ad96c138791e3656640c3023154` | Preserved identity | New runtime root is correct; health / MCP / schema and module identities were not changed by rebranding. No independent new migration issue found. |
| [#764](https://github.com/nwverse/persona/pull/764) | `26999220bf6521540308b5317ceb5f18e4f30886` | Automatic rename; preserved identity | No independent new migration issue in its own changes; new encryption files migrate automatically. Related imports / guard / button issues belong to parent PRs; mock / env / wire identities remain. |
| [#765](https://github.com/nwverse/persona/pull/765) | `c33e81ddb00e7233964dece490270b61a5fb27cd` | Automatic rename; preserved identity | UI-test paths in the original diff move automatically into the new root; merged during the audit. No independent new migration issue found. |
| [#781](https://github.com/nwverse/persona/pull/781) | `ec6e13c39bf525706630d995acfdaa17e99cf655` | Preserved identity | New business / mock code does not reintroduce old imports, runtime roots, public environment entry points, or product branding. |
| [#784](https://github.com/nwverse/persona/pull/784) | `579391134cc974d27adbe0cbfccccf14080541ed` | Preserved identity | New Go imports use the current repository; protocol fields are business additions, not mechanical renames. |

## Handling after the snapshot

- The missed non-link source reference in #749 was fixed and merged by [#809](https://github.com/nwverse/persona/pull/809), merge `29e7249c1b9857ba58e3453fa285543b74d5d75a`. A targeted check verified that the old reference failed and the corrected file actually existed. The original Markdown-link audit did not cover this code literal; link counts cannot replace this check.
- After excluding the fixed documentation item, migration evidence for **10 issues across 8 PRs** from the original snapshot was added to each PR. This describes the original issue scope, not how many remain unfixed now. Incremental rechecks of new heads appear below and still require coordination with the original owners. Supplemental evidence or partial corrections do not mean the whole PR is fixed or merged, nor do they authorize merging unrelated features.
- PR metadata was rechecked while organizing this document, without repeating the full audit. Current heads for #90, #241, #254, #291, #292, and #721 matched the snapshot above; #566 and #713 had updated heads. The leader performed read-only incremental rechecks of those two, appended separately below. They do not overwrite the original fixed findings or justify saying “all latest heads are unchanged.”

| PR | New head queried during documentation work | Handling boundary |
| --- | --- | --- |
| [#566](https://github.com/nwverse/persona/pull/566) | `ff5c6e5a37ddc3f44b04b7fedc7465e36b636734` | New attachment imports now use the current repository; the Release guard's old binary path is fixed and this PR's new marker added. But the full list still lacks `showConnectionCycle` and `showToasts`; it must rebase and retain the baseline's complete guard. This is a partial fix, not fully fixed. |
| [#713](https://github.com/nwverse/persona/pull/713) | `a64a1b4d4af77b4286c798b8f9a87f33ed3c527e` | Current source paths exist, but the new button still uses the old visible brand; that text issue is unfixed. Parent-PR and own business quality still require separate acceptance. |

This is not a new count of open PRs. The original 25 rows are a fixed snapshot; later PRs that open, merge, or update cannot be assumed audited from it.

## Incremental recheck of the 08:59 fixed snapshot

This section appends the incremental audit at 2026-11-03 **08:59:00.026551 UTC**, with baseline fixed at `0e02f0f7fc6aec48cc73197642917c382294ae81`. There were **26 open PRs: 7 new PRs, 4 updated heads, and 15 unchanged heads**; 6 of the original 25 left the open set, which does not imply all were merged. Metadata was checked again at audit completion, **09:02:59.325821 UTC**; baseline, set, and heads were unchanged.

That audit rechecked only **11 delta objects**, not the 15 unchanged PRs. Simulated merges had 9 conflict-free results and 2 conflicts. **No new independent migration issues were found**, but #764 still carries the parent branch's old button text and needs coordination. This does not erase existing issues in unchanged PRs or mean all open PRs passed functional, build, screenshot, or release acceptance. The documentation lane only archives existing evidence; it did not rerun the incremental audit.

### 4 updated heads

The table appends new evidence without changing the original 25 rows or the previous partial-fix table. “Fixed” refers only to the specified migration item, not the whole PR being merged or passing business acceptance.

| PR | 08:59 fixed head | Incremental finding and boundary |
| --- | --- | --- |
| [#566](https://github.com/nwverse/persona/pull/566) | `40d0fec99ded5ba66a5f4ed44277d09574077a7b` | Original attachment-import and Release-guard migration issues are fixed: current Go module, guard reads the `Persona` binary, all 190 fixed-baseline markers retained, and `showLarkAttachmentDraft` added; `showConnectionCycle` and `showToasts` are not lost. Simulated merge is conflict-free; attachment functionality was not accepted. |
| [#581](https://github.com/nwverse/persona/pull/581) | `bd556eab5f14d9e70364bc771595d1de67013a1b` | No independent new migration issue found. Current Go module, runtime root, and public environment entry points did not regress; old temporary filenames and compatible CLI names remain. This does not establish that authorization functionality passed. Simulated merge is conflict-free. |
| [#713](https://github.com/nwverse/persona/pull/713) | `21242d10f3d2a10d4a78828f5c145b3ba9f8b24d` | Original button text is fixed to “Summarize with Persona”; inherited imports and the full guard remain intact. Simulated merge conflicts in the chat page and lessons document. No new independent migration issue found, but it cannot be claimed immediately releasable. |
| [#764](https://github.com/nwverse/persona/pull/764) | `070eb0192249283eddcbe262dd624e9bed80bbee` | Its own encryption delta adds no old-module, root-directory, or public-environment-entry issue; imports and the full guard remain intact. It still inherits the parent branch's “Summarize with Jarvis” button and has not incorporated #713's correction. Simulated merge conflicts in the chat page and lessons document; this residue is not counted again as its own new issue. |

In #764's fixed head, `apple/Persona/Views/Inbox/Lark/LarkChatView.swift:103` still contains `Label("让 Jarvis 总结", systemImage: Theme.Icon.ai)` (“Summarize with Jarvis”); its base and old snapshot contain the same text. Incorporate #713's fix, or retain “Summarize with Persona” when resolving conflicts. This confirms only that the branch still carries old text; **a conflict tree cannot establish that the final product will necessarily introduce this error**. Internal methods, modules, and system identities do not rename with the label.

That audit also used anonymous `.app` fixtures to verify complete-guard conditions for #566, #713, and #764: none of the three heads lacked baseline markers, and every new marker was detected. This is not validation of the release script, real artifacts, or uploads; the conflict trees for #713 and #764 are not releasable versions either.

### 7 new PRs

| PR | 08:59 fixed head | Incremental finding and boundary |
| --- | --- | --- |
| [#828](https://github.com/nwverse/persona/pull/828) | `22b445e873e3917e9ed7d9b7adbbdb1f9fbbe33b` | Current Apple source root; sorting-state changes do not blindly rename modules, persistent keys, or system types, and the guard loses no entries. No independent new migration issue found; no sorting-function or screenshot acceptance performed. |
| [#837](https://github.com/nwverse/persona/pull/837) | `2cc85f9ce08882adf56d6f99f64730f5d4914a5f` | HOME-only runtime-root restoration; parser hash matches the existing targeted-recheck version, so evidence for that exact version is reused without rerunning script tests. No independent new migration issue found. This does not extend to complete-parser or real-upgrade acceptance; see the next section for actual merge status after the snapshot. |
| [#860](https://github.com/nwverse/persona/pull/860) | `4bc2a2cc38f0506e529caec8ffaa85089c3e2640` | Current Go module; botmux-owned upstream environment and cookie identities are not missed Persona-prefix changes, and visible source branding is Persona. No independent new migration issue found; host execution, message delivery, and real-session queries were not performed. |
| [#863](https://github.com/nwverse/persona/pull/863) | `0589a9a130133c8ee000da31e3b5eb7549d233ce` | Installation documentation does not reintroduce the old source root; the old Xcode project entry is a preserved generated-project alias. No independent new migration issue found; installation results were not validated. |
| [#869](https://github.com/nwverse/persona/pull/869) | `2d4e1eaf9eb9b28948f48ac20ecc5a2b1c59e1b3` | Local Android keyboard changes do not alter package, system-component, serialization, or cache identities. No independent new migration issue found; no build or screenshots. |
| [#871](https://github.com/nwverse/persona/pull/871) | `ceb9d95e4df1f6be11fa7141dd72356779a5c252` | Local web-rendering changes do not alter schemes, persistent identities, runtime roots, or public environment entry points. No independent new migration issue found; no web tests, build, or browser acceptance. |
| [#872](https://github.com/nwverse/persona/pull/872) | `76e97efd47c7785d347e4e9eba64ec803cc2e280` | Local reply-suffix and prompt-description changes retain new and old markers, without changing product names, tool identities, or permission rules. No independent new migration issue found. This audit did not run reply tests, golden tests, or Lark requests. |

Simulated merges of all 7 new PRs with the fixed baseline were conflict-free. This describes only the merge results for fixed objects, not CI or runtime acceptance.

**15 unchanged PRs:** #90, #140, #241, #254, #291, #292, #306, #415, #521, #679, #712, #714, #717, #721, #757. Only head metadata was checked; original migration findings for #90, #241, #254, #291, #292, #721, and others remain. Their status was not reassessed in this round.

**6 PRs that left the open set:** #530, #738, #749, #765, #781, #784. Only the set change is recorded; final states and actual merges were not re-audited individually. #809's path correction and #849's historical-audit archive remain, without being presented as production revalidation in this round.

## EnvironmentFile support after #837 actually merged

[#837](https://github.com/nwverse/persona/pull/837) merged as `62039ebc4d17285a4ffd39e3631e96e18e65c70d`. This is actual status verified after the 08:59 snapshot; it does not overwrite the fixed head in the table.

`scripts/persona_identity.py` now supports **extracting only `PERSONA_HOME` / `JARVIS_HOME`** from `EnvironmentFile=`: files override the unit environment in file order, supporting optional files, filename patterns, and empty-assignment resets, followed by `UnsetEnvironment=`. If a file has other settings but no HOME, its existence alone no longer prevents restoring the old default root. Explicit new/old HOME precedence and defined-empty behavior remain. Missing required files or root-field parse errors still stop execution; failure to restore must not switch to another empty store. File content and shell expansions are not executed, and other fields are not returned, logged, or used for authentication. See [Runtime-directory restoration](rename.md#部署脚本按服务恢复运行目录) for the full contract.

**Evidence from the corresponding code lane, not rerun by this documentation lane:** Final fixed head `2cc85f9ce08882adf56d6f99f64730f5d4914a5f` records 128 local script tests and 16 formal anonymous focused tests passing. All 9 independently reported issues have passing evidence using their original inputs on the fixed version, including the final UTF-8-byte and Unicode-codepoint escape fixes. This establishes only that no confirmed issues remain within the audited coverage, not full systemd compatibility. CI's self-hosted-runner checks stayed queued throughout a bounded wait and never ran. Merge used the authorized local-validation fallback; **CI did not pass**. No real EnvFile, production configuration, database, deployment, or upload was used.

## Server-test safety boundaries

[#802](https://github.com/nwverse/persona/issues/802) records an isolation problem where the server-test launcher inherited the production runtime root. [#812](https://github.com/nwverse/persona/pull/812) later fixed launcher isolation, merge `81b237e397ed7b83ee06dedf3f6320ebf7d3af01`. The corresponding lane records a passing real-Postgres recheck, but prior evidence shows actual production files were deleted; **the full impact remains unknown**. Later isolation fixes and passing rechecks do not establish that the earlier production state was unaffected or restored.

This record states only safety boundaries and unfinished status. It does not copy production data, affected-object details, actual counts, addresses, or credentials. The documentation lane does not run real server tests, deployments, or uploads, nor access the production root to investigate impact.

The problem where database-URL query parameters could override the test path was fixed by [#879](https://github.com/nwverse/persona/pull/879), merge `761ce5473289be03808f7d941d8c17f10813cbaf`. An independent no-connection recheck found no confirmed defects within its limited scope; it does not count as a live Postgres pass. The final release owner must confirm that the freeze includes #812 / #879 and pass full validation with the new isolated launcher before continuing deployment / upload. Merging #837 or documentation alone does not lift the pause or prove recovery from the incident. See [Acceptance record](rename-acceptance.md) for current local acceptance and final release conditions.

## Release status and next steps

- **Release 1045 failed and cannot count as a success or final acceptance. The final release is not complete.**
- The original fixed-snapshot audit and the 11 delta rechecks above are complete, but unresolved migration items, subsequent PR coordination, and release freezing are different stages. **The final freeze has not started**; a completed audit does not mean all issues are fixed or all of #277 is finished.
- Original owners handle migration items in their own PRs, preserving correctly migrated baseline content. Do not merge unrelated features merely to wrap up.
- This documentation PR only archives and indexes evidence, preserving handoff boundaries. It does not merge business PRs, deploy, or upload.

See [Rename migration](rename.md) for compatibility rules and the preserved-identity list.
