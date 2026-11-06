# Encryption service version and authorization prompts

iPhone 18 Pro / iOS 27, isolated mock hub; no real account, password or sending.

| Scenario | Light Liquid Glass | Dark BeautifulUI |
| --- | --- | --- |
| Hub is missing the API | [Upgrade hub](light-unavailable-hub.png) | [Upgrade hub](dark-unavailable-hub.png) |
| Lark gateway is missing the API | [Upgrade Lark gateway](light-unavailable-gateway.png) | [Upgrade Lark gateway](dark-unavailable-gateway.png) |
| Separate native authorization is missing | [Turn on authorization](light-unavailable-authorization.png) | [Turn on authorization](dark-unavailable-authorization.png) |
| Recheck after upgrading | [Choose the public text](light-upgrade-recovered.png) | [Choose the public text](dark-upgrade-recovered.png) |

All 8 screenshots were checked one by one. The UI regression test confirms that all three failures explain the cause before entering the public-text page; after the service is restored, a recheck keeps the input and the encryption mode, then goes on to the candidate list. The 3 existing flows (unlock, wrong password, encrypted send, retry after failure, draft protection across entry points) also pass in light and dark. All writes go to the mock only.
