# Third-party notices

## Brand marks

`apple/Persona/Resources/Assets.xcassets/Brands` and `apple/PersonaWatch/Assets.xcassets/Brands` hold other products' marks
(`web/src/views/inbox/lark.svg` is a copy of `brand-lark` for the web inbox),
shown next to the service Jarvis connects to. The marks are trademarks of their owners; using them here does not imply
endorsement. They are drawn as published: not recolored (the one-color OS marks take the distribution's color), not
redrawn.

| Asset | Mark | Source | Terms |
| --- | --- | --- | --- |
| `brand-claude` | Claude (Anthropic) | Simple Icons `claude.svg` (CC0 for the file) | Anthropic's brand guidelines |
| `brand-openai` | OpenAI Blossom | github.com/openai/openai-cookbook, `chatgpt-blossom-black.svg` (viewBox cropped to the mark) | openai.com/brand |
| `brand-lark` | 飞书 / Lark | github.com/larksuite/lark-samples, `public/svg/icon.svg` (viewBox cropped to the mark) | Lark user agreement; no public logo guidelines found |
| `brand-google` | Google G | developers.google.com/identity/branding-guidelines, `signin-assets.zip` (the G matted from the light and dark buttons) | Google Sign-In branding guidelines |
| `brand-gmail` | Gmail | gstatic.com Google product icon `gmail_2020q4_512dp.png` | Google brand terms |
| `ios-app-*` | Apple 通讯录、日历、提醒事项、健康、照片、地图 app icons | App Store artwork of each app (iTunes lookup `artworkUrl512`, fetched at 240×240), shown next to the data Jarvis reads from that app | Apple trademarks; Apple's terms for its marks |
| `os-*` | Ubuntu, Debian, Fedora, Red Hat, CentOS, Rocky Linux, AlmaLinux, Arch Linux, Alpine Linux, openSUSE, NixOS, Linux (Tux) | Simple Icons (CC0 for the files) | Debian CC-BY-SA-3.0, NixOS CC-BY-4.0, Rocky Linux CC-BY-SA-4.0; the others under each project's trademark policy |

## Ovis

Parts of `server/internal/screen` (Claude Code input-box rules) are ported from Ovis, and
`server/internal/screen/testdata/claude-rewind-2.1.304-*` is taken from its screen fixtures.

https://github.com/devswha/ovis

MIT License

Copyright (c) 2026 devswha

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
documentation files (the "Software"), to deal in the Software without restriction, including without limitation the
rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit
persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the
Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

## JetBrains Mono

The terminal font (`apple/Persona/Resources/Fonts/JetBrainsMono-*.ttf`, v2.304, unmodified).

https://github.com/JetBrains/JetBrainsMono

Copyright 2020 The JetBrains Mono Project Authors (https://github.com/JetBrains/JetBrainsMono)

This Font Software is licensed under the SIL Open Font License, Version 1.1.
This license is copied below, and is also available with a FAQ at:
https://scripts.sil.org/OFL


-----------------------------------------------------------
SIL OPEN FONT LICENSE Version 1.1 - 26 February 2007
-----------------------------------------------------------

PREAMBLE
The goals of the Open Font License (OFL) are to stimulate worldwide
development of collaborative font projects, to support the font creation
efforts of academic and linguistic communities, and to provide a free and
open framework in which fonts may be shared and improved in partnership
with others.

The OFL allows the licensed fonts to be used, studied, modified and
redistributed freely as long as they are not sold by themselves. The
fonts, including any derivative works, can be bundled, embedded, 
redistributed and/or sold with any software provided that any reserved
names are not used by derivative works. The fonts and derivatives,
however, cannot be released under any other type of license. The
requirement for fonts to remain under this license does not apply
to any document created using the fonts or their derivatives.

DEFINITIONS
"Font Software" refers to the set of files released by the Copyright
Holder(s) under this license and clearly marked as such. This may
include source files, build scripts and documentation.

"Reserved Font Name" refers to any names specified as such after the
copyright statement(s).

"Original Version" refers to the collection of Font Software components as
distributed by the Copyright Holder(s).

"Modified Version" refers to any derivative made by adding to, deleting,
or substituting -- in part or in whole -- any of the components of the
Original Version, by changing formats or by porting the Font Software to a
new environment.

"Author" refers to any designer, engineer, programmer, technical
writer or other person who contributed to the Font Software.

PERMISSION & CONDITIONS
Permission is hereby granted, free of charge, to any person obtaining
a copy of the Font Software, to use, study, copy, merge, embed, modify,
redistribute, and sell modified and unmodified copies of the Font
Software, subject to the following conditions:

1) Neither the Font Software nor any of its individual components,
in Original or Modified Versions, may be sold by itself.

2) Original or Modified Versions of the Font Software may be bundled,
redistributed and/or sold with any software, provided that each copy
contains the above copyright notice and this license. These can be
included either as stand-alone text files, human-readable headers or
in the appropriate machine-readable metadata fields within text or
binary files as long as those fields can be easily viewed by the user.

