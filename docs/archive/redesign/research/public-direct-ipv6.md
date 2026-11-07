# Direct public connectivity, IPv6, TLS, and persistent-connection keepalive: research notes

> Research date: 2026-10-30 ("checked 2026-10-30" below refers to this date). Research only; no repository changes.
> Labels: **[Fact]** has a source (URL + lookup date); **[Assessment]** is my reasoning / recommendation; **[Not found]** means searched but no reliable source found.
> Privacy: all example addresses use documentation ranges `2001:db8::/32`, `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`, and `example.com`; no real hosts, addresses, or accounts.
> Background: the hub currently uses plaintext HTTP, listens on `:18660`, uses a Bearer token (WS also allows `?token=`), and pairs through `persona://pair?url=…&token=…` QR codes. It sends protocol-level pings every 20 seconds with a 15-second timeout (from the task brief; another agent is checking code details).

## Contents

0. Findings at a glance (added last)
1. IPv6 / residential broadband in China
2. How the hub discovers its public address
3. Handling address changes: DDNS and push
4. Multi-address routing on phones
5. Android 17 local-network permission and public IPv6
6. TLS
7. Hardening a personal server exposed to the Internet
8. Background persistent-connection keepalive
9. Recommended approach (summary)
10. Unresolved questions / required real-world tests
11. Sources

---

## 0. Findings at a glance

> This round changes only Android and the hub. iOS is read-only; related approaches are marked "when iOS follows up." Hub changes must let the existing iOS App (private-network plaintext, old QR format, single address) keep working without code changes (section 9.1).

- **Is direct public connectivity possible?**
  - Technically yes. IPv6 is widespread on mobile data in China (70.85% of mobile-network traffic at the end of 2025). Outbound phone connections are unaffected by carrier blocking of inbound phone IPv6.
  - The barriers are at the hub's home, in three layers:
    1. ONT / router IPv6 inbound firewalls are enabled by default and often offer only on / off;
    2. Prefixes change, more than twice daily in some regions;
    3. Some regions block 80/443.
  - Neither the App nor the hub can solve these for the user; only detect and guide (1.3–1.5).
- **Address discovery** (section 2):
  - The hub reads addresses and `IFA_FLAGS` through netlink, excluding temporary / deprecated / ULA / link-local addresses and virtual interfaces. `/proc/net/if_inet6` cannot expose flags at 0x100 or above.
  - Subscribe to address changes and also rescan periodically.
  - Reachability can be tested only externally: connecting locally to the machine's own address uses loopback and does not traverse the ONT.
- **Address changes** (section 3):
  - Abrupt renumbering immediately breaks old connections. "Hub pushes its new address" works only with an alternate channel (iOS APNs) or a smooth prefix transition.
  - **No domain is only a degraded mode**; recommend DDNS (own domain or free subdomain).
  - The two Chinese free DNS plans have a minimum TTL of 600 seconds, so DNS is a recovery mechanism; prefer literal addresses.
- **Routing** (section 4):
  - The hub sends multiple candidates; the phone probes concurrently, staggered by 250 ms following RFC 8305;
  - **A candidate wins only after TLS public-key pinning and `hub_id` verification**, since an old prefix may have been reassigned;
  - Cache winners by network, reprobe on network changes.
  - OkHttp 5 fast fallback races only different IPs for the same hostname; different URLs need a custom race.
- **Android 17 local-network permission** (section 5):
  - Under the official definition (IPv6 "Directly-connected routes") and android-17.0.0_r1 source (all on-link IPv6 prefixes count as local), **connecting to the hub's global IPv6 in the same /64 over home Wi-Fi still requires `ACCESS_LOCAL_NETWORK`**; switching to public IPv6 does not bypass it.
  - Public IPv4 reached through router hairpinning does not count as local. Requires real-device verification.
- **TLS** (section 6):
  - Default to a hub self-signed certificate with SPKI fingerprint in the QR code; Android pins the public key with a custom TrustManager (`CertificatePinner` does not work for self-signed certificates);
  - Add ACME DNS-01 when a domain exists (same key);
  - Let's Encrypt IP certificates (GA 2026-01, 6 days, HTTP-01 / TLS-ALPN-01 only, requiring open 80/443) are unsuitable.
- **Hardening** (section 7):
  - QR codes issue short-lived temporary tokens (unchanged format, compatible with old iOS pairing);
  - Aggregate authentication-failure rate limits by IPv6 /64;
  - Reject plaintext from public sources;
  - Avoid ports 80/443/8080/8443;
  - Randomize stable interface identifiers (not `::1`);
  - Use a host firewall as a fallback (disabling the ONT firewall exposes everything; Docker ports bypass ufw);
  - With a domain, use a wildcard certificate to avoid CT-log scanning (queries for a name appear 73 seconds to about 3 minutes after CT exposure, followed by port scans).
- **Keepalive** (section 8):
  - Android deep sleep prevents OkHttp client pings (`System.nanoTime` stops during deep sleep); `AllowWhileIdle` alarms run at most once every 9 minutes. **Background heartbeat must therefore be hub-initiated and negotiated per connection**: start at 4 minutes (below the known 5-minute NAT timeout), adapt by network, jitter only downward.
  - Keep 20-second + application-level ping in the foreground.
  - Current fixed 20-second pings plus Go's default 15-second TCP keepalive (if the hub does not override it) are too frequent for background persistent connections; power consumption is **not measured**.
- **Biggest unknowns** (section 10):
  - Chinese 4G/5G CGNAT and IPv6 firewall idle timeouts (only 2015 2G/3G data available);
  - Power use of a persistent foreground-service connection;
  - Behavior of Chinese vendor ROMs;
  - Android 17 local-network permission edge cases.

---

## 1. IPv6 / residential broadband in China

### 1.1 Overall figures (official)

- [Fact] As of 2025-12: 869 million active IPv6 users, 77.38% of Internet users; IPv6 accounts for 33.79% of all traffic, including **70.85% of mobile-network traffic** and 32.38% of fixed-network traffic. The overall IPv6 share rose to 34.55% in 2026-06.
  Source: Xinhua, "China's active IPv6 users reach 869 million", https://www.news.cn/info/20260729/e3793c4b1c3a428bb365a1f5a2b64513/c.html (checked 2026-10-30; reporting the *China IPv6 Development Report (2026)*, released 2026-07-28).
- [Fact] During the 14th Five-Year Plan, "IPv6 was enabled on 96% of home gateways nationwide." The 15th Five-Year Plan's eight metrics include "IPv6 connectivity rate among fixed-broadband users," but the press Q&A gives neither a definition nor target.
  Source: Cyberspace Administration of China, press Q&A for the *Implementation Plan for Deepening IPv6 Technological Innovation and Integrated Application (2026–2030)*, https://www.cac.gov.cn/2026-07/21/c_1786380790538547.htm (checked 2026-10-30).
- [Fact] Mobile networks: phones are generally **dual-stack** (IPv4 + IPv6). China Telecom's 5G user plane uses 464XLAT + DNS64 to support IPv6-only, but a 2021 case study says "the user plane currently mainly assigns both IPv6 and IPv4 addresses to terminals." Telecom's 2023 pilot was IPv6-only 5G IoT (NAT64/DNS64). China Mobile's 2025-08 G-SRv6 single-stack validation kept "content and terminals on IPv4/IPv6 dual stack," using single-stack only inside the network.
  Sources: https://www.cac.gov.cn/2021-11/25/c_1639439849620158.htm ; http://union.china.com.cn/zhuanti/txt/2025-08/06/content_43195686.html (checked 2026-10-30; the latter is a secondary report).
- [Not found] Public statements about any of the three major carriers migrating ordinary phone users to IPv6-only (464XLAT) at scale; China Unicom mobile-network single-stack status.

### 1.2 Residential broadband: global IPv6 availability and prefix length

- [Fact, user reports] Both ONT routing mode and a router dialing behind a bridged ONT can obtain global IPv6. Prefix lengths vary by province and carrier, commonly **/56 and /60**; Unicom often gives only **/64** (2023 V2EX survey: Guangzhou Telecom /60, Mobile /60, Unicom /64; Shanghai Telecom /56, Unicom /56, Mobile /60; Jiangsu Telecom /56→/60; Shanxi Unicom /64, etc.).
  Source: https://v2ex.com/t/930849 (checked 2026-10-30; 2023 post, unofficial).
- [Fact, user reports] With the ONT dialing (routing mode), many ONTs **delegate only one /64 downstream, or no PD** (enable the DHCPv6 server or bridge it to give the router /60). A 2026 blog reports a Telecom XG-140G-TF obtaining /60, but downstream DHCPv6-PD is disabled by default and requires telnet changes.
  Sources: https://www.v2ex.com/t/1017699 ; https://zhangshuqiao.org/2026-07/XG-140G-TF%E5%85%89%E7%8C%AB%E5%90%AF%E7%94%A8IPv6%20PD%E5%89%8D%E7%BC%80%E5%88%86%E5%8F%91/ (checked 2026-10-30).
- [Not found] Official carrier prefix-length policies.

**Impact on Persona [Assessment]**: the hub needs only **one global address for its own machine**, not a subnet. Prefix length and PD delegation to a secondary router do not affect "whether an address exists"; they affect **which layer holds the firewall and whether it can permit traffic** (1.4).

### 1.3 How often prefixes change

- [Fact, user reports] No universal interval. PPPoE redial usually changes the prefix, but it may change without redial too:
  - 2026-10 (posted around 10-19), a Shanghai Telecom user: router uptime 24.7 days, 57 historical prefixes retained in the kernel, "delegated prefixes change more than twice a day on average." The same thread reports upstream RA lifetimes "preferred ≈ 132000s (1.53 days) / valid ≈ 218000s (2.53 days)", but "PD changes much faster than the stated lifetime." Other users report "never changed", an Anhui Telecom customer having support extend the redial interval from 2 days to one month, and another user in the same city receiving "no PD at all."
    Source: https://www.v2ex.com/t/1250020 (checked 2026-10-30).
  - Other reports: Harbin Telecom described as "fixed"; changes within 24 hours for some users; in 2020, IPv4 forced redial intervals were about 48 hours for Guangdong Telecom, with no redial in Shanghai / Suzhou.
    Sources: https://www.v2ex.com/t/775397 , https://v2ex.com/t/706397 , https://www.nodeseek.com/post-118403-1 (checked 2026-10-30; all user observations).
- [Assessment] Design must assume **global IPv6 prefixes may change several times daily or remain stable for months**. IPv4 (if public) typically changes every 1–30 days, or not at all. The hub must detect address changes within seconds and notify / update DNS (sections 2, 3).

### 1.4 Inbound firewalls: ONTs / routers block by default

- [Fact] RFC 6092 (simple IPv6 CPE security) recommends blocking unsolicited inbound TCP by default (REC-34: reply to inbound SYN with ICMPv6 "administratively prohibited"), but also requires a one-click "transparent mode" (REC-49, which "MAY be the default").
  Source: https://www.rfc-editor.org/rfc/rfc6092.html (checked 2026-10-30).
- [Fact, user reports] In routing mode, Chinese ONTs have **IPv6 firewalls enabled by default** ("IPv6 Session" / "Ipv6Spi" switches). External ping works, but all TCP ports are filtered. Disabling varies by model: some allow ordinary administrator access, others require a superuser password / telnet / configuration-file edits. Some (certain Unicom SDN ONTs) have no switch, requiring bridging or replacing the ONT. **Most offer only on / off, not individual-port allowances** (some Huawei models have "IPv6 virtual host configuration" for per-port rules).
  Sources: https://www.chinadsl.net/thread-169397-1-1.html ; https://blog.csdn.net/u013084266/article/details/128358328 ; https://www.cnblogs.com/ie123610/p/19026984 ; https://www.v2ex.com/t/1026607 #20 (checked 2026-10-30).
- [Fact, user reports] Common approach: bridge the ONT, dial through your own router (OpenWrt / ASUS, etc.), and **allow destination address + port** on the router (OpenWrt wan→lan, family ipv6, ACCEPT; safer to match only the lower 64 address bits for the hub machine).
  Source: https://hk.v2ex.com/t/1014000 (2024-02, #18, #20) (checked 2026-10-30).
- [Assessment] This is the biggest **user-side barrier** to direct public connectivity: neither App nor hub can open the ONT firewall for the user. The product needs an **external reachability self-check** (sections 7, 9) and setup guidance for "allow inbound IPv6 to a port on the ONT / router." The hub machine's own firewall (ufw / nftables) must also permit it.

### 1.5 Inbound port blocking

