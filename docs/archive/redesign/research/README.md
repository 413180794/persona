# Raw research notes

The product name is now Persona (previously under another product name). Note file names, source evidence, protocol strings, old commands and `~/.persona` paths keep the research baseline: they are historical / compatibility references, not the new default names of the migrated version. For current names and configuration see [Naming and compatibility](../../../operations/config.md#命名与兼容).

> Written 2026-10-30 by parallel research agents, as evidence for the [redesign proposal](../README.md). Read-only research; no code was changed.
> The first 8 notes are round one; the last 3 are round two, added after you settled on "no Google services, public direct connection with IPv6 first" (line numbers refer to master at ec11652).
> Line numbers in the evidence refer to master at 77999ab, i.e. before PR #84 (Android first version) and about 20 other commits were merged; later changes are in section 26 of [../parity.md](../parity.md).
> The notes mark "facts" and "judgments" separately; performance numbers only cite values that the docs or code state were measured; everything else is a code constant or configuration.

| Note | Contents |
| --- | --- |
| [bh-architecture.md](bh-architecture.md) | The reference iOS app's overall architecture and engineering method: layering, concurrency templates, three-state results and idempotent receipts, scopes and generations, capability negotiation, storage, testing; what to borrow / not borrow |
| [bh-ui-qa.md](bh-ui-qa.md) | The reference iOS app's design system, first-time onboarding, prototypes and reviews, acceptance levels, test pyramid; the state and adoption rate of Persona's design system |
| [persona-ios-arch.md](persona-ios-arch.md) | Persona iOS's current architecture and tech debt: `AppModel` responsibilities, sync, caching, rules implemented in several places, list of platform-independent logic (the parts Android must match) |
| [persona-features.md](persona-features.md) | Persona iOS's complete feature list (213 items) and the Android equivalent for each (draft of [../parity.md](../parity.md)) |
| [persona-protocol.md](persona-protocol.md) | hub protocol: auth and pairing, 111 HTTP routes, WebSocket events, sync model, phone tools, push; server changes Android needs |
| [android-tech.md](android-tech.md) | Android technology choices (versions with sources): approach, modules, architecture, networking, storage, UI components, system capabilities, phone tools, testing, distribution |
| [hub-network-current.md](hub-network-current.md) | Audit of the hub's current networking (with line numbers): how it listens, pairing and auth, health and the WS handshake, heartbeats; what has to change for public direct connections; what to watch for to stay compatible with the existing iOS app (`Envelope` name clash, QR code format, endless reconnects on 401) |
| [public-direct-ipv6.md](public-direct-ipv6.md) | Research on public direct connections (with sources): IPv6 status of home broadband and mobile in China, ONT firewalls, prefix changes, blocked ports; address discovery; DDNS; multi-address route selection; Android 17 local network permission; TLS and pinning; hardening; background keep-alive and carrier idle timeouts |
| [android-cn-roms.md](android-cn-roms.md) | Chinese Android ROMs and version support (with sources): Android version distribution and minSdk, each vendor's market share and current version, each vendor's notification and background restrictions and settings deep links, persistent foreground services, Live Updates support, system capabilities without Google services, distribution and self-update, real-device matrix and cloud devices |