3) No Modified Version of the Font Software may use the Reserved Font
Name(s) unless explicit written permission is granted by the corresponding
Copyright Holder. This restriction only applies to the primary font name as
presented to the users.

4) The name(s) of the Copyright Holder(s) or the Author(s) of the Font
Software shall not be used to promote, endorse or advertise any
Modified Version, except to acknowledge the contribution(s) of the
Copyright Holder(s) and the Author(s) or with their explicit written
permission.

5) The Font Software, modified or unmodified, in part or in whole,
must be distributed entirely under this license, and must not be
distributed under any other license. The requirement for fonts to
remain under this license does not apply to any document created
using the Font Software.

TERMINATION
This license becomes null and void if any of the above conditions are
not met.

DISCLAIMER
THE FONT SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO ANY WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT
OF COPYRIGHT, PATENT, TRADEMARK, OR OTHER RIGHT. IN NO EVENT SHALL THE
COPYRIGHT HOLDER BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
INCLUDING ANY GENERAL, SPECIAL, INDIRECT, INCIDENTAL, OR CONSEQUENTIAL
DAMAGES, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
FROM, OUT OF THE USE OR INABILITY TO USE THE FONT SOFTWARE OR FROM
OTHER DEALINGS IN THE FONT SOFTWARE.

## BeautifulUI

`apple/Packages/BeautifulUI` (the chat's thoughts and tool chips) is BeautifulUI, a SwiftUI port of Beautiful UI
(https://www.beautifului.dev/), copied in as a local Swift package (`apple/Packages/BeautifulUI/VENDORED.md`).
The package directory keeps both licenses (`LICENSE`, `LICENSE-beautifului`) and `NOTICE`.

The web client (`web/`) uses Beautiful UI's design tokens, keyframes and component grammar directly (`web/src/styles.css`,
the views under `web/src/`), reimplemented in React against Jarvis's data; Beautiful UI's license is the one in
`apple/Packages/BeautifulUI/LICENSE-beautifului` (MIT, Copyright (c) 2026 Shane Levine).

BeautifulUI — MIT License, Copyright (c) 2026 ChrisPan

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
documentation files (the "Software"), to deal in the Software without restriction, including without limitation the
rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit
persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the
Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

Beautiful UI — MIT License, Copyright (c) 2026 Shane Levine

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
documentation files (the "Software"), to deal in the Software without restriction, including without limitation the
rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit
persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the
Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

## SwiftNIO SSH and its dependencies

The iOS and macOS apps link `apple/Packages/PersonaSSH` (the SSH tunnel to the hub, docs/network.md), which depends on
the packages below. `Glue` and `ChannelDataUnwrapper` in `apple/Packages/PersonaSSH/Sources/PersonaSSH/Handlers.swift`
follow the `GlueHandler` and `SSHWrapperHandler` of swift-nio-ssh's `NIOSSHClient` example.

| Package | Source | License |
| --- | --- | --- |
| swift-nio-ssh | https://github.com/apple/swift-nio-ssh | Apache License 2.0 |
| swift-nio | https://github.com/apple/swift-nio | Apache License 2.0 |
| swift-nio-transport-services | https://github.com/apple/swift-nio-transport-services | Apache License 2.0 |
| swift-crypto | https://github.com/apple/swift-crypto | Apache License 2.0 |
| swift-atomics | https://github.com/apple/swift-atomics | Apache License 2.0 |
| swift-collections | https://github.com/apple/swift-collections | Apache License 2.0 |
| swift-system | https://github.com/apple/swift-system | Apache License 2.0 |
| swift-asn1 | https://github.com/apple/swift-asn1 | Apache License 2.0 |

Copyright (c) Apple Inc. and the SwiftNIO / Swift project authors. Licensed under the Apache License, Version 2.0
(https://www.apache.org/licenses/LICENSE-2.0); each package's LICENSE.txt and NOTICE.txt apply.

## App Store Connect CLI

`scripts/release/asc_cli.py` downloads a pinned release of the App Store Connect CLI (`asc`) from its GitHub releases
and runs it; the binary is not part of this repository or of anything Persona ships.

https://github.com/rorkai/App-Store-Connect-CLI

MIT License

Copyright (c) 2026 Rudrank Riyam

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
documentation files (the "Software"), to deal in the Software without restriction, including without limitation the
rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit
persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the
Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

## openclaw-weixin

`server/internal/wechat/ilink` (the iLink client for the WeChat bot, docs/wechat.md) follows the wire behavior of Tencent's
openclaw-weixin v2.4.6: its API types and calls, the QR authorization, the media AES-128-ECB, and its markdown filter. The Go
code is written for Persona; nothing of the plugin's runtime is copied.

https://github.com/Tencent/openclaw-weixin

Tencent is pleased to support the open source community by making openclaw-weixin available.

Copyright (C) 2026 Tencent.  All rights reserved.

openclaw-weixin is licensed under the MIT.

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
documentation files (the "Software"), to deal in the Software without restriction, including without limitation the
rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit
persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the
Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