- [Fact, user reports] Residential IPv6 blocking of 80/443 has **no nationwide rule; it varies by province, district, even neighborhood**: Beijing Mobile allows it in Haidian but blocks it in Changping; Anhui / Jiangsu Mobile block, Guangdong Mobile allows; Zhenjiang Mobile in Jiangsu started blocking IPv6 80/443 at 2025-03-27 13:00 (port 10086 still worked). Shanghai Mobile (no public v4) was reported open in both 2024 and 2026-09. A 2026-01 report says Telecom's Beijing Tongzhou v6 80/443 are open, but Hangzhou Binjiang / Shenzhen Longgang (with v4) are blocked. Some report common v6 ports being blocked after applying for public v4.
  Sources: https://www.v2ex.com/t/1026607 ; https://www.v2ex.com/t/1121687 ; https://v2ex.com/t/904810 ; https://www.v2ex.com/t/1130831 (checked 2026-10-30).
- [Fact, user reports] Public residential IPv4 commonly blocks 80/443 (sometimes 8080, 445, 25). A newly installed Telecom customer reported port 80 blocked in 2025-10.
  Source: https://linux.do/t/topic/1016716 (checked 2026-10-30).
- [Not found] Reports of high ports such as 18660 being blocked; official carrier port-blocking policies.
- [Assessment] **Do not use 80/443/8080/8443**. Retaining 18660 or a user-configurable high port (five digits) is more robust, but assume it might suddenly be blocked one day: provide self-checks and multiple candidate addresses. Not using 443 also loses the middlebox-friendly appearance of "TLS on 443" (usually fine).

### 1.6 Can mobile data reach IPv6 on someone else's residential broadband?

- [Fact, user reports] **Inbound IPv6** on mobile data is blocked in Mobile / Unicom core networks (Telecom allows it in most regions), but this affects only connections **to the phone**, **not phone-initiated outbound connections**.
  Sources: https://www.v2ex.com/t/843359 ; https://www.nodeseek.com/post-76979-1 (checked 2026-10-30).
- [Fact, user reports] After bridging the ONT and disabling the router firewall, a phone can reach a home PC's IPv6 port 8080 ("may vary by region").
  Source: https://hk.v2ex.com/t/1014000 #11 (checked 2026-10-30).
- [Fact, user reports] Isolated reports of IPv6 communication failures within the same carrier and province (for example, Zhejiang Telecom 5GC→5GC failing in 2022).
  Source: https://www.txrjy.com/thread-1240845-1-1.html (checked 2026-10-30).
- [Not found] Systematic reachability statistics for "mobile data → residential IPv6." [Assessment] It should work in the mainstream case, but the App's probes must decide, not assumptions.

### 1.7 Carrier NAT / stateful-firewall idle TCP timeouts

- [Fact, standard] RFC 5382 REQ-5: a NAT's "established connection idle-timeout" **must not be shorter than 2 hours 4 minutes**, and "transitory" timeout must not be shorter than 4 minutes. RFC 6092 REC-35 requires the same 2 hours 4 minutes for IPv6 CPE firewalls.
  Sources: https://www.rfc-editor.org/rfc/rfc5382.html ; https://www.rfc-editor.org/rfc/rfc6092.html (checked 2026-10-30). **Carriers often violate this in practice**, below.
- [Fact, measured, 2011, 73 cellular carriers mainly in the US] NetPiculet (SIGCOMM 2011) measured idle TCP timeouts: (0,5] minutes for 4 carriers, (5,10] for 7, (10,20] for 6, (20,30] for 8, and >30 for 48. One major US carrier (Carrier B) sent spoofed RSTs to close idle connections at 255 seconds (but used 1600 seconds for Google push port 5228). Energy: using a 1350 mAh battery and 3G power model, keepalive consumed >17% of battery capacity per day with timeouts <5 minutes, versus <2% at 30 minutes. The paper recommends 30 minutes.
  Source: https://conferences.sigcomm.org/sigcomm/2011/papers/sigcomm/p374.pdf (Table 5, §4.3.1, checked 2026-10-30; verified against locally extracted PDF text).
- [Fact, measured, around 2013–2015, 2G/3G] WeChat's team measured China Mobile 3G/2G **5 minutes**, China Unicom 2G **5 minutes**, China Telecom 3G **>28 minutes**. GCM heartbeat: Wi-Fi 15 minutes / data 28 minutes; WhatsApp 4 minutes 45 seconds; Line Wi-Fi 3 minutes 20 seconds, cellular 7 minutes.
  Source: "Android WeChat intelligent heartbeat scheme" (WeChat client development team, originally published 2015-08-17), https://cloud.tencent.com/developer/article/1030660 (checked 2026-10-30).
- [Fact, measured] Home gateways (2010, 34 devices) had a median TCP idle timeout around 60 minutes (IMC 2010, Hätönen et al.; median cited via RFC 9006 / another paper, original not read).
  Source: https://conferences.sigcomm.org/imc/2010/papers/p260.pdf (checked 2026-10-30; secondary citation).
- [Fact, measured, 2025] ITC 36 (2025) retested 23 home gateways (German market, including carrier-issued devices): **only 4 met RFC 5382's 124 minutes**, one was exactly 7200 seconds, and "the remaining 18 were far below the recommendation, the lowest just 1 minute." Figure 3 values are approximately 1, 3, 10, 10, 15 (×6), 20, 30 (×4), 40, 60, 60, 120, 124 minutes, and >1 day; timeout is unrelated to device age.
  Source: Neubert, Winter, Winkler, *On The Evolution of Home Gateways*, ITC 36 (2025), https://thomasez.folk.ntnu.no/itc36/itc2025-final4.pdf (§A.1 and Fig. 3, checked 2026-10-30; verified against locally extracted PDF text).
- [Fact] Linux conntrack defaults: `nf_conntrack_tcp_timeout_established` = 432000 seconds (5 days), UDP 30 / 120 seconds. Source: https://docs.kernel.org/networking/nf_conntrack-sysctl.html (checked 2026-10-30). Most Chinese routers are Linux-based, but vendors may change values. [Not found] Measured values for Chinese routers / ONTs.
- [Not found] **Authoritative 4G/5G-era measurements of IPv4 CGNAT TCP idle timeouts for China's three carriers**, or **any measurements / carrier documentation for IPv6 idle timeouts (no NAT, only stateful firewalls / 5GC UPF)**. Recent blogs repeating "Telecom about 2 minutes" / "Mobile about 5 minutes" give no methods and are not used.
- [Assessment] Until we have our own measurements, design background heartbeat for **worst-case IPv4 CGNAT ≈ 4–5 minutes** (keepalive interval ≤ about 4 minutes), without assuming IPv6 lasts longer; also probe adaptively (section 8). **Either endpoint can be the bottleneck**: carrier CGNAT / firewall at the phone, and ONT / router port mapping (IPv4) or conntrack entries for IPv6 stateful-firewall allowances at the hub. The 2025 measurements show home gateways may also last only minutes. Choose the shortest timeout of the two ends, measured rather than assumed.

## 2. How the hub discovers its public address

### 2.1 Linux address flags (facts)

- [Fact] `include/uapi/linux/if_addr.h` (Linux mainline, checked 2026-10-30 through GitHub API):
  `IFA_F_TEMPORARY` (= `IFA_F_SECONDARY`) 0x01, `IFA_F_NODAD` 0x02, `IFA_F_OPTIMISTIC` 0x04, `IFA_F_DADFAILED` 0x08, `IFA_F_HOMEADDRESS` 0x10, `IFA_F_DEPRECATED` 0x20, `IFA_F_TENTATIVE` 0x40, `IFA_F_PERMANENT` 0x80, `IFA_F_MANAGETEMPADDR` 0x100, `IFA_F_NOPREFIXROUTE` 0x200, `IFA_F_MCAUTOJOIN` 0x400, `IFA_F_STABLE_PRIVACY` 0x800 (mainline added `IFA_F_GLOBAL` 0x1000 in 2026-10, but it is for AF_PACKET multicast tables, not unicast addresses).
  Verbatim header comment: "IFA_FLAGS is a u32 attribute that extends the u8 field ifa_flags. If present, the value from struct ifaddrmsg will be ignored."
  Source: https://github.com/torvalds/linux/blob/master/include/uapi/linux/if_addr.h
- [Fact] **Pitfall**: `/proc/net/if_inet6` prints `(u8) ifp->flags` (`if6_seq_show` in `net/ipv6/addrconf.c`), so **`MANAGETEMPADDR` (0x100) and `STABLE_PRIVACY` (0x800) are invisible in procfs**. Full flags require the netlink `IFA_FLAGS` attribute.
  Source: https://github.com/torvalds/linux/blob/master/net/ipv6/addrconf.c (checked 2026-10-30).
- [Fact] Meanings (with ip-sysctl documentation):
  - `TEMPORARY`: temporary address generated by RFC 4941 privacy extensions, default preferred lifetime 1 day (`temp_prefered_lft` 86400), valid lifetime 2 days (`temp_valid_lft` 172800). **Do not give it to phones.**
  - `MANAGETEMPADDR`: the public SLAAC address from which temporary addresses are derived; itself stable.
  - `STABLE_PRIVACY`: RFC 7217 address (stable within a prefix; interface identifier changes with the prefix).
  - `DEPRECATED`: preferred lifetime has reached 0; the prefix is being retired. **Do not advertise it for new connections.**
  - `TENTATIVE` / `DADFAILED`: duplicate-address detection unfinished or failed; unusable.
  - `PERMANENT`: statically configured address (no lifetime).
  - Kernel defaults: `use_tempaddr` = 0 (privacy extensions off), `addr_gen_mode` = 0 (EUI-64). Distributions / NetworkManager often enable temporary addresses and stable-privacy instead.
  Source: https://github.com/torvalds/linux/blob/master/Documentation/networking/ip-sysctl.rst (checked 2026-10-30).
- [Fact] systemd-networkd: unspecified `IPv6PrivacyExtensions=` inherits networkd.conf and "defaults to \"no\"." `[IPv6AcceptRA] Token=` supports `eui64`, `static:ADDRESS` (a fixed interface identifier combined with the RA prefix), and `prefixstable` (RFC 7217).
  Sources: https://www.freedesktop.org/software/systemd/man/latest/systemd.network.html and https://github.com/systemd/systemd/blob/main/man/systemd.network.xml (checked 2026-10-30).

### 2.2 Reading them in Go

- [Fact] Standard library `net.Interfaces()` / `Addrs()` **does not expose flags or lifetimes**. `net/netip`'s `IsGlobalUnicast()` docs say: "It also returns true even if ip is in the IPv4 private address space or IPv6 unique local address space." Also use `IsPrivate()` (covers `fc00::/7`) to exclude ULA. Source: Go 1.26.4 `src/net/netip/netip.go` (local `go env GOROOT`, checked 2026-10-30).
- [Fact] `golang.org/x/sys/unix` (v0.47.0) already has all `IFA_F_*` constants (except 0x1000), `IFA_FLAGS` (8), `IFA_CACHEINFO` (6), `IfaCacheinfo{Prefered, Valid, Cstamp, Tstamp}`, and `RTNLGRP_IPV6_IFADDR` (9). Parse them yourself with `unix.NetlinkRIB(RTM_GETADDR, AF_INET6)` + `syscall.ParseNetlinkMessage`, with zero third-party dependencies. Source: local module cache `golang.org/x/sys@v0.47.0/unix/zerrors_linux.go`, `ztypes_linux.go` (checked 2026-10-30).
- [Fact] `github.com/vishvananda/netlink` v1.3.1 (released 2025-05-09):
  - `type Addr struct { *net.IPNet; Label string; Flags int; Scope int; Peer *net.IPNet; Broadcast net.IP; PreferedLft int; ValidLft int; LinkIndex int }`;
  - Parsing **reads `IFA_FLAGS` to override the u8 field** (`addr_linux.go`: `case unix.IFA_FLAGS: addr.Flags = int(native.Uint32(...))`), with lifetimes from `IFA_CACHEINFO`;
  - `AddrList(link Link, family int) ([]Addr, error)` (may return `ErrDumpInterrupted`, with incomplete results; retry);
  - Subscription: `AddrSubscribeWithOptions(ch chan<- AddrUpdate, done <-chan struct{}, AddrSubscribeOptions{ListExisting, ErrorCallback, ReceiveBufferSize, …})`, subscribing to `RTNLGRP_IPV4_IFADDR` + `RTNLGRP_IPV6_IFADDR`; `AddrUpdate{LinkAddress, LinkIndex, Flags, Scope, PreferedLft, ValidLft, NewAddr}`.
  Sources: v1.3.1 source downloaded from proxy.golang.org (`addr.go`, `addr_linux.go`, checked 2026-10-30); https://pkg.go.dev/github.com/vishvananda/netlink .
