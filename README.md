<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/readme/banner-dark.svg">
  <img src="docs/assets/readme/banner-light.svg" alt="Persona: your SuperAgent. One place for every task." width="100%">
</picture>

English · [简体中文](README.zh-CN.md)

### One SuperAgent for everything you do.<br>It knows your world, runs your Agents, and goes wherever you go.

[![client: iPhone](docs/assets/readme/badge-iphone.svg)](docs/ios.md) [![client: Apple Watch](docs/assets/readme/badge-apple-watch.svg)](docs/ios.md) [![client: Mac](docs/assets/readme/badge-mac.svg)](docs/mac.md) [![client: Vision Pro](docs/assets/readme/badge-vision-pro.svg)](docs/vision.md) [![client: Android](docs/assets/readme/badge-android.svg)](docs/android.md) [![client: Web](docs/assets/readme/badge-web.svg)](docs/web.md)<br>
[![agent: Claude Code](docs/assets/readme/badge-claude-code.svg)](docs/claude-integration.md) [![agent: Codex](docs/assets/readme/badge-codex.svg)](docs/codex.md)<br>
[![engine: herdr](docs/assets/readme/badge-herdr.svg)](docs/engines.md) [![engine: Orca](docs/assets/readme/badge-orca.svg)](docs/engines.md)

[Try the demo](#get-started) · [Features](#features) · [How it works](#how-it-works) · [Docs](#docs)

</div>

<br>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/readme/hero-dark.webp">
  <img src="docs/assets/readme/hero-light.webp" alt="Persona on iPhone: the assistant hands off to Agents, a Claude asks a question, and one tap answers it; beside it are the Inbox, a plan waiting for approval, live progress and machines" width="100%">
</picture>

<p align="center"><sub>Demo mode with made-up data. This recording still shows the Chinese UI.</sub></p>

## Features

- **A SuperAgent, one entry for everything.** Ask in one chat. Persona pulls in your context, breaks the work down and hands it to Agents. [Assistant](docs/assistant.md)
- **Phone and computer, one session.** Claude Code and Codex keep running on your computers. Watch them live, answer and approve from your phone, then carry on at your desk. [Engines](docs/engines.md)
- **Extensions for everything.** Tools, Agent runtimes, data sources, channels and voice plug in, with an SDK and a market. [Extensions](docs/extensions.md)
- **Dynamic Island, Siri and Shortcuts.** Every Agent at a glance on the Lock Screen; ask, approve or start work by voice, a shortcut or your watch. [Live Activities](docs/live-activity.md) · [Siri](docs/siri.md)
- **Knowledge base and Inbox.** Your chats, people and projects become shared memory; Lark, mail and GitHub land in one AI-sorted Inbox. [Knowledge](docs/knowledge.md) · [Inbox](docs/inbox.md)
- **Your phone's data, in reach.** Contacts, calendar, reminders, photos, health and location become tools, and writes are confirmed on your phone. [Phone tools](docs/phone-tools.md)
- **Connectors and channels.** Bring in Lark, mail, GitHub, iMessage, Telegram and WeChat; talk to Persona from the app, Lark, WeChat or by voice. [Connectors](docs/connectors.md) · [Channels](docs/channels.md)
- **Every screen.** iPhone, Apple Watch, Mac, Vision Pro, Android and the web, in English, Chinese and Japanese. [Clients](docs/ios.md)

## How it works

You run a small hub on an always-on machine; your devices talk only to it, and it reaches each of your computers, where Agents run in herdr or Orca. Your data stays on your own hardware.
[Architecture](docs/architecture.md) · [Network and security](docs/network.md)

## Get started

- **Just looking?** Install the [TestFlight beta](https://testflight.apple.com/join/vsjfy3hE) and tap **No Hub? Try the Demo**: no server or account needed.
- **Your own setup:** one command installs the hub, then scan a QR code. See [Getting started](docs/getting-started.md).
- **From source:** [config](docs/config.md) · [deploy](docs/server.md) · [contributing](CLAUDE.md)

## Docs

[Getting started](docs/getting-started.md) · [Architecture](docs/architecture.md) · [Protocol](docs/protocol.md) · [Docker](docs/docker.md) · [iOS](docs/ios.md) · [Mac](docs/mac.md) · [Android](docs/android.md) · [Web](docs/web.md) · [All docs](CLAUDE.md#docs-index)

<sub>Third-party licenses: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).</sub>