- [Assessment] Use vishvananda/netlink (mature, complete flags, subscriptions), or write an approximately 150-line RTM_GETADDR parser using only x/sys. The hub already monitors machines (`docs/server/machines.md`); reuse its rule that "`/sys/class/net/<name>` pointing under `/sys/devices/virtual/` means a virtual interface."

### 2.3 Address-selection rules [Assessment, based on the facts above]

Candidate IPv6 addresses must satisfy all of these:
1. Interface: `UP`, non-loopback, **non-virtual** (exclude docker0, br-*, veth*, virbr*, tailscale0, wg*, tun*, etc. under `/sys/devices/virtual/`; alternatively exclude `veth` / `bridge` / `tun` / `wireguard` / `vxlan` / `dummy` using netlink `LinkList`'s `Type()`). Allow a user-configured interface allowlist in `hub.json`.
2. Address: scope = universe (0), within `2000::/3`; exclude `fc00::/7` (ULA), `fe80::/10`, `2001:db8::/32` and `3fff::/20` (documentation), `2002::/16` (6to4), `2001::/32` (Teredo).
3. Flags: exclude `TEMPORARY`, `DEPRECATED`, `TENTATIVE`, `DADFAILED`; require `PreferedLft > 0` (0 = deprecated).
4. Multiple-candidate priority: `PERMANENT` (static) > SLAAC with fixed token > `MANAGETEMPADDR` / EUI-64 > `STABLE_PRIVACY`. Within a level, choose the longest valid lifetime. **Sticky**: keep the advertised address while it still meets the criteria to prevent flapping.
- **Recommend a fixed interface identifier for the hub** (`Token=static:::<fixed suffix>` or NetworkManager `ipv6.token`): prefix changes leave the suffix intact, allowing router rules by suffix (OpenWrt supports `dest_ip ::23/-64` to "match only the lower 64 bits", remaining effective after prefix changes).
  Source (OpenWrt): https://openwrt.org/docs/guide-user/firewall/fw3_configurations/fw3_ipv6_examples , "Dynamic prefix forwarding" (checked 2026-10-30). Note the original OpenWrt wording: "The negative netmask notation works only with OpenWrt firewall."
- **Do not select using "the default route's outgoing source address"** (`ip route get` / netlink `RouteGet`): with privacy extensions enabled it is temporary. If the hub machine uses a TUN proxy (as recorded in this repository's CLAUDE.md), the default route exits through the TUN interface.

### 2.4 TUN proxy / policy-routing pitfalls

- [Fact, issue reports] With sing-box `auto_route` enabled, users report "inbound requests arrive, but replies are pulled into TUN", leaving external clients retrying until timeout (sing-box #2167, OpenWrt; IPv6 affected since 1.11.1). IPv6 inbound fails when sing-box TUN takes over all server traffic (#3079); stopping sing-box incorrectly removes IPv6 policy rule `0: from all lookup local` (#4605).
  Sources: https://github.com/SagerNet/sing-box/issues/2167 , https://github.com/LIghtJUNction/sing-box/issues/3079 , https://github.com/SagerNet/sing-box/issues/4605 (checked 2026-10-30).
- [Assessment] If the hub machine runs a TUN proxy, **validate public inbound separately**. If needed, exclude the hub port / source address from the proxy (`route_exclude_address`, uid / port bypass, etc.; syntax varies). Include this in deployment self-checks.

### 2.5 Public IPv4: is it behind NAT?

[Assessment, standard approach] Check three layers:
1. **Does a local interface have public IPv4?** (not 10/8, 172.16/12, 192.168/16, 100.64/10, 169.254/16). If yes, the hub dials or connects directly to the Internet (uncommon at home).
2. **Router WAN address**: query the gateway with UPnP-IGD `GetExternalIPAddress`, NAT-PMP (RFC 6886), or PCP (RFC 6887). A WAN address in 100.64/10 or private space means **carrier CGNAT / double NAT; inbound IPv4 is effectively impossible** (unless a public IP is requested from the carrier, discussed below). PCP can also control IPv6 firewalls (RFC 6887 §1 lists "IPv6 and IPv4 firewall devices", §2.1 "IPv4 and IPv6 simple firewall control"), but support in Chinese ONTs is [Not found].
   Source: https://www.rfc-editor.org/rfc/rfc6887.html (checked 2026-10-30).
3. **Externally observed address**: STUN (RFC 8489) Binding or an HTTPS endpoint echoing the source address. If it differs from step 2's WAN address, another NAT layer exists.
- [Fact, user reports] Public IPv4 is increasingly hard to obtain: in 2021, Sichuan Telecom plans below 299 yuan got only private addresses; in 2025–2026 users still obtained Telecom public v4 through complaints / extra fees (for example, 10 yuan monthly), while most Mobile residential broadband does not offer it.
  Sources: https://www.v2ex.com/t/808536 , https://juejin.cn/post/7647395004020932654 , https://mao.fan/article/432 (checked 2026-10-30; personal experiences).
- [Assessment] IPv4 is only a bonus candidate: advertise it only after successful discovery (public address + usable port mapping); otherwise advertise IPv6 + private-network addresses. Automatic UPnP port opening has security concerns: off by default, explicit opt-in.

### 2.6 Watching for changes

- [Assessment]
  - Main path: netlink subscription to `RTNLGRP_IPV6_IFADDR` / `RTNLGRP_IPV4_IFADDR` (`AddrSubscribeWithOptions`, `ListExisting: true`), **debounce 2–5 seconds** before recomputing (SLAAC prefix changes produce consecutive TENTATIVE → normal, old-address DEPRECATED, and other messages).
  - Fallback: full rescan every 1–5 minutes (netlink multicast loses messages when the receive buffer fills, `ENOBUFS`); retry `AddrList` on `ErrDumpInterrupted`.
  - Public IPv4 changes generate no local events (the router WAN address changes), so poll STUN / UPnP.
  - On change: update DDNS (section 3), push new candidate addresses to connected clients over WS (sections 3, 4), log it.
- [Assessment] **The hub cannot test its own external reachability**: local connections to its global address use the `local` table and loop back on the machine (ip-route(8): "the destinations are assigned to this host. The packets are looped back and delivered locally", source https://man7.org/linux/man-pages/man8/ip-route.8.html , checked 2026-10-30), never traversing the ONT firewall. Only an external vantage point can test it:
  - Phone on cellular (the real path, most trustworthy);
  - Another node outside the same home network (the hub already manages computers over SSH; have it `connect` to the hub's public address and report);
  - Third-party port-check service (reveals address and port, not recommended by default).

## 3. Handling address changes: DDNS and push

### 3.1 DNS update APIs and TTLs (facts)

| Service | Update API | Minimum TTL | Source (checked 2026-10-30) |
| --- | --- | --- | --- |
| Cloudflare | `PATCH /zones/{zone_id}/dns_records/{dns_record_id}`, token permission "DNS Write", can be limited to one zone | 60–86400 seconds; `1` = auto; Enterprise can reach 30 | https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/edit/ |
| Alibaba Cloud DNS | `UpdateDomainRecord` (API documentation text not retrieved this time) | **Free and Personal plans: 600 seconds**; Enterprise flagship / premium: 1 second | https://help.aliyun.com/zh/dns/pubz-add-parsing-record |
| DNSPod / Tencent Cloud | API 3.0 `ModifyDynamicDNS` (`dnspod.tencentcloudapi.com`, Version 2021-03-23, parameters Domain, SubDomain, RecordId, RecordLine, Value, Ttl; default limit 20 calls/second, from search excerpts) | **Free: 600 seconds**, Professional: 60, Enterprise / Premium: 1; multiple records for the same host and routing line use the maximum TTL | https://docs.dnspod.cn/dns/help-ttl/ (updated 2025-05-21); https://cloud.tencent.com/document/api/1427/56158 |
| dynv6 (free subdomain) | `https://dynv6.com/api/update?zone=…&token=…&ipv6=…`, can also update the prefix; supports RFC 2136 + TSIG | Not checked | https://dynv6.com/docs/apis |
| deSEC (free) | `update.dedyn.io` dynDNS API, updates A / AAAA from source address or parameters | Not checked | https://desec.readthedocs.io/en/latest/dyndns/update-api.html |
| DuckDNS (free) | `…/update?domains=…&token=…&ipv6=…` (forum-user report only, official spec not retrieved) | Not checked | https://forum.netgate.com/topic/188368/duckdns-ipv6-update-url |

- [Fact] Existing open-source ddns-go supports over twenty providers, including Alibaba Cloud, Tencent Cloud, DNSPod, Cloudflare, Huawei Cloud, and deSEC, syncing every 5 minutes by default. IPv6 address sources are "API / interface / command"; interface mode selects by index (`@1`…) or regex. [Not found] Whether it excludes temporary addresses using `IFA_F_TEMPORARY`; a user running ddns-go in Docker reports IPv6 records changing "basically every 5 minutes" (no conclusive cause in the thread).
  Sources: https://github.com/jeessy2/ddns-go ; https://github.com/jeessy2/ddns-go/discussions/983 (checked 2026-10-30).
- [Assessment] Implement DDNS updates in the hub itself, without ddns-go: address selection must follow section 2's flag-aware rules, which generic DDNS tools handle poorly. Make providers pluggable (Cloudflare / Alibaba Cloud / DNSPod / generic HTTP callback URL / RFC 2136). Credentials only in `hub.json` (0600), token permission narrowed to DNS writes for one zone. In Go, consider `libdns` (Caddy's DNS abstraction, with cloudflare / alidns / tencentcloud implementations)—specific packages' maintenance status is **unverified**.

### 3.2 DDNS trade-offs [Assessment]

- Benefits:
  - Stable name lets a phone recover even if the address changes while offline;
  - Can obtain a publicly trusted certificate with Let's Encrypt DNS-01 (section 6);
  - The phone only needs to resolve again after a prefix change.
- Costs / risks:
  - Both Chinese free plans have a minimum TTL of **600 seconds**: resolvers caching an old record return the old address for up to 10 minutes after a change. The App **cannot rely only on DNS**: prefer literal addresses pushed by the hub; DNS is recovery when all candidates fail. The App can bypass system caches with its own DoH lookup (test whether resolvers honor TTL; [Not found] data on Chinese resolvers caching past TTL).
  - Public name: anyone querying DNS learns the home's address. Certificate Transparency (CT) logs publish certificate domains, attracting scanners (section 7). A wildcard certificate `*.example.com` (DNS-01) avoids exposing the specific subdomain.
  - Third-party dependency: unavailable DNS APIs delay updates; free DDNS availability and access speed from China are unknown.
  - Dual-stack records: publish A only after confirming external reachability. **Do not publish A** when residential IPv4 inbound is unusable, or clients (especially v4-first ones) time out on v4 first.

### 3.3 No domain: are the last successful address + hub-pushed updates enough?

- [Fact] PPPoE redial changes IPv4 and the IPv6 prefix together ("redial gets a new ipv4 and the v6 prefix changes; they are synchronized", V2EX 1250020 #19). Carrier-side prefix changes are "flash renumbering." RFC 8978 notes hosts retain and may use old-prefix addresses; RFC 4861 default lifetimes "could take hosts up to 7 days (one week) to deprecate", because unauthenticated RA cannot reduce valid lifetime below 2 hours.
  Sources: https://www.v2ex.com/t/1250020 ; https://www.rfc-editor.org/rfc/rfc8978.html (checked 2026-10-30).
- [Assessment] Therefore:
  1. **The old connection breaks immediately on change**; the hub cannot tell the phone its new address through it. Push updates work only when old and new addresses overlap (smooth renumbering, old prefix DEPRECATED before disappearing), or an alternate channel does not depend on the old address. **iOS has APNs** (hub sends outbound, unaffected by its home address change); Android without Google services **has no such alternate channel**.
  2. If the address changes while the phone is offline (App killed, long absence), it returns with only dead addresses: wait until home Wi-Fi allows reconnection over a private address, or scan again. Given section 1.3's reports (twice-daily prefix changes in some regions), this will happen **often** without a domain.
  3. The hub can also misjudge validity: after flash renumbering, an old address may remain non-deprecated locally (RFC 8978). Advertise **all** non-deprecated candidates, sorted by `IFA_CACHEINFO`'s `tstamp` (last RA refresh time), putting long-unrefreshed ones last. Phone-side concurrent probes decide (section 4).
- [Assessment] Conclusion: **last successful address + push without a name is only degraded mode**, suitable for mostly-at-home, occasional-away use. Reliable public connectivity needs at least one automatically updated name. Three tiers:
  1. Own domain + Cloudflare / Alibaba Cloud / DNSPod (recommended, also solves TLS certificates);
  2. Free DDNS subdomain (dynv6 / deSEC / DuckDNS, AAAA support): zero cost, but test availability and resolution speed in China;
  3. **Signed address announcement**: the hub signs its current candidate list with its identity key (the TLS key in section 6) and writes it to a user-chosen "bulletin board" (DNS TXT record, object-storage file, another user-owned server with a fixed address…). When all candidates fail, the App fetches it and verifies using the hub public key saved after pairing (the QR contains only a public-key hash; obtain and save the full key at the first TLS connection). Signatures ensure integrity without trusting storage, but add another dependency and expose the home's address in the announcement.
- [Assessment] (When iOS follows up) iOS can additionally receive new candidates via APNs background push (the hub already uses APNs; hub-side support can be added this round, iOS later). Delivery is not guaranteed and is rate-limited (Apple says "don't try to send more than two or three per hour", with 30 seconds of App execution). **The Apple page's original text was not retrieved directly**; recorded from widely cited wording, verify at https://developer.apple.com/documentation/usernotifications/pushing-background-updates-to-your-app .

## 4. Multi-address routing on phones

### 4.1 Happy Eyeballs essentials (facts)

- [Fact] RFC 8305 (Happy Eyeballs v2):
  - Connection Attempt Delay: "One recommended value for a default delay is 250 milliseconds", recommended minimum 100 ms, must not be below 10 ms, recommended maximum 2 seconds;
  - If A arrives first, recommended Resolution Delay waiting for AAAA is 50 ms; First Address Family Count for interleaving is 1;
  - With historical RTT, "SHOULD add a Destination Address Selection rule … that prefers addresses with lower RTTs", and prefer previously used addresses. **History must not be shared across network interfaces; clear it on network changes** (§4).
  Source: https://www.rfc-editor.org/rfc/rfc8305.html (checked 2026-10-30).
- [Fact] Happy Eyeballs v3 (SVCB/HTTPS records and QUIC) remains an IETF HAPPY working-group draft (`draft-ietf-happy-happyeyeballs-v3`, -03 in 2026-03, still discussed at IETF 126 in 2026-07), **not an RFC**.
  Sources: https://datatracker.ietf.org/doc/draft-ietf-happy-happyeyeballs-v3/ ; https://www.ietf.org/archive/id/draft-ietf-happy-happyeyeballs-v3-03.html (checked 2026-10-30).

### 4.2 Existing capabilities (facts)

**Android / OkHttp 5**
- [Fact] OkHttp 5 `fastFallback` is **enabled by default** (since 5.0.0-alpha.11; source comment: "Defaults to enabled, call with fastFallback = false to revert to 4.x behaviour"). It races **multiple addresses resolved for the same hostname**; the changelog says IPv6 gets a 250 ms head start. It races only IPs for one URL, **not different URLs (hosts / ports)**.
  Sources: https://github.com/square/okhttp/blob/master/CHANGELOG.md ; `okhttp3/OkHttpClient.kt` (master, checked 2026-10-30).
- [Fact] OkHttp `pingInterval` defaults to 0 (no client pings). When set: "If the server does not respond to each ping with a pong within interval, this client will assume that connectivity has been lost." It automatically replies pong to server pings (`RealWebSocket.onReadPing`). Sources: the same `OkHttpClient.kt`, `internal/ws/RealWebSocket.kt` (checked 2026-10-30).
- [Fact] Public `LinkProperties` APIs **do not include** `hasGlobalIpv6Address()` / `hasIpv6DefaultRoute()` / `isIpv6Provisioned()` (all `@hide` / SystemApi). Public APIs include `getLinkAddresses()` (`LinkAddress` exposes `getFlags()`, `getScope()`), `getRoutes()` (`RouteInfo.isDefaultRoute()`), `getDnsServers()`, `isPrivateDnsActive()` / `getPrivateDnsServerName()` (API 28), `getNat64Prefix()` (API 30), `getDhcpServerAddress()` (API 30).
  Source: https://developer.android.com/reference/android/net/LinkProperties (page too large; WebFetch retrieved only navigation. The list was compiled by the tool from SDK / AOSP knowledge and **must be checked against the actual SDK**, checked 2026-10-30).
- [Assessment] Compute "does this network have IPv6?" yourself: `linkAddresses` contains a universe-scope IPv6 address outside `fc00::/7`, without `DEPRECATED` / `DADFAILED`, and `routes` contains an IPv6 default route. More reliably: **actually connect**—the probe is the answer.

**iOS (when iOS follows up)**
- [Fact] `NWPath.supportsIPv6`: "A Boolean indicating whether the path can route IPv6 traffic." Also `isExpensive`, `isConstrained` (Low Data Mode); `NWPath` since iOS 12. Source: https://developer.apple.com/tutorials/data/documentation/network/nwpath.json (checked 2026-10-30).
- [Fact] Apple: on DNS64/NAT64 networks, since iOS 9 NSURLSession / CFNetwork synthesize IPv6 from IPv4 literals locally; direct sockets do not (Apple engineer's forum explanation). Apple recommends DNS names over literals. Sources: https://developer.apple.com/library/archive/documentation/NetworkingInternetWeb/Conceptual/NetworkingOverview/UnderstandingandPreparingfortheIPv6Transition/UnderstandingandPreparingfortheIPv6Transition.html ; https://developer.apple.com/forums/thread/5643 ; https://developer.apple.com/support/ipv6 (checked 2026-10-30; from search excerpts).
- [Not found] Whether `NWConnection` also synthesizes IPv4 literals (not stated in Apple docs).

### 4.3 Recommended routing algorithm [Assessment]

1. **Candidate sources**:
   - Hub-provided list (new authenticated endpoint, new WS `hello` field; **not unauthenticated `/health`**, see 9.1): each has `url`, `kind` (`lan` / `ipv6` / `ipv4` / `name` / `vpn`), `family`, `port`, and `seen_at` (when the hub last confirmed it on an interface / refreshed by RA);
   - Last successful address on the **current network**;
   - A / AAAA from the configured domain, if any;
   - Address from the pairing QR code.
2. **Filter and rank by current network**:
   - Do not try private / ULA addresses on cellular (guaranteed timeout; also avoids Android 17 local-network permission);
   - Without current-network IPv6, put IPv6 literals last rather than excluding them (detection may be wrong);
   - On Wi-Fi, prefer private-network addresses (same home);
   - Order: last success on this network > private network > global IPv6 > domain > public IPv4.
3. **Staggered concurrency**: start the first immediately, then another every 250 ms, **at most 3–4 concurrently**. Each must complete "TCP + TLS + certificate pin verified + `hub_id` from `GET /health` matches pairing" to win. Use the first winner for WebSocket and REST, cancel others. Overall timeout about 5 seconds on Wi-Fi, 8 seconds on cellular, then backoff.
4. **Cache**: remember winners by "network identity." Android SSID access needs location permission (Android 10+ / target 31+ `NetworkCallback` redacts by default, requiring `FLAG_INCLUDE_LOCATION_INFO` + location permission; **official wording not retrieved**). Instead key by a hash of "transport type + gateway / DHCP server address + local IPv6 /64 prefix", without location permission. RFC 8305 requires clearing history on network changes; separate per-network history meets this.
5. **Reprobe when**:
   - `registerDefaultNetworkCallback` fires `onAvailable` / `onLost`, `onLinkPropertiesChanged` (address change), or `onCapabilitiesChanged` (`VALIDATED` / `CAPTIVE_PORTAL` change);
   - Current connection breaks;
   - Hub pushes a new candidate list;
   - Periodic "upgrade check": for example, when using IPv4 or a domain on cellular, probe a better candidate every 10–30 minutes in the background and switch only if it wins (keep the old connection during the switch).
6. **Verify identity before sending tokens**: a carrier may have reassigned the hub's old prefix, placing someone else's machine at its old address. **Sending a Bearer token over plaintext HTTP to an old address hands it to a stranger**—the most direct reason public connectivity requires TLS + pinning (section 6).

### 4.4 Pitfalls (facts + assessments)

- **DNS64 / NAT64 / 464XLAT**: [Fact] All three Chinese carriers are piloting IPv6-only / NAT64; Telecom's 5G user plane uses 464XLAT + DNS64 (1.1). Android has built-in CLAT (clatd) since 4.3, providing fallback for IPv4 literals on IPv6-only networks (sources: https://github.com/ddrown/android_external_android-clat ; T-Mobile 464XLAT report https://conference.apnic.net/data/37/464xlat-apricot-2014_1393236641.pdf , checked 2026-10-30). [Assessment] The hub's public IPv4 literal remains reachable from an IPv6-only phone through CLAT / NAT64; AAAA-only domains are unaffected. Do not cache DNS64-synthesized `64:ff9b::/96` addresses as "the hub's IPv6."
- **Captive portals**: unauthenticated Wi-Fi can hijack or redirect connections. [Assessment] If `NET_CAPABILITY_CAPTIVE_PORTAL` is present or the network is not `VALIDATED`, show "Sign in to this Wi-Fi first", rather than retrying forever. TLS pinning blocks hijacking and prevents sending tokens.
- **Wi-Fi has IPv6, cellular does not (or vice versa)**: refilter on every network change using 4.3 step 2. Do not carry a Wi-Fi IPv6 success over to cellular.
- **Reaching your own public IPv4 at home requires router NAT hairpinning**, unsupported by many home routers. Prefer private addresses or global IPv6 at home (direct same-/64 traffic, no NAT).
- **Another proxy / VPN App on the phone**: fake-ip modes such as Clash resolve domains into `198.18/15` (benchmarking range) and take over traffic, breaking direct paths or adding detours. [Assessment] The user manages VPN themselves. The App only shows "Currently using VPN (`TRANSPORT_VPN`)" in diagnostics and prefers hub-provided literal addresses (bypassing fake-ip DNS).
- **IPv6 path-MTU black holes**: [Fact, user reports] A Shanghai Telecom IPv6 issue thread suggested clamping v6 MSS to 1420 (https://www.v2ex.com/t/1250020 #18, #28, checked 2026-10-30). [Fact] Setting Linux `TCP_MAXSEG` before connection establishment changes the advertised MSS (tcp(7), https://man7.org/linux/man-pages/man7/tcp.7.html , checked 2026-10-30). [Assessment] Symptom: "TCP connects, TLS handshake stalls" (large certificate-chain packets lost). The hub public listener could optionally use a smaller MSS (1360–1420, **not measured**); report handshake timeouts separately in probes.
- **Carrier DNS**: [Not found] Systematic data on Chinese carrier resolvers caching beyond TTL. The App can use its own DoH lookup for domain candidates (section 3).

## 5. Android 17 local-network permission and public IPv6

### 5.1 Official documentation wording (facts)

- [Fact] "Local Network Definition" (last updated 2026-03-06):
  > A local network in this project refers to an IP network that utilizes a broadcast-capable network interface, such as Wi-Fi or Ethernet, but excludes cellular (WWAN) or VPN connections.

  IPv4: `169.254/16`, `100.64/10`, `10/8`, `172.16/12`, `192.168/16`; IPv6: "Link-local", "**Directly-connected routes**", "Stub networks like Thread", "Multiple-subnets"; plus multicast (224/4, ff00::/8) and limited broadcast (all ones). The page does not mention global IPv6, NAT hairpinning, or loopback addresses.
  Source: https://developer.android.com/privacy-and-security/local-network-definition (checked 2026-10-30).
- [Fact] "Local network permission" (last updated 2026-10-02):
  - "Starting in Android 17, local network protections are mandatory and enforced for apps targeting Android 17 or higher"; target ≤ 36 receives it implicitly through `INTERNET`; "don't add ACCESS_LOCAL_NETWORK to your manifest";
  - Restrictions "apply to **all networking APIs** … Cronet and OkHttp"; outbound / inbound TCP and UDP require permission;
  - Without permission, "TCP Connections will typically result in a **timeout error**"; NDK `android_getnetworkblockedreason(sockFd)` returns `ANDROID_NETWORK_BLOCKED_REASON_LNP`;
  - Exceptions: local DNS server port 53; Output Switcher;
  - The page says nothing about global IPv6 or NAT hairpinning, listing only "Discrepancies in LAN access" under "Bugs."
  Source: https://developer.android.com/privacy-and-security/local-network-permission (checked 2026-10-30).

### 5.2 AOSP implementation (facts, android-17.0.0_r1 source)

- [Fact] `ConnectivityService.getEffectiveLocalPrefixes(lp)` takes the on-link prefix of **every local address on the link**, adding it to the local set if `isPrefixLocal` is true. For IPv6, `isPrefixLocal` implements `return prefix.getPrefixLength() != 0;`: **any nonzero-length IPv6 on-link prefix is local, including global /64**. IPv4 counts only on-link prefixes within `IPV4_LOCAL_PREFIXES` (169.254/16, 100.64/10, 10/8, 172.16/12, 192.168/16). Route-based rules (Thread, non-default routes covering local addresses) apply only when `isLnpDeveloperOptInEnabled()` is true. The code still has `// TODO: Add all directly-connected routes.`
  Source: https://android.googlesource.com/platform/packages/modules/Connectivity/+/refs/tags/android-17.0.0_r1/service/src/com/android/server/ConnectivityService.java (checked 2026-10-30, downloaded with `?format=TEXT`).
- [Fact] BPF (`bpf/progs/netd.c`) does longest-prefix matching in `local_net_access_map` (LPM trie) using `(interface, remote address, protocol, port)`: **no match means allow**. A blocked match drops packets if the uid lacks `ACCESS_LOCAL_NETWORK`.
  Source: https://android.googlesource.com/platform/packages/modules/Connectivity/+/refs/tags/android-17.0.0_r1/bpf/progs/netd.c (checked 2026-10-30).
- [Fact] Diagnostic command `adb shell dumpsys connectivity trafficcontroller` lists prefixes marked local per interface (usage in a sing-box issue). Source: https://github.com/SagerNet/sing-box/issues/4676 (checked 2026-10-30).

### 5.3 Implications for Persona [Assessment, based on 5.1 documentation + 5.2 source; requires real-device verification]

| Scenario (target 37, permission not granted) | Counts as local network? | Basis |
| --- | --- | --- |
| Home Wi-Fi, hub private IPv4 in the same subnet (for example, 192.168.x.0/24) | **Yes**, blocked | IPv4 definition; source on-link prefix |
| Home Wi-Fi, hub **global IPv6 in the same /64 as the phone** | **Yes**, blocked | "Directly-connected routes" definition; source treats all IPv6 on-link prefixes as local |
| Home Wi-Fi, hub global IPv6 in **another /64** behind a secondary router | **No** in 17.0.0_r1 source (outside on-link prefix; route rules not enabled by default); "Multiple-subnets" definition may cover it later | Ambiguity between source and definition |
| Home Wi-Fi, hub **public IPv4** through router NAT hairpinning | **No** (destination outside all local prefixes), but many routers lack hairpinning | Source; public addresses absent from IPv4 definition |
| Home Wi-Fi, hub private IPv4 in **another subnet** (for example, 192.168.10.x, phone on 192.168.1.x/24) | **No** in source (only on-link prefixes blocked), but definition lists all of 192.168/16 | Source / docs mismatch; real-device behavior decides |
| Cellular, any public hub address | No | Definition excludes WWAN |
| VPN to hub (for example, Tailscale 100.x) | Definition excludes VPN; source marks only the VPN interface's own on-link prefix local. A Tailscale /32 contains only itself; **a VPN configured with /10 or similar makes the entire subnet local**. **Not tested.** | Definition + source inference |

- **Key point**: "Use public IPv6 to bypass local-network permission" **does not work at home**—global addresses in the same /64 are still local. Android targeting 37 **must implement the permission flow** (android.md 5.1 already designs this: request after scanning; on denial show "Allow Persona to access the local network").
- **Fallback on denial** [Assessment]:
  1. Use public IPv4 if the hub has it and the router supports hairpinning (not local);
  2. Use global IPv6 if the hub is in another /64;
  3. Last resort: explicitly connect to the hub's public address over cellular (`ConnectivityManager.requestNetwork(TRANSPORT_CELLULAR)` obtains a `Network`; OkHttp uses `network.socketFactory`). Using cellular at home costs data and power; only with user confirmation.
- **Probes must distinguish "blocked by local-network permission" from "hub down"**: both look like TCP timeouts. At target 37, combinations of local-candidate timeout versus public-candidate success / failure on Wi-Fi, plus `checkSelfPermission(ACCESS_LOCAL_NETWORK)`, are enough to infer it. Use NDK `android_getnetworkblockedreason` for greater precision.
- **Play policy**: target 37+ permission requests must be declared in Play Console, mandatory from 2027-08-25 (already recorded in `/tmp/persona-redesign/research/android-tech.md`, not reverified here).

## 6. TLS

### 6.1 Why direct public connectivity requires TLS [Assessment]

- A plaintext Bearer token can be captured at any hop (carrier, public Wi-Fi, hijacked DNS);
- More concretely, after a prefix change, **the hub's old address may belong to someone else** (4.3 step 6). A phone reconnecting there sends its token to a stranger;
- "Verify peer identity before sending the token" is therefore mandatory on public paths. Keep private-network plaintext for compatibility (section 9), but new clients prefer TLS whenever supported by the hub.

### 6.2 Option A: hub self-signed certificate + SPKI SHA-256 public-key fingerprint in pairing data, pinned by the phone

**Facts**
- [Fact] OkHttp: "CertificatePinner can not be used to pin self-signed certificate if such certificate is not accepted by javax.net.ssl.TrustManager." **`CertificatePinner` alone cannot handle self-signed certificates**, because the default TrustManager rejects them first. It configures pins by hostname / wildcard pattern, using `sha256/<base64>` (SPKI).
  Source: class comment in OkHttp master `okhttp3/CertificatePinner.kt` (checked 2026-10-30).
- [Fact] OkHttp's default `OkHostnameVerifier` checks an IP literal only against certificate SAN **iPAddress** entries (type 7), comparing normalized values. Source: `okhttp3/internal/tls/OkHostnameVerifier.kt` (checked 2026-10-30). → Connecting by IP without that IP in SAN fails default hostname verification.
- [Fact] Google Play rejects "trust everything" `X509TrustManager`s and `onReceivedSslError` handlers that unconditionally call `SslErrorHandler.proceed()`. It requires throwing `CertificateException` for unexpected certificates, and proceeding only when expected, otherwise cancelling. Lint `TrustAllX509TrustManager` scans bytecode, including dependencies.
  Sources: https://googlesamples.github.io/android-custom-lint-rules/checks/TrustAllX509TrustManager.md.html ; https://experienceleague.adobe.com/en/docs/experience-cloud-kcs/kbarticles/ka-17526 (quotes Google's notification) (checked 2026-10-30).
- [Fact] Apple (when iOS follows up): manual trust evaluation handles `NSURLAuthenticationMethodServerTrust` in `urlSession(_:didReceive:completionHandler:)`, uses `.useCredential(URLCredential(trust:))` after verification, otherwise `.cancelAuthenticationChallenge`. "You cannot loosen server trust requirements for an ATS-protected domain, but you can tighten them." Recommendations: "Compare the server's credentials against a public key, instead of storing a single certificate"; "Use a set of keys or CAs, so you can rotate." Existing iOS `Info.plist` has `NSAllowsArbitraryLoads = true` (`ios/Persona/Resources/Info.plist`), so ATS does not restrict loosening trust.
  Source: https://developer.apple.com/documentation/foundation/performing-manual-server-trust-authentication (JSON version, checked 2026-10-30).

**Hub implementation [Assessment]**
- Generate a long-lived **hub identity key** (ECDSA P-256) on first run. Like tokens under repository rules, keep it only on the hub machine (`hub.json` or a 0600 file in the same directory), not in the database.
- Sign a self-signed certificate with it (for example, 1-year validity, automatically renewed before expiry with **the same key**—public-key pinning survives certificate renewal). SAN contains a fixed name such as `<hub_id>.persona.invalid`, not changing IPs.
- QR / pairing data includes `fp=sha256/<base64 SPKI>` and `hub_id`. `/health` and WS `hello` also publish current `fp` and next-key `fp_next` (announced rotation; phones trust both).
- IP-literal connections send no SNI (RFC 6066 §3: "Literal IPv4 and IPv6 addresses are not permitted in \"HostName\"", https://www.rfc-editor.org/rfc/rfc6066.html , checked 2026-10-30). Go `crypto/tls` `GetCertificate` can return this certificate when `ServerName` is empty.

**Android implementation [Assessment]**
- Use a hub-specific `OkHttpClient`:
  - Custom `X509TrustManager` (prefer `X509ExtendedTrustManager`): SHA-256 `chain[0].publicKey.encoded` (Java SubjectPublicKeyInfo DER), constant-time comparison against the paired pin set, throwing `CertificateException` on mismatch—meets Play requirements;
  - Relax `hostnameVerifier` only for this client (identity verified by public key; changing IPs cannot rely on SAN);
  - Optional `CertificatePinner`, though it adds nothing for self-signed certificates.
- Alternatively, okhttp-tls `HandshakeCertificates.Builder().addTrustedCertificate(hubCert)` trusts the hub certificate as a root, but pins the whole certificate, requiring re-pairing on renewal. Public-key pinning is better.
- **Every hub request must use this client**: REST, WebSocket (same `OkHttpClient`, same TLS settings), image loading (inject it into Coil / Glide), uploads / downloads.
- **WebView does not use OkHttp**: if the in-App browser loads hub-proxied resources (Lark media, etc.), fetch them through OkHttp in `shouldInterceptRequest`, or verify the public key through `SslError.getCertificate().getX509Certificate()` (API 29+) in `onReceivedSslError` before `proceed()`.
- Network Security Config `<pin-set>` is static and domain-based; hub addresses are known only at pairing, so it is **unsuitable**.

**Pitfalls [Assessment]**
- The fingerprint must come from the QR. Manual address entry without scanning means TOFU (trust on first use): have the user compare the first fingerprint characters on the hub terminal, or use "one-time pairing code + independently computed confirmation code" (section 7).
- Losing the key (reinstalling the hub machine) requires every phone to re-pair: document clearly, or support a "new-key announcement signed by the old key."

### 6.3 Option B: Let's Encrypt IP-address certificates

- [Fact] GA since 2026-01-15: "IP address certificates must be short-lived certificates", valid **160 hours** (about 6 days), using only the `shortlived` profile. Supports IPv4 and IPv6.
  Sources: https://letsencrypt.org/2026/01/15/6day-and-ip-general-availability ; https://letsencrypt.org/docs/profiles/ (checked 2026-10-30).
- [Fact] For IP certificates, "you can't use the DNS challenge method" and "only the http-01 and tls-alpn-01 methods can be used." HTTP-01 "can only be done on port 80", TLS-ALPN-01 on **443**.
  Sources: https://letsencrypt.org/2025/07/01/issuing-our-first-ip-address-certificate ; https://letsencrypt.org/docs/challenge-types/ (checked 2026-10-30).
- [Assessment] **Unsuitable for Persona**:
  - Public validation requires 80 or 443, often blocked on Chinese residential broadband (1.5), and another exposed port;
  - IP-bound certificates require renewal on each prefix change, possibly several daily. Six-day validity requires the validation port to remain open;
  - Between prefix change and renewal, phones see mismatched certificates.

### 6.4 Option C: ACME DNS-01 with a domain

- [Fact] DNS-01 supports wildcard certificates and does not require public server reachability. "Keeping API credentials on your web server is risky"; use restricted tokens. Source: https://letsencrypt.org/docs/challenge-types/ (checked 2026-10-30).
- [Fact] Let's Encrypt validity is shrinking: default **64 days** from 2027-02-10 (authorization reuse 30 → 10 days), 45 days in 2028 (authorization reuse 7 hours). Use ARI or renew around ⅔ of lifetime, not a hardcoded "60 days."
  Source: https://letsencrypt.org/2026/10/07/64-day-certs (checked 2026-10-30).
- [Fact] Go `github.com/go-acme/lego/v4` accepts a private key through `ObtainRequest.PrivateKey` ("A new private key is generated for every invocation of the function Obtain. If you do not want that you can supply your own private key"), and provides `ObtainForCSR`. DNS providers include `cloudflare`, `alidns`, `dnspod` (`tencentcloud` not confirmed this time).
  Sources: https://pkg.go.dev/github.com/go-acme/lego/v4/certificate ; https://go-acme.github.io/lego/dns/ (checked 2026-10-30). lego's docs recently changed CLI style; verify the module path remains `/v4` before adoption.
- [Assessment] Obtain certificates using the **hub identity key**, reused on renewal. Phones keep the same SPKI pin, while the certificate also chains to public trust (WebView, browsers, curl work directly). Use wildcard `*.example.com` to avoid exposing the hub's subdomain in CT. Share a zone-restricted DNS token with DDNS.

### 6.5 Recommendation [Assessment]

1. **Default: option A (self-signed + SPKI pinning)**. No dependency on domain, ports, or CA. Pairing already requires scanning; including the fingerprint costs nothing. Implement in the first Android version.
2. **Optional enhancement: option C**, enabled automatically when a domain and DNS token are configured. Replace the certificate with a public-CA-signed one but **keep the same hub key**, leaving phone pinning unchanged and adding a "name" candidate.
3. **Do not implement option B** (IP certificates).
4. Compatibility: keep plaintext `:18660` for private networks and the existing iOS App. TLS uses a separate configurable port (for example, `:18661`), section 9.

## 7. Hardening a personal server exposed to the Internet

### 7.1 Sources of exposure (facts)

- [Fact] Brute-forcing an IPv6 /64 is infeasible (RFC 7707 §4.2: "brute-force address-scanning attack of an entire /64 network would be infeasible"), but addresses leak through:
  - Public DNS (§5.1.1: hosts published in DNS "will remain open to probing"), including DDNS names;
  - Predictable interface identifiers: low-byte addresses (`::1`, `::2`) have a search space around 2^8–2^16 (§4.1.3.1), with 56%–93% of measured servers using them (§4.1.5); EUI-64 (derived from MAC, about 2^24 with a known vendor OUI);
  - Application leaks (mail headers, P2P, etc.).
  Source: https://www.rfc-editor.org/rfc/rfc7707.html (checked 2026-10-30).
- [Fact] Certificate Transparency (CT): an IMC 2018 honeypot whose subdomain leaked only through CT saw first DNS queries **73 seconds to about 3 minutes after issuance**, then port scans. EuroS&P 2023 reproduced this over 200 days, with scans surging almost immediately after issuance and renewal.
  Sources: https://dl.acm.org/doi/10.1145/3278532.3278562 ; https://sites.cs.ucsb.edu/~chris/research/doc/eurosnp23_certvuln.pdf (checked 2026-10-30).
- [Fact] Most ONTs have only an IPv6 firewall on / off switch (1.4). Turning it off directly exposes **all** hub-machine services listening on `::` (SSH, Lark gateway, other containers…). Docker docs: "Docker and ufw use firewall rules in ways that make them incompatible"; published ports "gets diverted before it goes through the ufw firewall settings." This repository's Postgres container already binds only to `127.0.0.1` (`scripts/db.sh`).
  Source: https://docs.docker.com/engine/network/packet-filtering-firewalls/ (checked 2026-10-30).
- [Assessment] Therefore:
  1. **Use a random 64-bit fixed interface identifier**, not low-byte `::1` / `::1234` or EUI-64 (also applies to fixed tokens recommended in 2.3);
  2. With a domain, use wildcard certificates to keep the hub subdomain out of CT;
  3. Changing ports blocks only the crudest scanning; real protection is TLS (section 6) and authentication below.

### 7.2 Hardening checklist [Assessment, basis given per item]

1. **One-time pairing credentials instead of the permanent main token in QR codes**
   - Current behavior (`docs/protocol/protocol.md`, "Authentication"; code checked by another agent): QR codes / `personad hub pair` print `hub.json`'s main token (all capabilities, never expires). Per-device `jd_…` tokens already exist (`docs/protocol/protocol.md`, "Per-device credentials").
   - Change to: `hub pair` generates a **high-entropy (≥128-bit) temporary pairing token**, stores its hash, with a **short lifetime (10–15 minutes)** and one device-token exchange. After exchange, allow a grace period before invalidation (for example, 10 minutes: iOS `pair()` concurrently connects WS and registers push. Which token `PushRegistrar` uses is **unverified**; too short a grace period may break push registration). CLI revocation supported. QR keeps `url` + `token` (**same format**) with added fields (`fp`, `hub_id`, `alt`…).
   - **Existing iOS compatibility (flow verified in iOS code)**: after scanning, iOS uses this token for `GET /api/v1/machines` validation, WS connection, and push registration, then exchanges it through `POST /api/v1/auth/tokens` (`pair()` in `ios/Persona/App/AppModel.swift`, `upgradeCredentials()` in `DeviceCredentials.swift`). `ServerConfig(pairURL:)` reads only named `url` and `token`, ignoring unknown parameters. Temporary tokens must therefore **work as full Bearer tokens during their lifetime**, not only on the exchange endpoint, or old iOS cannot pair. Failed iOS exchange retries at next launch, when the temporary token may have expired; re-scan then (acceptable, or extend unexchanged temporary tokens to 24 hours).
   - Keep `personad hub pair --legacy` (prints main token) for CLI / special cases.
   - Manual entry without scanning: short code (for example, 8 digits), strict rate limits, displayed only on the hub terminal; compare fingerprint prefix on first connection (6.2, "Pitfalls").
2. **Authentication-failure rate limits / bans**
   - Aggregate by source: individual IPv4 address, **IPv6 /64** (a residential subscriber receives /56–/64, section 1.2; per-address IPv6 limiting is ineffective). Escalate repeated triggers to /56.
   - Example: 10 responses of 401 from one source within 1 minute → 15-minute ban, increasing exponentially. Keep bans in memory + periodically persist (hub may be redeployed anytime; CLAUDE.md records three deployments in two minutes).
   - Device and temporary pairing tokens are high-entropy random values, not practically guessable. Limits mainly **reduce noise, log flooding, and resource use**.
   - All 401s use identical bodies and timing, without distinguishing nonexistent from revoked tokens.
3. **Reject plaintext requests from public addresses**
   - Plaintext accepts only loopback, RFC 1918, ULA (`fc00::/7`), link-local, `100.64/10` (Tailscale and similar VPNs), and `hub.json` allowlisted sources. Other plaintext requests get 403 explaining TLS is required; **do not read Authorization or log tokens**.
   - Trust the TCP connection itself, **not `X-Forwarded-For`**, unless an upstream proxy is explicitly configured.
   - **`100.64/10` is ambiguous**: both Tailscale and carrier CGNAT use it. If the hub's residential connection is behind CGNAT, other subscribers in the same CGNAT domain may also appear from `100.64/10`. Check the **local endpoint** too (incoming interface / local address); only traffic arriving through a LAN interface, VPN interface such as `tailscale0`, or loopback counts as private.
   - TLS accepts any source.
4. **Logs**:
   - Keep current behavior: method, path, status, duration only, no query, body, token (`docs/server/server.md`);
   - Retain WS `?token=` only for old clients; new clients use `Authorization` (Android OkHttp supports headers);
   - Authentication-failure logs record source prefix (/64) and count, not submitted credentials;
   - Count public scan noise separately, without per-request logs.
5. **Ports**:
   - Avoid 80 / 443 / 8080 / 8443 (1.5: commonly blocked and scanned);
   - Configurable high default TLS port;
   - Do not allow plaintext 18660 through the router (private-network use only).
6. **HTTP server parameters**:
   - Set `ReadHeaderTimeout`, `IdleTimeout`, `MaxHeaderBytes` on the public listener (Go `net/http` has no default header-read timeout: "If zero, the value of ReadTimeout is used. If negative, or if zero and ReadTimeout is zero or negative, there is no timeout", source Go 1.26.4 `src/net/http/server.go`, checked 2026-10-30);
   - Maximum concurrent connections per source;
   - Maximum WS message size.
7. **Host layer**:
   - Hub-machine nftables rejects external inbound IPv6 by default, allowing only hub TLS (and SSH if the user needs it);
   - Self-check and list all ports listening on `::` / `0.0.0.0` in `personad hub doctor`;
   - Bind Docker-published ports to `127.0.0.1`.
8. **Reachability self-check**: `personad hub doctor` or App diagnostics tests from outside (2.6: phone on cellular, another node outside home). Give concrete next steps: "ONT IPv6 firewall is blocking traffic", "Hub host firewall is blocking traffic", "TUN proxy swallowed replies", "Carrier blocked this port".
9. **New-device pairing notification**: when a new device obtains a token, notify paired devices: "New device 'xxx' paired. If this wasn't you, revoke it in Settings › Devices."

## 8. Background persistent-connection keepalive

### 8.1 Facts

**NAT / firewall timeouts** (details in 1.7)
- Among 73 cellular carriers in 2011, 4 had ≤5-minute timeouts, 7 had 5–10 minutes, and 48 had >30 minutes;
- WeChat's measurements around 2015: China Mobile / Unicom 2G/3G **5 minutes**, Telecom 3G >28 minutes;
- Of 23 home gateways measured in 2025, 18 were below 124 minutes, minimum 1 minute;
- **Authoritative Chinese 4G/5G data and IPv6 stateful-firewall timeouts: not found.**

**Radio energy use**
- [Fact] LTE: RRC inactivity timer (tail) **11.576 seconds**, tail baseline power **1060 mW**. Paper: "LTE is as much as 23 times less power efficient compared with WiFi", "the long high power tail is found to be a key contributor."
  Scope: measured power model on one US carrier's LTE network, 2012.
  Source: Huang et al., MobiSys 2012, https://web.eecs.umich.edu/~zmao/Papers/RRC4G_mobisys2012.pdf (search excerpts, checked 2026-10-30).
- [Fact] 5G: SA 5G tail timer about 10 seconds, then about 5 seconds in RRC_INACTIVE before IDLE; 5G tail power exceeds 4G (Verizon NSA mmWave tail: 1092 mW).
  Scope: US carrier measurements, 2021.
  Source: Narayanan et al., SIGCOMM 2021, https://feng-qian.github.io/paper/5g_sigcomm21.pdf (search excerpts, checked 2026-10-30).
- [Fact] Heartbeat interval and energy: with a 1350 mAh battery and 3G model, NAT timeout <5 minutes (more frequent heartbeat) costs >17% battery capacity per day; at 30 minutes, <2%.
  Scope: 2011, 3G.
  Source: NetPiculet, SIGCOMM 2011 (§4.3.1, checked 2026-10-30).
- [Assessment, based on these three papers] Every heartbeat puts the radio in a high-power state with roughly 10 seconds of tail, so **heartbeat frequency almost directly determines background power use**.

**IM approaches**
- [Fact] WeChat (2015):
  - Fixed foreground heartbeat; on backgrounding or screen-off while foregrounded, several minimum-interval heartbeats precede adaptive calculation;
  - Separate by network: subtype for data, Wi-Fi name for Wi-Fi;
  - A new connection needs 3 consecutive successful short heartbeats before testing longer intervals. "Success is definite; only repeated failures can establish failure" (5 delayed tests confirm);
  - Choose slightly below the last successful interval for stability. After a failure in the stable phase, test 5 times and recalculate only if all fail; probe upward again every Wednesday (carrier timeouts may have increased);
  - Result: "70% of users reach our maximum heartbeat interval";
  - Competitors then: WhatsApp 4 minutes 45 seconds; Line Wi-Fi 3 minutes 20 seconds / cellular 7 minutes; GCM Wi-Fi 15 minutes / cellular 28 minutes.
  Source: https://cloud.tencent.com/developer/article/1030660 (checked 2026-10-30).
- [Fact] NetEase Yunxin (2026-09 blog): foreground 15–30 seconds (timeout 45–90), background 60–120 seconds (timeout 180–360), weak networks 30–60 seconds; ±30% random jitter ("some carriers block small packets at fixed frequencies"); reconnect backoff 1→2→4→…→60 seconds. No sources given for NAT data.
  Source: https://yunxin.163.com/dev-blog/698 (checked 2026-10-30).

**Android platform behavior**
- [Fact] Doze: "Suspends network access", "Ignores wake locks", delays standard alarms until maintenance windows. `setAndAllowWhileIdle()` / `setExactAndAllowWhileIdle()` "can't fire alarms more than once per nine minutes, per app." The official page **does not** say foreground services bypass Doze networking limits; it says only that they prevent App Standby.
  Source: https://developer.android.com/training/monitoring-device-state/doze-standby (checked 2026-10-30).
- [Fact] AOSP: `NetworkPolicyManager.isProcStateAllowedWhileIdleOrPowerSaveMode` returns true for `procState <= PROCESS_STATE_BOUND_FOREGROUND_SERVICE` (including foreground services), or processes with `PROCESS_CAPABILITY_POWER_RESTRICTED_NETWORK`. Thus **foreground-service processes retain networking in Doze**.
  Source: aosp-mirror `core/java/android/net/NetworkPolicyManager.java` (archived GitHub mirror, last file modification 2024-07; checked 2026-10-30). Chinese ROM power policies may impose extra restrictions; [Not found].
- [Fact] `System.nanoTime()` uses the `uptimeMillis` clock: "This clock stops when the system enters deep sleep." OkHttp scheduling (including WebSocket `pingInterval`) uses `System.nanoTime()`. **OkHttp client pings therefore never fire during device deep sleep**; the foreground service does not hold a wakelock either.
  Sources: class comment in aosp-mirror `core/java/android/os/SystemClock.java`; OkHttp master `internal/concurrent/TaskRunner.kt` (`override fun nanoTime() = System.nanoTime()`) (checked 2026-10-30).
- [Fact] Go: with `net.ListenConfig.KeepAlive` 0, "keep-alive are enabled". `KeepAliveConfig` defaults: Idle 15 seconds, Interval 15 seconds, Count 9. Thus **a default Go server sends TCP keepalive probes on every idle connection after 15 seconds**, also waking the phone's radio. Whether the hub overrides this is being checked by another agent.
  Sources: Go 1.26.4 `src/net/dial.go`, `src/net/tcpsock.go` (checked 2026-10-30).

### 8.2 Deductions [Assessment]

1. **Android background keepalive should be initiated by the hub, not the phone.**
   - During deep sleep, OkHttp timers stop. `AllowWhileIdle` alarms fire at most every 9 minutes, exceeding China's roughly 5-minute NAT timeouts. Phone-scheduled heartbeats cannot preserve connections in Doze.
   - Incoming hub packets (WS ping or TCP keepalive) wake the phone, OkHttp replies pong / kernel replies ACK, refreshing **NAT entries in both directions**.
   - Requires foreground-service networking in Doze (AOSP supports it; test Chinese ROMs).
2. **Current 20-second protocol pings are too frequent for background persistent connections.**
   - With Go's default 15-second TCP keepalive (if not overridden), the phone wakes about every 15–20 seconds.
   - This barely exceeds LTE / 5G's roughly 10–11.6-second tail, so the radio may not return to idle between wakeups.
   - Actual extra power use is **unmeasured**; the literature places this at the very-frequent end.
3. **Negotiate heartbeat per connection**: one hub serves foreground iPhones, persistent background Android connections, and future Watches; fixed 20 seconds is inappropriate.

### 8.3 Proposed heartbeat scheme [Assessment]

**Protocol** (design together with `app_ping` in hub-changes 3.8):
- Client `client_hello` includes `heartbeat: {mode: "foreground"|"background", server_ping_s: N}`. On foreground / background transitions, send `{"type":"heartbeat","mode":…,"server_ping_s":N}`.
- Hub adjusts three settings per connection: WS protocol-ping interval, pong timeout, TCP keepalive (for background, match Idle to ping interval or disable it to avoid two offset timers waking the phone separately).
- Hub replies `{"type":"heartbeat_ack","server_ping_s":actual value}`. Old hubs ignoring `heartbeat` retain 20 seconds (clients still work, with higher power use).
- Old clients (existing iOS) send none of these messages; keep current 20-second / 15-second settings unchanged.

**Foreground** (App visible):
- Current behavior: hub protocol ping every 20 seconds;
- Android application-level `{"type":"ping","id"}` detects weak networking (existing iOS rules: pong within 5 seconds means healthy; ping every 5 seconds when slow);
- OkHttp `pingInterval` only as fallback (for example, 30 seconds).

**Persistent background** (Android foreground service, screen off):
- **Start at 4 minutes** (240 seconds: below WeChat's 5 minutes and NetPiculet Carrier B's 255 seconds; WhatsApp used 4 minutes 45 seconds then). Do not raise it before our own measurements.
- **Adaptive**, following WeChat's rules:
  - Record by network key (cellular: carrier + radio technology; Wi-Fi: gateway / prefix hash, 4.3);
  - Stabilize a new connection with 3 successful starting-interval heartbeats, then probe +1 minute at a time, initial maximum 9–10 minutes;
  - Failure: connection found broken after "last ping time + interval + grace." Lower only after consecutive failures, to one step below the last successful value;
  - Reprobe upward weekly.
- **Allow a minimum below 1 minute**: some home gateways time out at 1 minute (2025 measurements). Repeated Wi-Fi disconnects within 1–2 minutes should prompt "Your router closes idle connections quickly."
- **Random jitter** (Yunxin suggests ±30%) avoids synchronized times and detection of fixed-frequency small packets. But **jitter only downward** (for example, 0.8–1.0× interval): 4 minutes +30% exceeds the known 5-minute timeout.
- **How the phone detects a dead connection without a usable timer**:
  - On every wake (hub ping, network callback, screen-on, maintenance window), check whether time since receiving any hub data exceeds "negotiated interval + grace"; reconnect immediately if so;
  - Reconnect immediately on network change (`onAvailable` / `onLost` / `onLinkPropertiesChanged`);
  - Send an application ping immediately on screen-on / foregrounding.
- **How the hub detects a dead client**: close the connection and mark the device offline if no pong arrives before timeout. Give background connections longer (30–60 seconds) since the device may be waking from deep sleep. Replay notifications after reconnection (hub determines "Completed" events for Android persistent mode; hub-changes 3.9).
- **TCP keepalive or WS ping?** Both refresh NAT. Kernel ACKs to keepalive require no App-process work; WS ping also checks that the App process lives. Prefer WS ping for background, with TCP keepalive at the same interval for redundancy or disabled. **No comparative power data; measure it.**

**iOS** (when iOS follows up): currently disconnects WS on backgrounding and relies on APNs (CLAUDE.md), without persistent background connections. This section affects only foreground heartbeat; it can remain unchanged.

### 8.4 Required measurements (CLAUDE.md: performance figures must be measured)

- **IPv4 CGNAT and IPv6 firewall idle timeouts on all three Chinese carriers' 4G / 5G**: add a hub test endpoint, accept a connection, stay completely silent for T minutes (keepalive disabled), then send one byte from the hub and see whether the phone receives it. Binary-search T (1, 2, 4, 5, 8, 15, 30 minutes); at least 10 trials per carrier × radio technology × v4/v6, recording min / p50 / p90 / max / mean.
- **Persistent-connection power use**: same phone and SIM, background heartbeat of 20 seconds, 2 minutes, 4 minutes, and adaptive. Run each for ≥10 segments of ≥2 hours, screen off and stationary; use `dumpsys batterystats` (cellular radio active time, wake count, estimated App power) and battery decline, recording 5 percentiles.
- **Chinese ROMs** (Xiaomi / Huawei / OPPO / vivo): can a foreground service receive hub pings in Doze, and how long after locking before it freezes?

## 9. Recommended approach (summary) [Assessment]

> Scope (coordinator addition 2026-10-30): change only **Android and the hub** this round; iOS is read-only. **Every hub change must let the existing iOS App work unchanged**—private-network plaintext, old QR format, single address. All iOS approaches below are marked "when iOS follows up."

### 9.1 Meeting compatibility constraints

| Existing iOS dependency | How the new hub preserves it | Basis |
| --- | --- | --- |
| Plaintext `http://<private network>:18660`, `Authorization: Bearer` / WS `?token=` | Keep plaintext listener unchanged; reject plaintext only from **public sources** (private network, ULA, link-local, 100.64/10 VPN, allowlist still work) | 7.2 item 3 |
| QR `persona://pair?url=…&token=…` | Same format. `url` remains a single private-network plaintext address; new information in **additional parameters** (`hub`, `fp`, `tls`, `alt`…). iOS `ServerConfig(pairURL:)` reads named `url` and `token`, ignoring extras. | `ios/Shared/APIClient.swift`, lines 7–16 |
| After scanning: `GET /machines` → WS → push registration → `POST /auth/tokens` device-token exchange | Temporary QR token acts as a full Bearer during its lifetime, with grace after device-token exchange; `--legacy` still prints the main token. | 7.2 item 1; `AppModel.pair()`, `upgradeCredentials()` |
| Fixed 20-second protocol ping | Connections without `heartbeat` negotiation keep 20 seconds / 15 seconds. | 8.3 |
| Unauthenticated `/api/v1/health` | Unchanged; add only nonsensitive fields (`hub_id`, TLS port, `fp`). **No candidate addresses here** (unauthenticated disclosure of the home's address); put them in authenticated endpoints and WS `hello`. | 7.1 |

- The only existing behavior change is rejection of public-source plaintext. Documentation has always required the hub to be private-network / VPN-only, so normal use is unaffected. However, existing iOS has `NSAllowsArbitraryLoads`; anyone already using public plaintext loses connectivity after upgrade. Add `allow_public_plaintext` in `hub.json` (off by default) to restore old behavior, with warnings in `hub doctor` and logs.

### 9.2 Hub (this round)

1. **Address discovery**:
   - netlink flag-aware addresses, rules in 2.3: exclude temporary, deprecated, ULA, link-local, virtual interfaces;
   - Address-change subscription + full rescan every 1–5 minutes;
   - For public IPv4, query WAN with UPnP / NAT-PMP / PCP plus STUN / HTTPS echo to detect CGNAT;
   - Produce candidates: `lan` (private IPv4 / ULA), `ipv6` (global, stable), `ipv4` (public, only after reachability confirmed), `name` (DDNS / `public_url`), each with port, protocol (`https` / `http`), and `seen_at`.
2. **Publish candidates**:
   - Authenticated `GET /api/v1/endpoints`;
   - New WS `hello` field;
   - Push `{"type":"endpoints"}` on change;
   - Add `endpoints`, `tls`, `heartbeat`, `app_ping` to `features`.
3. **TLS listener**:
   - Separate configurable high port, not 80/443/8080/8443;
   - Hub identity key (P-256, 0600, not in database), self-signed certificate renewed with the same key;
   - With domain + DNS token, use ACME DNS-01 (lego) to obtain a wildcard public-CA certificate with the same key;
   - No IP certificates.
4. **Pairing**: `personad hub pair` defaults to temporary tokens (high entropy, short lifetime, one exchange); QR adds `hub`, `fp`, `tls`, `alt` (only one or two most likely external candidates; fetch others after pairing to avoid dense QR codes).
5. **DDNS (optional)**:
   - Cloudflare / Alibaba Cloud / DNSPod / generic callback URL;
   - Advertise only stable addresses selected by 2.3;
   - Publish A only after confirmed external reachability;
   - Share the token with ACME, permission limited to one zone.
6. **Hardening** (7.2):
   - Reject public-source plaintext;
   - Rate-limit authentication failures by /64;
   - HTTP timeouts;
   - New-device pairing notifications;
   - `personad hub doctor` lists listeners, firewalls, TUN proxy risks and asks an external node / phone outside home to test reachability.
7. **Heartbeat negotiation** (8.3):
   - `client_hello.heartbeat` adjusts WS ping interval, pong timeout, TCP keepalive per connection;
   - Application `ping` / `pong` (hub-changes 3.8);
   - Old clients keep 20 seconds.

### 9.3 Android (this round)

1. **Pairing**: parse new parameters, save `hub_id`, `fp` (and `fp_next`), and candidates in encrypted storage. Old hubs without `tls` fall back to private-network plaintext, with a Settings notice: "This hub version is too old for direct public connectivity."
2. **Connection management**:
   - Staggered concurrency (250 ms) per 4.3; a candidate wins only after identity verification (SPKI pinning + `hub_id`);
   - Cache winners by network key;
   - Reprobe on network change, disconnect, or new candidates;
   - Periodic upgrade checks.
3. **TLS**: hub-specific `OkHttpClient`, custom TrustManager verifying SPKI (`CertificateException` on mismatch). REST, WS, images, uploads / downloads all use it; WebView does not load hub resources directly.
4. **Local-network permission** (target 37): private-network or same-/64 global IPv6 connections at home both require `ACCESS_LOCAL_NETWORK` (5.3). On denial, follow 5.3's fallback order; display permission blocking separately from hub unreachability.
5. **Persistent background**:
   - Hub-initiated heartbeat, negotiated starting interval 4 minutes, per-network adaptation and jitter;
   - On every wake, check time since last hub data and reconnect immediately if timed out;
   - Reconnect immediately on network change;
   - Foreground returns to 20 seconds + application ping.
6. **Diagnostics page** shows:
   - Current path (private network / IPv6 / IPv4 / domain) and RTT;
   - Phone IPv6 availability, VPN use, captive portal status;
   - Last failure category: DNS / rejection / timeout / certificate mismatch / local-network permission / old hub.

### 9.4 iOS (when iOS follows up)

- Parse new QR parameters; SPKI verification in `URLSessionDelegate`, `URLSessionWebSocketTask` on the same session;
- `NWPathMonitor` + staggered candidate probes; high-level APIs synthesize IPv4 literals on NAT64 networks (4.2);
- Keep APNs in the background, no persistent connection. The hub can send new candidates via APNs background pushes after address changes (3.3).

### 9.5 Risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| ONT IPv6 firewall is on / off only, or cannot be disabled | Public connectivity impossible, or entire home network exposed | Guide bridging + own router with suffix-based rules; hub host firewall fallback; doctor checks |
| Prefix changes several times daily, no domain | Phone loses the hub while offline | Recommend at least DDNS; signed announcement as alternative; private-address fallback |
| Carrier blocks ports / regional policies differ | Some users' ports unreachable | Configurable ports; clear doctor diagnostics |
| 4G/5G NAT timeout below 4 minutes | Frequent background disconnects and power-hungry reconnection | Adapt downward; measured data |
| Chinese ROM freezes foreground service or networking | Notifications lost | Battery allowlist / autostart guidance (PR #84 already has it); real-device tests; FCM if Google services exist |
| Self-signed pinning key lost | All devices must re-pair | Documentation; announce next key |
| Hub-machine TUN proxy swallows inbound replies | Silent public inbound failure | doctor detection; exclusion rules in deployment docs |

## 10. Unresolved questions / required measurements

| # | Question | Current knowledge | Follow-up |
| --- | --- | --- | --- |
| 1 | IPv4 CGNAT idle TCP timeout on all three Chinese carriers' 4G / 5G | Only around-2015 2G/3G data (5 minutes / >28 minutes) | Binary-search test in 8.4, ≥10 trials per group |
| 2 | Phone-side IPv6 stateful-firewall / 5GC idle timeout | No data found | Same, split v4 / v6 |
| 3 | Persistent foreground-service power use at different heartbeat intervals | Unmeasured | 8.4, ≥10 segments × ≥2 hours |
| 4 | Chinese ROM networking for foreground services in Doze / after lock | AOSP allows it; ROMs unknown | Real-device matrix |
| 5 | Android 17: same-/64 global IPv6 blocked by local-network permission? | 17.0.0_r1 source says yes; docs say "Directly-connected routes" | Real device + `dumpsys connectivity trafficcontroller` |
| 6 | Android 17: private IPv4 outside the same subnet (192.168.10.x) blocked? | Source blocks only on-link prefixes; docs list all 192.168/16; mismatch | Real device |
| 7 | Local-network permission over VPNs with broad address masks | Inferred to depend on VPN-interface prefix length | Real device, one Tailscale and one WireGuard |
| 8 | Carrier blocking of 18660 and other high ports | No reports found | doctor external probes + user feedback |
| 9 | Cross-carrier mobile-data → residential IPv6 reachability | Scattered user reports only | Matrix of three carriers' SIMs × three residential carriers |
| 10 | IPv6 path MTU and suitable MSS | Only user suggestion of 1420 | Large-certificate-chain handshakes; collect handshake timeouts |
| 11 | Chinese resolvers honoring DDNS TTL | Not found | Time resolution from multiple locations after address change |
| 12 | Free DDNS (dynv6 / deSEC / DuckDNS) availability and resolution speed in China | Not checked | Measure |
| 13 | ONT PCP / UPnP IGD2 support for IPv6 allowances | Not found | Test where possible; not a primary path |
| 14 | lego `tencentcloud` provider and current module path | Search inconclusive | Read repository before adoption |
| 15 | `NWConnection` synthesizing NAT64 for IPv4 literals (future iOS) | Apple docs do not say | Measure when iOS follows up |
| 16 | Exact Apple background-push rate-limit wording | JS-rendered page, original not retrieved | Manual verification if needed |
| 17 | Hub override of Go's 15-second TCP keepalive and exact 20-second ping implementation | Another agent checks code | Read hub code |

## 11. Sources (all checked 2026-10-30)

**Official / standards**
- Xinhua, report on *China IPv6 Development Report (2026)*: https://www.news.cn/info/20260729/e3793c4b1c3a428bb365a1f5a2b64513/c.html
- Cyberspace Administration of China, *Implementation Plan (2026–2030)* Q&A: https://www.cac.gov.cn/2026-07/21/c_1786380790538547.htm
- Cyberspace Administration of China, Telecom 5G SA IPv6-only case study: https://www.cac.gov.cn/2021-11/25/c_1639439849620158.htm
- RFC 5382: https://www.rfc-editor.org/rfc/rfc5382.html
- RFC 6092: https://www.rfc-editor.org/rfc/rfc6092.html
- RFC 6887 (PCP): https://www.rfc-editor.org/rfc/rfc6887.html
- RFC 7707 (IPv6 reconnaissance): https://www.rfc-editor.org/rfc/rfc7707.html
- RFC 8305 (Happy Eyeballs v2): https://www.rfc-editor.org/rfc/rfc8305.html
- RFC 8978 (flash renumbering): https://www.rfc-editor.org/rfc/rfc8978.html
- Happy Eyeballs v3 draft: https://datatracker.ietf.org/doc/draft-ietf-happy-happyeyeballs-v3/
- Linux `if_addr.h`, `addrconf.c`, `ip-sysctl.rst`: https://github.com/torvalds/linux
- Linux conntrack sysctl: https://docs.kernel.org/networking/nf_conntrack-sysctl.html
- ip-route(8): https://man7.org/linux/man-pages/man8/ip-route.8.html
- tcp(7): https://man7.org/linux/man-pages/man7/tcp.7.html
- systemd.network(5): https://www.freedesktop.org/software/systemd/man/latest/systemd.network.html
- OpenWrt IPv6 firewall examples: https://openwrt.org/docs/guide-user/firewall/fw3_configurations/fw3_ipv6_examples
- Cloudflare DNS API: https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/edit/
- Alibaba Cloud DNS TTL: https://help.aliyun.com/zh/dns/pubz-add-parsing-record
- DNSPod TTL: https://docs.dnspod.cn/dns/help-ttl/ ; Tencent Cloud API: https://cloud.tencent.com/document/api/1427/56158
- dynv6 API: https://dynv6.com/docs/apis ; deSEC: https://desec.readthedocs.io/en/latest/dyndns/update-api.html
- Let's Encrypt:
  - IP / 6-day certificate GA: https://letsencrypt.org/2026/01/15/6day-and-ip-general-availability
  - First IP certificate: https://letsencrypt.org/2025/07/01/issuing-our-first-ip-address-certificate
  - profiles: https://letsencrypt.org/docs/profiles/
  - Challenges: https://letsencrypt.org/docs/challenge-types/
  - 64-day certificates: https://letsencrypt.org/2026/10/07/64-day-certs
- lego: https://pkg.go.dev/github.com/go-acme/lego/v4/certificate ; https://go-acme.github.io/lego/dns/
- Android:
  - Local Network Definition: https://developer.android.com/privacy-and-security/local-network-definition
  - Local network permission: https://developer.android.com/privacy-and-security/local-network-permission
  - Doze: https://developer.android.com/training/monitoring-device-state/doze-standby
  - LinkProperties: https://developer.android.com/reference/android/net/LinkProperties
- AOSP Connectivity (android-17.0.0_r1):
  - https://android.googlesource.com/platform/packages/modules/Connectivity/+/refs/tags/android-17.0.0_r1/service/src/com/android/server/ConnectivityService.java
  - https://android.googlesource.com/platform/packages/modules/Connectivity/+/refs/tags/android-17.0.0_r1/bpf/progs/netd.c
  - https://android.googlesource.com/platform/packages/modules/Connectivity/+/refs/tags/android-17.0.0_r1/staticlibs/framework/com/android/net/module/util/NetworkStackConstants.java
- AOSP frameworks/base (archived GitHub mirror), `NetworkPolicyManager.java`, `SystemClock.java`: https://github.com/aosp-mirror/platform_frameworks_base
- OkHttp (master), `OkHttpClient.kt`, `CertificatePinner.kt`, `OkHostnameVerifier.kt`, `RealWebSocket.kt`, `TaskRunner.kt`: https://github.com/square/okhttp ; CHANGELOG: https://github.com/square/okhttp/blob/master/CHANGELOG.md
- vishvananda/netlink v1.3.1: https://pkg.go.dev/github.com/vishvananda/netlink
- Go 1.26.4 standard-library source (local GOROOT): `net/netip`, `net/dial.go`, `net/tcpsock.go`, `net/http/server.go`
- golang.org/x/sys v0.47.0 (local module cache): `unix/zerrors_linux.go`, `unix/ztypes_linux.go`
- Apple:
  - NWPath: https://developer.apple.com/tutorials/data/documentation/network/nwpath.json
  - Manual server trust: https://developer.apple.com/documentation/foundation/performing-manual-server-trust-authentication
  - DNS64/NAT64: https://developer.apple.com/library/archive/documentation/NetworkingInternetWeb/Conceptual/NetworkingOverview/UnderstandingandPreparingfortheIPv6Transition/UnderstandingandPreparingfortheIPv6Transition.html
- Docker firewall: https://docs.docker.com/engine/network/packet-filtering-firewalls/

**Papers / measurements**
- NetPiculet (SIGCOMM 2011): https://conferences.sigcomm.org/sigcomm/2011/papers/sigcomm/p374.pdf
- Silent TCP Connection Closure (CoNEXT 2013): https://conferences.sigcomm.org/co-next/2013/program/p211.pdf
- Home Gateway Characteristics (IMC 2010): https://conferences.sigcomm.org/imc/2010/papers/p260.pdf
- On The Evolution of Home Gateways (ITC 2025): https://thomasez.folk.ntnu.no/itc36/itc2025-final4.pdf
- LTE power use (MobiSys 2012): https://web.eecs.umich.edu/~zmao/Papers/RRC4G_mobisys2012.pdf
- 5G power use (SIGCOMM 2021): https://feng-qian.github.io/paper/5g_sigcomm21.pdf
- CT and scanning (IMC 2018): https://dl.acm.org/doi/10.1145/3278532.3278562 ; EuroS&P 2023: https://sites.cs.ucsb.edu/~chris/research/doc/eurosnp23_certvuln.pdf
- WeChat intelligent heartbeat (2015): https://cloud.tencent.com/developer/article/1030660
- NetEase Yunxin persistent-connection practices (2026-09): https://yunxin.163.com/dev-blog/698

**User reports (unofficial, large regional / temporal differences)**
- V2EX:
  - PD prefix lengths: https://v2ex.com/t/930849
  - Shanghai Telecom PD rotation: https://www.v2ex.com/t/1250020
  - Port blocking: https://www.v2ex.com/t/1026607 , https://www.v2ex.com/t/1121687 , https://v2ex.com/t/904810 , https://www.v2ex.com/t/1130831
  - Phone → residential IPv6: https://hk.v2ex.com/t/1014000
  - Phone IPv6 inbound: https://www.v2ex.com/t/843359
  - ONT PD: https://www.v2ex.com/t/1017699
  - Prefix changes: https://www.v2ex.com/t/775397 , https://v2ex.com/t/706397
  - Public IPv4: https://www.v2ex.com/t/808536
- Other forums and blogs:
  - ONT firewalls: https://www.chinadsl.net/thread-169397-1-1.html , https://blog.csdn.net/u013084266/article/details/128358328 , https://www.cnblogs.com/ie123610/p/19026984
  - NodeSeek: https://www.nodeseek.com/post-118403-1 , https://www.nodeseek.com/post-76979-1
  - Communications community forum: https://www.txrjy.com/thread-1240845-1-1.html
  - linux.do: https://linux.do/t/topic/1016716
  - ONT PD blog: https://zhangshuqiao.org/2026-07/XG-140G-TF%E5%85%89%E7%8C%AB%E5%90%AF%E7%94%A8IPv6%20PD%E5%89%8D%E7%BC%80%E5%88%86%E5%8F%91/
  - Public IPv4 requests: https://juejin.cn/post/7647395004020932654 , https://mao.fan/article/432
- sing-box issues: #2167, #4605, #4676 (https://github.com/SagerNet/sing-box/issues/ ); #3079 (https://github.com/LIghtJUNction/sing-box/issues/3079)
- ddns-go: https://github.com/jeessy2/ddns-go , https://github.com/jeessy2/ddns-go/discussions/983
