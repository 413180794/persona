# 公网直连、IPv6、TLS、长连接保活：调研笔记

> 调研日期：2026-10-30（下文「查于 2026-10-30」即本日）。只做调研，没有改仓库。
> 标注约定：**【事实】** 有来源（URL + 查询日期）；**【判断】** 我的推理 / 建议；**【未查到】** 找过但没有可靠来源。
> 隐私：示例地址一律用文档段 `2001:db8::/32`、`192.0.2.0/24`、`198.51.100.0/24`、`203.0.113.0/24` 和 `example.com`，不写任何真实主机、地址、账号。
> 背景：hub 现在明文 HTTP，监听 `:18660`，Bearer token（WS 也可 `?token=`），配对二维码 `jarvis://pair?url=…&token=…`；hub 每 20 秒发一次协议层 ping、15 秒超时（来自任务说明，代码细节由另一个 agent 核查）。

## 目录

0. 结论速览（最后补）
1. 国内 IPv6 / 家宽现状
2. hub 怎么知道自己的公网地址
3. 地址会变怎么办：DDNS 与推送
4. 手机端多地址选路
5. Android 17 本地网络权限和公网 IPv6
6. TLS
7. 个人服务器暴露在公网的加固
8. 后台长连接保活
9. 推荐方案（汇总）
10. 没查清 / 必须实测的
11. 来源清单

---

## 0. 结论速览

> 本轮范围：只改 Android 和 hub；iOS 只读，相关做法标「以后 iOS 跟进时」；hub 改动必须让现有 iOS App（内网明文、旧格式二维码、单地址）不改代码照常工作（第 9.1 节）。

- **能不能公网直连**：
  - 技术上能。国内手机流量 IPv6 普及（移动网 IPv6 流量占 70.85%，2025 年底）。手机主动连出去不受运营商对手机 IPv6 入站的拦截影响。
  - 门槛在 hub 所在的家里，三道：
    1. 光猫 / 路由器的 IPv6 入站防火墙默认开着，而且常常只有开 / 关两档；
    2. 前缀会变，有的地区一天两次以上；
    3. 部分地区封 80/443。
  - 这三道 App 和 hub 都替用户解决不了，只能检测 + 引导（1.3–1.5）。
- **地址发现**（第 2 节）：
  - hub 用 netlink 读带 `IFA_FLAGS` 的地址，排除临时 / 废弃 / ULA / 链路本地 / 虚拟网卡。`/proc/net/if_inet6` 看不到 0x100 以上的标志。
  - 订阅地址变化，外加定时重扫。
  - 可达性只能从外部测：本机连自己的地址走环回，不经过光猫。
- **地址变化**（第 3 节）：
  - 闪断重编号时旧连接立刻断，「hub 推送新地址」只在有旁路（iOS 的 APNs）或平滑换前缀时有效。
  - **没有域名只能算降级模式**；推荐 DDNS（自己的域名，或免费子域名）。
  - 国内两家免费版 DNS 的 TTL 最低 600 秒，所以 DNS 只作找回手段，字面地址优先。
- **选路**（第 4 节）：
  - hub 下发多候选，手机按 RFC 8305 的思路错峰 250 ms 并发探测；
  - **TLS 公钥钉扎 + `hub_id` 校验通过才算赢**，因为旧前缀可能已分给别人；
  - 按网络缓存赢家，换网重探。
  - OkHttp 5 的 fast fallback 只在同一主机名的多个 IP 之间赛跑，不同 URL 要自己赛跑。
- **Android 17 本地网络权限**（第 5 节）：
  - 按官方定义（IPv6「Directly-connected routes」）和 android-17.0.0_r1 源码（IPv6 的 on-link 前缀一律算本地），**家里 Wi-Fi 上连同一个 /64 的 hub 全局 IPv6 照样要 `ACCESS_LOCAL_NETWORK`**，改连公网 IPv6 绕不开这个权限；
  - 经路由器回环连公网 IPv4 不算本地。需真机验证。
- **TLS**（第 6 节）：
  - 默认用 hub 自签证书 + 二维码里放 SPKI 指纹，Android 用自定义 TrustManager 钉公钥（`CertificatePinner` 对自签证书无效）；
  - 有域名时可加 ACME DNS-01（同一把密钥）；
  - Let's Encrypt 的 IP 证书（2026-01 GA，6 天、只能 HTTP-01 / TLS-ALPN-01，要开 80/443）不适合。
- **加固**（第 7 节）：
  - 二维码改发短时临时 token（格式不变，兼容旧 iOS 的配对流程）；
  - 鉴权失败按 IPv6 /64 聚合限流；
  - 公网来源拒绝明文；
  - 端口避开 80/443/8080/8443；
  - 固定接口标识要随机（别用 `::1`）；
  - 主机防火墙兜底（光猫一关就全暴露，Docker 端口绕过 ufw）；
  - 有域名用通配证书避开 CT 日志扫描（CT 曝光后 73 秒到约 3 分钟就有人来查询这个名字，随后有端口扫描）。
- **保活**（第 8 节）：
  - Android 深睡时 OkHttp 的客户端 ping 不触发（`System.nanoTime` 在深睡时停）；`AllowWhileIdle` 闹钟最快 9 分钟一次。所以**后台心跳要由 hub 发起、按连接协商**：起点 4 分钟（低于已知的 5 分钟 NAT 超时）、按网络自适应、只往下加抖动。
  - 前台保持 20 秒 + 应用层 ping。
  - 现在固定 20 秒的 ping，再加上 Go 默认 15 秒的 TCP keepalive（若 hub 未覆盖），对后台常驻连接太密；耗电**未实测**。
- **最大的未知**（第 10 节）：
  - 国内 4G/5G 的 CGNAT 和 IPv6 防火墙空闲超时（只有 2015 年 2G/3G 的数据）；
  - 前台服务常驻连接的耗电；
  - 国产 ROM 的行为；
  - Android 17 本地网络权限的边界情况。

---

## 1. 国内 IPv6 / 家宽现状

### 1.1 总体数字（官方）

- 【事实】截至 2025-12：IPv6 活跃用户 8.69 亿，占网民 77.38%；全网流量中 IPv6 占 33.79%，其中**移动网络 70.85%**、固定网络 32.38%；2026-06 全网 IPv6 流量占比升到 34.55%。
  来源：新华网《我国IPv6活跃用户数达8.69亿》https://www.news.cn/info/20260729/e3793c4b1c3a428bb365a1f5a2b64513/c.html （查于 2026-10-30，报道《中国IPv6发展报告（2026）》，2026-07-28 发布）。
- 【事实】「十四五」期间「全国家庭网关 IPv6 开启率达 96%」；「十五五」8 项指标里有「固定宽带用户 IPv6 连通率」，但答记者问里没有定义和目标值。
  来源：中央网信办《深化 IPv6 技术创新和融合应用实施方案（2026—2030 年）》答记者问 https://www.cac.gov.cn/2026-07/21/c_1786380790538547.htm （查于 2026-10-30）。
- 【事实】移动网：手机目前普遍是**双栈**（IPv4 + IPv6）。中国电信 5G 用户面用 464XLAT + DNS64 支撑 IPv6 单栈，但 2021 年的案例写「目前在用户面主要为终端分配 IPv6 地址和 IPv4 地址」；2023 年电信做的是 5G 物联网 IPv6 单栈（NAT64/DNS64）试点；中国移动 2025-08 的 G-SRv6 单栈验证是「内容、终端保持 IPv4/IPv6 双协议栈」、只在网络内部单栈。
  来源：https://www.cac.gov.cn/2021-11/25/c_1639439849620158.htm ；http://union.china.com.cn/zhuanti/txt/2025-08/06/content_43195686.html （查于 2026-10-30，后者为转述）。
- 【未查到】三大运营商把普通手机用户大规模切到 IPv6 单栈（464XLAT）的公开说明；联通移动网的单栈情况。

### 1.2 家宽：给不给全局 IPv6、前缀多长

- 【事实，网友报告】光猫路由模式 / 桥接后路由器拨号，都能拿到全局 IPv6；前缀长度因省份、运营商而异，常见 **/56、/60**，联通有不少地方只给 **/64**（2023 年 V2EX 调查帖：广州电信 /60、移动 /60、联通 /64；上海电信 /56、联通 /56、移动 /60；江苏电信 /56→/60；山西联通 /64 等）。
  来源：https://v2ex.com/t/930849 （查于 2026-10-30，2023 年帖子，非官方）。
- 【事实，网友报告】光猫拨号（路由模式）时，很多光猫**只给下级一个 /64 或不下发 PD**（要开 DHCPv6 服务器或改桥接才有 /60 给路由器）；2026 年一篇博客：电信 XG-140G-TF 拿到 /60，但给下级的 DHCPv6-PD 默认没开，要 telnet 改。
  来源：https://www.v2ex.com/t/1017699 ；https://zhangshuqiao.org/2026-07/XG-140G-TF%E5%85%89%E7%8C%AB%E5%90%AF%E7%94%A8IPv6%20PD%E5%89%8D%E7%BC%80%E5%88%86%E5%8F%91/ （查于 2026-10-30）。
- 【未查到】运营商官方的前缀长度政策。

**对 Jarvis 的影响【判断】**：hub 只需要**自己一台机器的一个全局地址**，不需要子网，所以前缀长度和 PD 是否下发到二级路由都不影响「有没有地址」；影响的是**防火墙在哪一层、能不能放行**（见 1.4）。

### 1.3 前缀多久变一次

- 【事实，网友报告】没有统一周期。PPPoE 重拨后前缀通常跟着变；不重拨也可能变：
  - 2026-10（约 10-19 发帖）上海电信用户：路由器运行 24.7 天，内核残留 57 条历史前缀，「平均每天 2 次以上更换下发的前缀」；同帖上游 RA 给的寿命「preferred ≈ 132000s（1.53 天）/ valid ≈ 218000s（2.53 天）」，但「他换 PD 比他这个定的这个时间快多了」；同帖另有用户说「一直没换过」、安徽电信打客服把重拨周期从 2 天改成一个月；同城另一用户「压根没下发 pd」。
    来源：https://www.v2ex.com/t/1250020 （查于 2026-10-30）。
  - 其他报告：哈尔滨电信称「固定」；有人不到 24 小时换一次；IPv4 的强制重拨周期 2020 年时广东电信约 48 小时、上海/苏州不重拨。
    来源：https://www.v2ex.com/t/775397 、https://v2ex.com/t/706397 、https://www.nodeseek.com/post-118403-1 （查于 2026-10-30，均为网友观察）。
- 【判断】设计时必须假设：**全局 IPv6 前缀可能一天变几次，也可能几个月不变**；IPv4（若有公网）通常 1–30 天变一次或不变。地址变化后 hub 要在秒级感知并通知 / 更新 DNS（第 2、3 节）。

### 1.4 入站防火墙：光猫 / 路由器默认拦截

- 【事实】RFC 6092（IPv6 CPE 简单安全）建议默认拦截未经请求的入站 TCP（REC-34：默认对入站 SYN 回 ICMPv6「管理策略禁止」），但也要求提供一键「透明模式」（REC-49，且「MAY be the default」）。
  来源：https://www.rfc-editor.org/rfc/rfc6092.html （查于 2026-10-30）。
- 【事实，网友报告】国内光猫路由模式下 **IPv6 防火墙默认开着**（「IPv6 Session」/「Ipv6Spi」开关），外部能 ping 通但 TCP 端口全是 filtered；关法因型号而异：有的普通管理账号能关，有的要超级密码 / telnet / 改配置文件，有的型号（某些联通 SDN 光猫）根本没有这个选项、只能改桥接或换猫；**多数光猫只有开 / 关两档，不能只放行一个端口**（华为部分型号有「IPv6 虚拟主机配置」可按端口放行）。
  来源：https://www.chinadsl.net/thread-169397-1-1.html ；https://blog.csdn.net/u013084266/article/details/128358328 ；https://www.cnblogs.com/ie123610/p/19026984 ；https://www.v2ex.com/t/1026607 #20 （查于 2026-10-30）。
- 【事实，网友报告】常见做法：光猫改桥接、由自己的路由器（OpenWrt / 华硕等）拨号，在路由器上**按目的地址 + 端口放行**（OpenWrt 规则 wan→lan、family ipv6、ACCEPT；更安全的做法是按地址后 64 位匹配，只放行 hub 那台机器）。
  来源：https://hk.v2ex.com/t/1014000 （2024-02，#18、#20）（查于 2026-10-30）。
- 【判断】这是公网直连最大的「用户侧门槛」：App 和 hub 无法替用户打开光猫防火墙。产品上必须有**外部可达性自检**（见第 7 节和第 9 节），并把「光猫 / 路由器放行 IPv6 入站某端口」写成配置引导。hub 机器本机的防火墙（ufw / nftables）也要放行。

### 1.5 入站端口封锁

- 【事实，网友报告】家宽 IPv6 封不封 80/443 **没有全国统一规则，按省、区甚至小区不同**：北京移动海淀不封、昌平封；安徽、江苏移动封，广东移动不封；江苏镇江移动 2025-03-27 13:00 起封 IPv6 80/443（换 10086 端口正常）；上海移动（无公网 v4）2024 与 2026-09 两次报告 80/443 开放；2026-01 一份报告：电信北京通州 v6 80/443 开放、杭州滨江 / 深圳龙岗（有 v4）被封；有报告称申请公网 v4 后 v6 也跟着封常用端口。
  来源：https://www.v2ex.com/t/1026607 ；https://www.v2ex.com/t/1121687 ；https://v2ex.com/t/904810 ；https://www.v2ex.com/t/1130831 （查于 2026-10-30）。
- 【事实，网友报告】IPv4 公网家宽普遍封 80/443（有时 8080、445、25）；2025-10 有新装电信用户报 80 被封。
  来源：https://linux.do/t/topic/1016716 （查于 2026-10-30）。
- 【未查到】关于 18660 这类高位端口被封的报告；运营商官方的封端口政策。
- 【判断】**不要用 80/443/8080/8443**。沿用 18660 或用户可配的高位端口（5 位数）更稳；但也要假设「某天突然被封」，所以要有自检和多候选地址。注意：不用 443 也意味着拿不到「TLS on 443」那种对中间设备最友好的伪装（一般没关系）。

### 1.6 手机流量能否连到别人家宽的 IPv6

- 【事实，网友报告】手机流量的 **IPv6 入站**被移动、联通核心网拦截（电信多数地区能入站）——但这只影响「别人连手机」，**不影响手机主动连出去**。
  来源：https://www.v2ex.com/t/843359 ；https://www.nodeseek.com/post-76979-1 （查于 2026-10-30）。
- 【事实，网友报告】光猫桥接 + 关闭路由器防火墙后，从手机访问家里 PC 的 IPv6 8080 端口可通（「可能地区有差异」）。
  来源：https://hk.v2ex.com/t/1014000 #11（查于 2026-10-30）。
- 【事实，网友报告】有同运营商同省之间 IPv6 互访异常的个例（如浙江电信 2022 年 5GC→5GC 不通）。
  来源：https://www.txrjy.com/thread-1240845-1-1.html （查于 2026-10-30）。
- 【未查到】系统性的「手机流量 → 家宽 IPv6」可达率数据。【判断】主流情况应当可通，但必须由 App 的探测结果说了算，不能假设。

### 1.7 运营商 NAT / 状态防火墙对空闲 TCP 的超时

- 【事实，标准】RFC 5382 REQ-5：NAT 的「established connection idle-timeout」**不得短于 2 小时 4 分钟**，「transitory」不得短于 4 分钟；RFC 6092 REC-35 对 IPv6 CPE 防火墙同样要求 2 小时 4 分钟。
  来源：https://www.rfc-editor.org/rfc/rfc5382.html ；https://www.rfc-editor.org/rfc/rfc6092.html （查于 2026-10-30）。**现实中运营商经常不守**，见下。
- 【事实，实测，2011，美国为主的 73 家蜂窝运营商】NetPiculet（SIGCOMM 2011）测得空闲 TCP 超时分布：(0,5] 分钟 4 家、(5,10] 7 家、(10,20] 6 家、(20,30] 8 家、>30 分钟 48 家；某美国大运营商（Carrier B）255 秒就发伪造 RST 断开空闲连接（但对 5228 端口（Google 推送）是 1600 秒）。能耗：按 1350 mAh 电池和 3G 功耗模型，超时 <5 分钟时每天保活耗电 >17% 电池容量，30 分钟时 <2%。论文建议运营商设 30 分钟。
  来源：https://conferences.sigcomm.org/sigcomm/2011/papers/sigcomm/p374.pdf （Table 5、§4.3.1，查于 2026-10-30，PDF 本地提取文本核对）。
- 【事实，实测，约 2013–2015，2G/3G】微信团队测得：中国移动 3G/2G **5 分钟**、中国联通 2G **5 分钟**、中国电信 3G **>28 分钟**；GCM 心跳 Wi-Fi 15 分钟 / 数据网 28 分钟；WhatsApp 4 分 45 秒；Line Wi-Fi 3 分 20 秒、蜂窝 7 分钟。
  来源：《Android 微信智能心跳方案》（微信终端开发团队，原发 2015-08-17）https://cloud.tencent.com/developer/article/1030660 （查于 2026-10-30）。
- 【事实，实测】家用网关（2010，34 台）TCP 空闲超时中位数约 60 分钟（IMC 2010，Hätönen et al.；中位数经 RFC 9006 / 引用论文转述，未读原文）。
  来源：https://conferences.sigcomm.org/imc/2010/papers/p260.pdf （查于 2026-10-30，转述）。
- 【事实，实测，2025】ITC 36（2025）复测 23 台家用网关（德国市场，含运营商配发的）：**只有 4 台满足 RFC 5382 的 124 分钟**，1 台恰好 7200 秒，「其余 18 台都远低于推荐值，最低的只有 1 分钟」；图 3 中的值依次约为 1、3、10、10、15（×6）、20、30（×4）、40、60、60、120、124 分钟及 >1 天；超时和设备新旧无关。
  来源：Neubert, Winter, Winkler, *On The Evolution of Home Gateways*, ITC 36 (2025) https://thomasez.folk.ntnu.no/itc36/itc2025-final4.pdf （§A.1 与 Fig. 3，查于 2026-10-30，PDF 本地提取文本核对）。
- 【事实】Linux conntrack 默认 `nf_conntrack_tcp_timeout_established` = 432000 秒（5 天），UDP 30 / 120 秒。来源：https://docs.kernel.org/networking/nf_conntrack-sysctl.html （查于 2026-10-30）。国产路由器多基于 Linux，但厂商可能改值，【未查到】国产路由器 / 光猫的实测值。
- 【未查到】**4G/5G 时代国内三家运营商 IPv4 CGNAT 的 TCP 空闲超时权威实测**；**IPv6 方向（没有 NAT，只有状态防火墙 / 5GC UPF）的空闲超时**的任何实测或运营商文档。网上流传的「电信约 2 分钟」「移动约 5 分钟」的新博客都没有测试方法，不采用。
- 【判断】在拿到自己的实测之前，后台心跳**按 IPv4 CGNAT 最坏 ≈ 4–5 分钟设计**（即保活间隔 ≤ 约 4 分钟），IPv6 不要假设更长；同时做自适应探测（第 8 节）。**两端都可能是瓶颈**：手机那一侧是运营商 CGNAT / 防火墙；hub 那一侧是光猫 / 路由器的端口映射（IPv4）或 IPv6 状态防火墙放行规则对应的 conntrack 表项——2025 年的实测说明家用网关也可能只有几分钟。所以保活间隔取「两端里最短的那个」，靠探测得出，不靠假设。

## 2. hub 怎么知道自己的公网地址

### 2.1 Linux 的地址标志（事实）

- 【事实】`include/uapi/linux/if_addr.h`（Linux 主线，查于 2026-10-30，经 GitHub API 读取）：
  `IFA_F_TEMPORARY`（= `IFA_F_SECONDARY`）0x01、`IFA_F_NODAD` 0x02、`IFA_F_OPTIMISTIC` 0x04、`IFA_F_DADFAILED` 0x08、`IFA_F_HOMEADDRESS` 0x10、`IFA_F_DEPRECATED` 0x20、`IFA_F_TENTATIVE` 0x40、`IFA_F_PERMANENT` 0x80、`IFA_F_MANAGETEMPADDR` 0x100、`IFA_F_NOPREFIXROUTE` 0x200、`IFA_F_MCAUTOJOIN` 0x400、`IFA_F_STABLE_PRIVACY` 0x800（主线 2026-10 又加了 `IFA_F_GLOBAL` 0x1000，但它是给 AF_PACKET 组播表用的，和单播地址无关）。
  头文件注释原文：「IFA_FLAGS is a u32 attribute that extends the u8 field ifa_flags. If present, the value from struct ifaddrmsg will be ignored.」
  来源：https://github.com/torvalds/linux/blob/master/include/uapi/linux/if_addr.h
- 【事实】**坑**：`/proc/net/if_inet6` 打印的是 `(u8) ifp->flags`（`net/ipv6/addrconf.c` 的 `if6_seq_show`），所以 **`MANAGETEMPADDR`（0x100）和 `STABLE_PRIVACY`（0x800）在 procfs 里看不到**；要拿全标志必须走 netlink 读 `IFA_FLAGS` 属性。
  来源：https://github.com/torvalds/linux/blob/master/net/ipv6/addrconf.c （查于 2026-10-30）。
- 【事实】含义（结合 ip-sysctl 文档）：
  - `TEMPORARY`：RFC 4941 隐私扩展生成的临时地址，默认首选寿命 1 天（`temp_prefered_lft` 86400）、有效 2 天（`temp_valid_lft` 172800）——**不能给手机用**。
  - `MANAGETEMPADDR`：「从它派生临时地址」的那个公开 SLAAC 地址，本身是稳定的。
  - `STABLE_PRIVACY`：RFC 7217 生成的地址（同一前缀下稳定，换前缀后接口标识也会变）。
  - `DEPRECATED`：首选寿命已到 0，前缀正在退场，**不再作为新连接的地址发布**。
  - `TENTATIVE` / `DADFAILED`：还没完成或没通过重复地址检测，不可用。
  - `PERMANENT`：静态配置的地址（没有寿命）。
  - 内核默认 `use_tempaddr` = 0（不开隐私扩展），`addr_gen_mode` = 0（EUI-64）；发行版 / NetworkManager 常改成开启临时地址、stable-privacy。
  来源：https://github.com/torvalds/linux/blob/master/Documentation/networking/ip-sysctl.rst （查于 2026-10-30）。
- 【事实】systemd-networkd：`IPv6PrivacyExtensions=` 未设时取 networkd.conf 的值，「defaults to "no"」；`[IPv6AcceptRA] Token=` 支持 `eui64`、`static:ADDRESS`（用固定接口标识拼 RA 前缀）、`prefixstable`（RFC 7217）。
  来源：https://www.freedesktop.org/software/systemd/man/latest/systemd.network.html 与 https://github.com/systemd/systemd/blob/main/man/systemd.network.xml （查于 2026-10-30）。

### 2.2 Go 里怎么拿

- 【事实】标准库 `net.Interfaces()` / `Addrs()` **不给标志和寿命**。`net/netip` 的 `IsGlobalUnicast()` 文档原文：「It also returns true even if ip is in the IPv4 private address space or IPv6 unique local address space」——所以还要用 `IsPrivate()`（覆盖 `fc00::/7`）排除 ULA。来源：Go 1.26.4 `src/net/netip/netip.go`（本机 `go env GOROOT`，查于 2026-10-30）。
- 【事实】`golang.org/x/sys/unix`（v0.47.0）已有全部 `IFA_F_*` 常量（不含 0x1000）、`IFA_FLAGS`（8）、`IFA_CACHEINFO`（6）、`IfaCacheinfo{Prefered, Valid, Cstamp, Tstamp}`、`RTNLGRP_IPV6_IFADDR`（9）。可以自己用 `unix.NetlinkRIB(RTM_GETADDR, AF_INET6)` + `syscall.ParseNetlinkMessage` 解析，零第三方依赖。来源：本机模块缓存 `golang.org/x/sys@v0.47.0/unix/zerrors_linux.go`、`ztypes_linux.go`（查于 2026-10-30）。
- 【事实】`github.com/vishvananda/netlink` v1.3.1（2025-05-09 发布）：
  - `type Addr struct { *net.IPNet; Label string; Flags int; Scope int; Peer *net.IPNet; Broadcast net.IP; PreferedLft int; ValidLft int; LinkIndex int }`；
  - 解析时**读 `IFA_FLAGS` 覆盖 u8 字段**（`addr_linux.go`：`case unix.IFA_FLAGS: addr.Flags = int(native.Uint32(...))`），寿命来自 `IFA_CACHEINFO`；
  - `AddrList(link Link, family int) ([]Addr, error)`（可能返回 `ErrDumpInterrupted`，结果可能不完整，要重试）；
  - 监听：`AddrSubscribeWithOptions(ch chan<- AddrUpdate, done <-chan struct{}, AddrSubscribeOptions{ListExisting, ErrorCallback, ReceiveBufferSize, …})`，订阅 `RTNLGRP_IPV4_IFADDR` + `RTNLGRP_IPV6_IFADDR`；`AddrUpdate{LinkAddress, LinkIndex, Flags, Scope, PreferedLft, ValidLft, NewAddr}`。
  来源：proxy.golang.org 下载的 v1.3.1 源码（`addr.go`、`addr_linux.go`，查于 2026-10-30）；https://pkg.go.dev/github.com/vishvananda/netlink 。
- 【判断】选 vishvananda/netlink（成熟、标志齐全、带订阅），或者只用 x/sys 自己写一个 150 行左右的 RTM_GETADDR 解析。hub 现在已经在做机器监控（`docs/machines.md`），可复用其中「`/sys/class/net/<名字>` 指向 `/sys/devices/virtual/` 即虚拟网卡」的判断。

### 2.3 选地址的规则【判断，依据上面的事实】

候选 IPv6 地址必须同时满足：
1. 网卡：`UP`、非 loopback、**非虚拟网卡**（`/sys/devices/virtual/` 下的 docker0、br-*、veth*、virbr*、tailscale0、wg*、tun* 等一律排除；也可用 netlink `LinkList` 的 `Type()` 排除 `veth` / `bridge` / `tun` / `wireguard` / `vxlan` / `dummy`）。允许用户在 `hub.json` 里指定网卡白名单。
2. 地址：scope = universe（0）；在 `2000::/3` 内；排除 `fc00::/7`（ULA）、`fe80::/10`、`2001:db8::/32` 与 `3fff::/20`（文档）、`2002::/16`（6to4）、`2001::/32`（Teredo）。
3. 标志：排除 `TEMPORARY`、`DEPRECATED`、`TENTATIVE`、`DADFAILED`；`PreferedLft > 0`（0 = 已废弃）。
4. 多个候选时的优先级：`PERMANENT`（静态）> 带固定 token 的 SLAAC > `MANAGETEMPADDR` / EUI-64 > `STABLE_PRIVACY`；同级取有效寿命最长的；**粘滞**：当前发布的地址只要还满足条件就不换，避免来回跳。
- **建议用户给 hub 配固定接口标识**（`Token=static:::<固定后缀>` 或 NetworkManager 的 `ipv6.token`）：前缀变了后缀不变，路由器上可以按后缀放行（OpenWrt 支持 `dest_ip ::23/-64` 这种「只匹配后 64 位」的规则，前缀变了规则照样生效）。
  来源（OpenWrt）：https://openwrt.org/docs/guide-user/firewall/fw3_configurations/fw3_ipv6_examples 「Dynamic prefix forwarding」（查于 2026-10-30）。注意 OpenWrt 文档原文「The negative netmask notation works only with OpenWrt firewall」。
- **不能用「默认路由的出口地址」**（`ip route get` / netlink `RouteGet`）来选：开了隐私扩展时出口源地址就是临时地址；hub 机器如果走 TUN 代理（本仓库 CLAUDE.md 记录过 hub 机器用 TUN 代理），默认路由的出口是 TUN 网卡。

### 2.4 TUN 代理 / 策略路由的坑

- 【事实，issue 报告】sing-box 的 `auto_route` 打开时，有用户报告「入站请求到了，回包被拉进 TUN」，外部客户端一直重试到超时（sing-box #2167，OpenWrt；1.11.1 起 IPv6 也受影响）；服务器上 sing-box TUN 接管全部流量时 IPv6 入站失败（#3079）；关闭 sing-box 时误删 IPv6 策略规则 `0: from all lookup local`（#4605）。
  来源：https://github.com/SagerNet/sing-box/issues/2167 、https://github.com/LIghtJUNction/sing-box/issues/3079 、https://github.com/SagerNet/sing-box/issues/4605 （查于 2026-10-30）。
- 【判断】hub 机器如果开着 TUN 代理，**公网入站要单独验证**；必要时让代理把 hub 端口 / hub 的源地址排除在外（`route_exclude_address`、按 uid / 端口绕过等，各代理写法不同）。这一条要写进部署文档的自检项。

### 2.5 公网 IPv4：有没有被 NAT

【判断，标准做法】分三层判断：
1. **本机网卡上有没有公网 IPv4**（不是 10/8、172.16/12、192.168/16、100.64/10、169.254/16）：有 = hub 自己拨号或直连公网（家里少见）。
2. **路由器 WAN 口地址**：用 UPnP-IGD `GetExternalIPAddress`、NAT-PMP（RFC 6886）或 PCP（RFC 6887）问网关。若 WAN 口是 100.64/10 或私网地址 = **运营商 CGNAT / 双重 NAT，IPv4 入站基本不可能**（除非向运营商要公网 IP，见本节下面的申请情况）。PCP 也能控制 IPv6 防火墙（RFC 6887 §1 列出「IPv6 and IPv4 firewall devices」、§2.1「IPv4 and IPv6 simple firewall control」），但国内光猫是否支持【未查到】。
   来源：https://www.rfc-editor.org/rfc/rfc6887.html （查于 2026-10-30）。
3. **外部看到的地址**：STUN（RFC 8489）Binding 或一个 HTTPS「回显来源地址」的接口；和第 2 步的 WAN 地址不一致 = 中间还有一层 NAT。
- 【事实，网友报告】公网 IPv4 越来越难拿：2021 年四川电信 299 元以下套餐只给私网；2025–2026 年仍有用户靠投诉 / 加钱（如每月 10 元）拿到电信公网 v4，移动家宽多数不给。
  来源：https://www.v2ex.com/t/808536 、https://juejin.cn/post/7647395004020932654 、https://mao.fan/article/432 （查于 2026-10-30，个人经验）。
- 【判断】IPv4 只作为「锦上添花」的候选：自动发现成功（有公网 + 端口映射可用）才发布；否则只发布 IPv6 + 内网地址。UPnP 自动开端口有安全争议，默认不开，作为显式选项。

### 2.6 监听变化

- 【判断】
  - 主路径：netlink 订阅 `RTNLGRP_IPV6_IFADDR` / `RTNLGRP_IPV4_IFADDR`（`AddrSubscribeWithOptions`，`ListExisting: true`），收到事件后**去抖 2–5 秒**再重算（SLAAC 换前缀时会连续来 TENTATIVE → 正常、旧地址 DEPRECATED 等多条消息）。
  - 兜底：每 1–5 分钟全量重扫一次（netlink 组播在接收缓冲满时会丢消息，`ENOBUFS`）；`AddrList` 返回 `ErrDumpInterrupted` 时重试。
  - 公网 IPv4 的变化本机收不到事件（变的是路由器 WAN 口），只能定时用 STUN / UPnP 查。
  - 变化后：更新 DDNS（第 3 节）、通过 WS 给在线客户端推新的候选地址（第 3、4 节），写日志。
- 【判断】**hub 自己测不了「外面能不能连进来」**：本机连自己的全局地址走的是 `local` 表、在本机环回（ip-route(8)：「the destinations are assigned to this host. The packets are looped back and delivered locally」，来源 https://man7.org/linux/man-pages/man8/ip-route.8.html ，查于 2026-10-30），根本不经过光猫防火墙。外部可达性只能由外面的点来测：
  - 手机在蜂窝网络上（真实路径，最可信）；
  - 不在同一个家庭网络里的另一台 node 机器（hub 已经经 SSH 管着各台电脑，可以让它 `connect` 一下 hub 的公网地址回报结果）；
  - 第三方端口检测服务（会泄露地址和端口，不推荐默认用）。

## 3. 地址会变怎么办：DDNS 与推送

### 3.1 各家 DNS 的更新接口和 TTL（事实）

| 服务 | 更新接口 | 最小 TTL | 来源（查于 2026-10-30） |
| --- | --- | --- | --- |
| Cloudflare | `PATCH /zones/{zone_id}/dns_records/{dns_record_id}`，令牌权限「DNS Write」，可限定到单个 zone | 60–86400 秒，`1` = 自动；Enterprise 可到 30 | https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/edit/ |
| 阿里云云解析 | `UpdateDomainRecord`（API 文档本次未抓到原文） | **免费版、个人版 600 秒**；企业旗舰版 / 尊享版 1 秒 | https://help.aliyun.com/zh/dns/pubz-add-parsing-record |
| DNSPod / 腾讯云 | API 3.0 `ModifyDynamicDNS`（`dnspod.tencentcloudapi.com`，Version 2021-03-23，参数 Domain、SubDomain、RecordId、RecordLine、Value、Ttl；默认限频 20 次/秒，经搜索摘要转述） | **免费版 600 秒**、专业版 60、企业版 / 尊享版 1；同主机同线路多条记录取最大 TTL | https://docs.dnspod.cn/dns/help-ttl/ （2025-05-21 更新）；https://cloud.tencent.com/document/api/1427/56158 |
| dynv6（免费子域名） | `https://dynv6.com/api/update?zone=…&token=…&ipv6=…`，可同时更新前缀；也支持 RFC 2136 + TSIG | 未查 | https://dynv6.com/docs/apis |
| deSEC（免费） | `update.dedyn.io` dynDNS 接口，按来源地址或参数更新 A / AAAA | 未查 | https://desec.readthedocs.io/en/latest/dyndns/update-api.html |
| DuckDNS（免费） | `…/update?domains=…&token=…&ipv6=…`（仅论坛用户说法，官方 spec 未抓到） | 未查 | https://forum.netgate.com/topic/188368/duckdns-ipv6-update-url |

- 【事实】现成的开源实现 ddns-go 支持阿里云、腾讯云、DNSPod、Cloudflare、华为云、deSEC 等二十多家，默认每 5 分钟同步；IPv6 支持「接口 / 网卡 / 命令」三种取地址方式，网卡方式按序号（`@1`…）或正则挑地址。【未查到】它是否按 `IFA_F_TEMPORARY` 排除临时地址；有用户在 Docker 里跑 ddns-go 时 IPv6 记录「基本 5 分钟一次」地变（原因帖内未定论）。
  来源：https://github.com/jeessy2/ddns-go ；https://github.com/jeessy2/ddns-go/discussions/983 （查于 2026-10-30）。
- 【判断】hub 自己实现 DDNS 更新（不依赖 ddns-go）：地址选择要用第 2 节带标志的规则，这一点通用 DDNS 工具做不好；DNS 服务商做成可插拔（Cloudflare / 阿里云 / DNSPod / 通用 HTTP 回调 URL / RFC 2136），凭据只放 `hub.json`（0600），令牌权限收窄到一个 zone 的 DNS 写。Go 生态可参考 `libdns`（Caddy 用的 DNS 抽象，有 cloudflare / alidns / tencentcloud 等实现）——【未核实】具体包的维护状态。

### 3.2 DDNS 的取舍【判断】

- 好处：
  - 名字不变，手机离线期间地址变了也能找回来；
  - 可以配合 Let's Encrypt DNS-01 拿正式证书（第 6 节）；
  - 换前缀后手机只要重新解析。
- 代价 / 风险：
  - 国内两家的免费版 TTL 最低 **600 秒**：地址刚变的 10 分钟里，缓存了旧记录的解析器会给旧地址。所以 App **不能只靠 DNS**：hub 推送的字面地址优先，DNS 只是「全部候选都失败时」的找回手段。App 可以绕开系统缓存自己做 DoH 查询拿新记录（要实测各家解析器是否尊重 TTL，【未查到】国内解析器超 TTL 缓存的数据）。
  - 名字公开：任何人查 DNS 就能拿到家里的地址；证书透明日志（CT）会公开证书里的域名，扫描器会顺着找上门（第 7 节）。用通配证书 `*.example.com`（DNS-01）可以不暴露具体子域名。
  - 依赖第三方：DNS 服务商的 API 不可用时地址更新会延迟；免费 DDNS 服务的可用性和国内访问速度未知。
  - 双栈记录：只在确认外部可达时才发布 A 记录；家宽没有可用的 IPv4 入站时**不要发布 A 记录**，否则客户端（尤其「先试 v4」的实现）会先在 v4 上超时。

### 3.3 没有域名：只靠「上次成功的地址 + hub 推送的新地址」够不够

- 【事实】PPPoE 重拨时 IPv4 和 IPv6 前缀一起变（「重新拨号换一个新的 ipv4，v6 前缀就会变，这两个是同步的」，V2EX 1250020 #19）；运营商侧换前缀属于「闪断式重编号」：RFC 8978 指出主机会继续保留并可能使用旧前缀的地址，按 RFC 4861 默认寿命「could take hosts up to 7 days (one week) to deprecate」，因为未认证的 RA 不能把有效寿命降到 2 小时以下。
  来源：https://www.v2ex.com/t/1250020 ；https://www.rfc-editor.org/rfc/rfc8978.html （查于 2026-10-30）。
- 【判断】由此：
  1. **旧连接在变化的一瞬间就断了**，hub 没法经旧连接把新地址告诉手机。「hub 推送新地址」只在两种情况下有效：新旧地址有重叠期（平滑重编号，旧前缀先 DEPRECATED 再消失）；或者有一条不依赖旧地址的旁路——**iOS 有 APNs**（hub 往外发，不受家里地址变化影响），Android 没有 Google 服务时**没有这条旁路**。
  2. 手机不在线时地址变了（App 被杀、长时间离线），回来后手里只有死地址：要么等回到家里 Wi-Fi 用内网地址重连，要么重新扫码。按 1.3 的报告（有的地区一天换两次前缀），这在没有域名时会**经常发生**。
  3. hub 自己也可能「看错」：闪断重编号后旧地址在本机上仍未废弃（RFC 8978），hub 会以为它还有效。所以 hub 发布**所有**未废弃的候选（按 `IFA_CACHEINFO` 的 `tstamp`——最后一次被 RA 刷新的时间——排序，久未刷新的排后面），由手机并发探测决定（第 4 节）。
- 【判断】结论：**没有名字的「上次成功地址 + 推送」只能算降级模式**，适合「主要在家里用、偶尔在外面」的人。要稳定的公网直连，至少要一个能自动更新的名字。可选的三档：
  1. 自己的域名 + Cloudflare / 阿里云 / DNSPod（推荐，顺带解决 TLS 证书）；
  2. 免费 DDNS 子域名（dynv6 / deSEC / DuckDNS，支持 AAAA）——零成本，但可用性、国内解析速度要实测；
  3. **签名的地址公告**：hub 用自己的身份密钥（第 6 节 TLS 用的那把）给当前候选列表签名，写到任意一个用户指定的「公告板」（某个 DNS TXT 记录、对象存储里的一个文件、用户自己的一台有固定地址的服务器……）；App 在全部候选失败时去取，用配对后存下的 hub 公钥验签后使用（二维码里只有公钥哈希，完整公钥在第一次 TLS 连接时拿到并存下）。完整性靠签名，不用信任存储方；缺点是又多一个依赖，而且公告内容暴露家里地址。
- 【判断】（以后 iOS 跟进时）iOS 可以额外用 APNs 后台推送把新候选送到手机（hub 已经在用 APNs，hub 侧可以本轮先做，iOS 侧以后接）；但后台推送不保证送达、会被限频（Apple 文档「don't try to send more than two or three per hour」，App 有 30 秒执行时间——**未能直接抓取 Apple 页面原文**，按广泛引用的原文记录，需核对 https://developer.apple.com/documentation/usernotifications/pushing-background-updates-to-your-app ）。

## 4. 手机端多地址选路

### 4.1 Happy Eyeballs 的要点（事实）

- 【事实】RFC 8305（Happy Eyeballs v2）：
  - 连接尝试间隔（Connection Attempt Delay）「One recommended value for a default delay is 250 milliseconds」，最小推荐 100 ms、不得小于 10 ms，最大推荐 2 秒；
  - A 先回来时等 AAAA 的 Resolution Delay 推荐 50 ms；地址族交错时 First Address Family Count 推荐 1；
  - 有历史 RTT 时「SHOULD add a Destination Address Selection rule … that prefers addresses with lower RTTs」，还应优先用过的地址；**历史不能跨网络接口共用，换网络时要清掉**（§4）。
  来源：https://www.rfc-editor.org/rfc/rfc8305.html （查于 2026-10-30）。
- 【事实】Happy Eyeballs v3（加入 SVCB/HTTPS 记录、QUIC）仍是 IETF HAPPY 工作组草案（`draft-ietf-happy-happyeyeballs-v3`，-03 为 2026-03，2026-07 IETF 126 仍在讨论），**没有成为 RFC**。
  来源：https://datatracker.ietf.org/doc/draft-ietf-happy-happyeyeballs-v3/ ；https://www.ietf.org/archive/id/draft-ietf-happy-happyeyeballs-v3-03.html （查于 2026-10-30）。

### 4.2 现成能力（事实）

**Android / OkHttp 5**
- 【事实】OkHttp 5 的 `fastFallback` **默认开启**（5.0.0-alpha.11 起；源码注释「Defaults to enabled, call with fastFallback = false to revert to 4.x behaviour」），对**同一个主机名解析出的多个地址**并发尝试，changelog 写 IPv6 先跑 250 ms。它只在「一个 URL 的多个 IP」之间赛跑，**不会在不同 URL（不同主机 / 端口）之间选**。
  来源：https://github.com/square/okhttp/blob/master/CHANGELOG.md ；`okhttp3/OkHttpClient.kt`（master，查于 2026-10-30）。
- 【事实】OkHttp `pingInterval` 默认 0（不发客户端 ping）；设了以后「If the server does not respond to each ping with a pong within interval, this client will assume that connectivity has been lost」；收到服务端的 ping 会自动回 pong（`RealWebSocket.onReadPing`）。来源：同上 `OkHttpClient.kt`、`internal/ws/RealWebSocket.kt`（查于 2026-10-30）。
- 【事实】`LinkProperties` 公开 API 里**没有** `hasGlobalIpv6Address()` / `hasIpv6DefaultRoute()` / `isIpv6Provisioned()`（都是 `@hide` / SystemApi）；公开的有 `getLinkAddresses()`（`LinkAddress` 带 `getFlags()`、`getScope()`）、`getRoutes()`（`RouteInfo.isDefaultRoute()`）、`getDnsServers()`、`isPrivateDnsActive()` / `getPrivateDnsServerName()`（API 28）、`getNat64Prefix()`（API 30）、`getDhcpServerAddress()`（API 30）。
  来源：https://developer.android.com/reference/android/net/LinkProperties （页面过大，WebFetch 只拿到导航；以上为工具按 SDK / AOSP 知识整理的结果，**需对照实际 SDK 核对**，查于 2026-10-30）。
- 【判断】所以「当前网络有没有 IPv6」要自己算：`linkAddresses` 里有 scope = universe、不在 `fc00::/7`、不带 `DEPRECATED` / `DADFAILED` 的 IPv6 地址，且 `routes` 里有 IPv6 默认路由。再加一条更可靠的：**真去连一下**（探测本身就是答案）。

**iOS（以后 iOS 跟进时）**
- 【事实】`NWPath.supportsIPv6`：「A Boolean indicating whether the path can route IPv6 traffic.」；`isExpensive`、`isConstrained`（低数据模式）；`NWPath` iOS 12 起。来源：https://developer.apple.com/tutorials/data/documentation/network/nwpath.json （查于 2026-10-30）。
- 【事实】Apple：在 DNS64/NAT64 网络上，iOS 9 起 NSURLSession / CFNetwork 会把 IPv4 字面地址在本机合成 IPv6；直接用 socket 时不会（Apple Sample Contact在论坛的说明）；Apple 建议用 DNS 名而不是字面地址。来源：https://developer.apple.com/library/archive/documentation/NetworkingInternetWeb/Conceptual/NetworkingOverview/UnderstandingandPreparingfortheIPv6Transition/UnderstandingandPreparingfortheIPv6Transition.html ；https://developer.apple.com/forums/thread/5643 ；https://developer.apple.com/support/ipv6 （查于 2026-10-30，经搜索摘要转述）。
- 【未查到】`NWConnection` 对 IPv4 字面地址是否同样合成（Apple 文档没写）。

### 4.3 推荐的选路算法【判断】

1. **候选来源**：
   - hub 下发的列表（鉴权后的新接口、WS `hello` 里的新字段；**不放免鉴权的 `/health`**，见 9.1）：每项含 `url`、`kind`（`lan` / `ipv6` / `ipv4` / `name` / `vpn`）、`family`、`port`、`seen_at`（hub 最近一次确认它还在网卡上 / RA 刷新的时间）；
   - 上次在**当前网络**上成功的地址；
   - 域名（若配置）解析出的 A / AAAA；
   - 配对时二维码里的地址。
2. **按当前网络过滤和排序**：
   - 蜂窝上不试私网 / ULA 地址（必然超时；Android 17 上也省得触发本地网络权限）；
   - 当前网络没有 IPv6 时 IPv6 字面地址排最后（不完全排除，判断可能有误）；
   - Wi-Fi 上：内网地址优先（同一个家）；
   - 排序：本网络上次成功的 > 内网 > 全局 IPv6 > 域名 > 公网 IPv4。
3. **错峰并发**：第一个立即开始，之后每 250 ms 起一个，**最多同时 3–4 个**。每个尝试必须完成「TCP + TLS + 证书钉扎通过 + `GET /health` 返回的 `hub_id` 等于配对时记下的」才算赢。第一个赢的用于 WebSocket 和 REST，其余取消。总超时：Wi-Fi 约 5 秒、蜂窝约 8 秒，之后进入退避。
4. **缓存**：按「网络身份」记住赢家。Android 上拿 SSID 要定位权限（Android 10+ / target 31+ 的 `NetworkCallback` 默认脱敏，需 `FLAG_INCLUDE_LOCATION_INFO` + 定位权限，【未抓到官方原文】），所以用「传输类型 + 网关 / DHCP 服务器地址 + 本机 IPv6 前缀（/64）」的哈希做键，不需要定位权限。RFC 8305 要求换网络时清掉历史，这里按网络分开存即可。
5. **何时重新探测**：
   - `registerDefaultNetworkCallback` 的 `onAvailable` / `onLost`、`onLinkPropertiesChanged`（地址变了）、`onCapabilitiesChanged`（`VALIDATED` / `CAPTIVE_PORTAL` 变化）；
   - 当前连接断开；
   - hub 推送新的候选列表；
   - 定期「升级检查」：比如在蜂窝上用的是 IPv4 或域名，每 10–30 分钟后台探一次更优的候选，赢了再切（切换期间旧连接不断）。
6. **身份校验先于发送 token**：hub 的旧前缀可能已经被运营商分给了别的用户，旧地址上可能是别人的机器。**明文 HTTP 下把 Bearer token 发给一个旧地址，等于把 token 交给陌生人**，这是公网直连必须上 TLS + 钉扎的最直接理由（第 6 节）。

### 4.4 坑（事实 + 判断）

- **DNS64 / NAT64 / 464XLAT**：【事实】国内三家都在做 IPv6 单栈 / NAT64 的试点，电信 5G 用户面用 464XLAT + DNS64（1.1 节）。Android 从 4.3 起内置 CLAT（clatd），在 IPv6 单栈网络上给 IPv4 字面地址兜底（来源：https://github.com/ddrown/android_external_android-clat ；T-Mobile 464XLAT 报告 https://conference.apnic.net/data/37/464xlat-apricot-2014_1393236641.pdf ，查于 2026-10-30）。【判断】hub 的公网 IPv4 字面地址在单栈手机上靠 CLAT / NAT64 仍可达；域名只有 AAAA 时不受影响。DNS64 合成的 `64:ff9b::/96` 地址不要当成「hub 的 IPv6」缓存。
- **强制门户（captive portal）**：Wi-Fi 未认证时连接会被劫持或重定向；【判断】看 `NET_CAPABILITY_CAPTIVE_PORTAL` / 未 `VALIDATED` 时直接提示「这个 Wi-Fi 需要先登录」，不要无限重试；TLS 钉扎会挡住劫持（不会把 token 发出去）。
- **Wi-Fi 有 IPv6、蜂窝没有（或反过来）**：每次换网都按 4.3 第 2 步重新过滤；不要把「上次在 Wi-Fi 上 IPv6 成功」带到蜂窝上。
- **在家里连自己的公网 IPv4 要靠路由器 NAT 回环**，很多家用路由器不支持：在家优先内网地址或全局 IPv6（同一 /64 直连，不经 NAT）。
- **手机上开着别的代理 / VPN App**：Clash 一类的 fake-ip 模式会把域名解析成 `198.18/15`（基准测试网段） 并接管流量，直连失效或绕远。【判断】用户说 VPN 自己管，App 只在诊断页显示「当前经 VPN（`TRANSPORT_VPN`）」，并优先用 hub 下发的字面地址（不经 fake-ip DNS）。
- **IPv6 路径 MTU 黑洞**：【事实，网友报告】有用户在上海电信 IPv6 问题帖里被建议把 v6 MSS 钳到 1420（https://www.v2ex.com/t/1250020 #18、#28，查于 2026-10-30）。【事实】Linux `TCP_MAXSEG` 在连接建立前设置会改变通告给对端的 MSS（tcp(7)，https://man7.org/linux/man-pages/man7/tcp.7.html ，查于 2026-10-30）。【判断】症状是「TCP 连上了、TLS 握手卡住」（证书链大包丢了）。hub 的公网监听可以选配一个较小的 MSS（如 1360–1420，【未实测】），探测把「握手超时」单独分类上报。
- **运营商 DNS**：【未查到】国内运营商解析器超 TTL 缓存的系统性数据；App 对域名候选可以自己做 DoH 查询拿新记录（第 3 节）。

## 5. Android 17 本地网络权限和公网 IPv6

### 5.1 官方文档原文（事实）

- 【事实】「Local Network Definition」（最后更新 2026-03-06）原文：
  > A local network in this project refers to an IP network that utilizes a broadcast-capable network interface, such as Wi-Fi or Ethernet, but excludes cellular (WWAN) or VPN connections.

  IPv4：`169.254/16`、`100.64/10`、`10/8`、`172.16/12`、`192.168/16`；IPv6：「Link-local」「**Directly-connected routes**」「Stub networks like Thread」「Multiple-subnets」；外加组播（224/4、ff00::/8）和受限广播地址（全 1）。页面没有提到全局 IPv6、NAT 回环、环回地址。
  来源：https://developer.android.com/privacy-and-security/local-network-definition （查于 2026-10-30）。
- 【事实】「Local network permission」（最后更新 2026-10-02）：
  - 「Starting in Android 17, local network protections are mandatory and enforced for apps targeting Android 17 or higher」；target ≤ 36 时由 `INTERNET` 隐式授予，「don't add ACCESS_LOCAL_NETWORK to your manifest」；
  - 限制「apply to **all networking APIs** … Cronet and OkHttp」；出站 / 入站 TCP、UDP 都要权限；
  - 没权限时「TCP Connections will typically result in a **timeout error**」，NDK 可用 `android_getnetworkblockedreason(sockFd)` 得到 `ANDROID_NETWORK_BLOCKED_REASON_LNP`；
  - 例外：本地 DNS 服务器的 53 端口；Output Switcher；
  - 页面没有讲全局 IPv6 或 NAT 回环，只在「Bugs」里列了「Discrepancies in LAN access」。
  来源：https://developer.android.com/privacy-and-security/local-network-permission （查于 2026-10-30）。

### 5.2 AOSP 实现（事实，android-17.0.0_r1 源码）

- 【事实】`ConnectivityService.getEffectiveLocalPrefixes(lp)`：对链路上**每个本机地址取它的 on-link 前缀**，`isPrefixLocal` 为真就加入「本地」集合；`isPrefixLocal` 对 IPv6 的实现是 `return prefix.getPrefixLength() != 0;`——**任何非 0 长度的 IPv6 on-link 前缀都算本地，包括全局 /64**；对 IPv4 只有落在 `IPV4_LOCAL_PREFIXES`（169.254/16、100.64/10、10/8、172.16/12、192.168/16）里的 on-link 前缀才算。按路由（Thread、覆盖本机地址的非默认路由）的规则只在 `isLnpDeveloperOptInEnabled()` 时生效，代码里还留着 `// TODO: Add all directly-connected routes.`。
  来源：https://android.googlesource.com/platform/packages/modules/Connectivity/+/refs/tags/android-17.0.0_r1/service/src/com/android/server/ConnectivityService.java （查于 2026-10-30，`?format=TEXT` 下载后阅读）。
- 【事实】BPF（`bpf/progs/netd.c`）按 `(接口, 远端地址, 协议, 端口)` 在 `local_net_access_map`（LPM trie）里做最长前缀匹配：**查不到就放行**；查到且为「阻断」、而该 uid 没有 `ACCESS_LOCAL_NETWORK` 就丢包。
  来源：https://android.googlesource.com/platform/packages/modules/Connectivity/+/refs/tags/android-17.0.0_r1/bpf/progs/netd.c （查于 2026-10-30）。
- 【事实】排查命令：`adb shell dumpsys connectivity trafficcontroller` 会列出每个接口被标成本地的前缀（sing-box issue 里的用法）。来源：https://github.com/SagerNet/sing-box/issues/4676 （查于 2026-10-30）。

### 5.3 对 Jarvis 的结论【判断，基于 5.1 原文 + 5.2 源码；必须真机验证】

| 场景（target 37，没给权限） | 是否算本地网络 | 依据 |
| --- | --- | --- |
| 家里 Wi-Fi，连 hub 的内网 IPv4（同一网段，如 192.168.x.0/24） | **是**，被拦 | 定义的 IPv4 列表；源码 on-link 前缀 |
| 家里 Wi-Fi，连 hub 的**全局 IPv6，与手机在同一个 /64** | **是**，被拦 | 定义的「Directly-connected routes」；源码对 IPv6 on-link 前缀一律算本地 |
| 家里 Wi-Fi，hub 的全局 IPv6 在**另一个 /64**（二级路由后面） | 按 17.0.0_r1 源码**不算**（不在 on-link 前缀里，路由规则未默认开启）；定义里的「Multiple-subnets」将来可能覆盖 | 源码 + 定义有歧义 |
| 家里 Wi-Fi，连 hub 的**公网 IPv4**（靠路由器 NAT 回环） | **不算**（目的地址不在任何本地前缀里）；但很多路由器不支持回环 | 源码；定义的 IPv4 列表不含公网 |
| 家里 Wi-Fi，hub 的内网 IPv4 在**另一个网段**（如 192.168.10.x，手机在 192.168.1.x/24） | 按源码**不算**（只拦 on-link 前缀），但定义页列的是整个 192.168/16 | 源码与文档不一致，以真机为准 |
| 蜂窝网络，连 hub 的任何公网地址 | 不算 | 定义排除 WWAN |
| 经 VPN（如 Tailscale 100.x）连 hub | 定义排除 VPN；源码只把 VPN 接口自己的 on-link 前缀算本地——Tailscale 地址若是 /32 就只含自己，**若某 VPN 把地址配成 /10 之类，整个网段都会被当本地**【未实测】 | 定义 + 源码推断 |

- **要点**：「改连公网 IPv6 就能绕开本地网络权限」**在家里不成立**——同一 /64 里的全局地址照样算本地。所以 target 37 的 Android 版**必须走权限流程**（android.md 5.1 已有设计：扫码后先申请，被拒显示「需要允许 Jarvis 访问本地网络」）。
- **被拒时的兜底**（【判断】）：
  1. 若 hub 有公网 IPv4 且路由器支持回环，用它（不算本地）；
  2. 若 hub 在另一个 /64，用全局 IPv6；
  3. 最后一招：显式在蜂窝网络上连 hub 的公网地址（`ConnectivityManager.requestNetwork(TRANSPORT_CELLULAR)` 拿到 `Network`，OkHttp 用 `network.socketFactory`）——在家却走流量，费电费流量，只作为用户确认后的选项。
- **探测要能区分「被本地网络权限拦了」和「hub 挂了」**：都表现为 TCP 超时。target 37 时，Wi-Fi 上的本地候选超时而公网候选成功 / 失败的组合，加上 `checkSelfPermission(ACCESS_LOCAL_NETWORK)`，足以判断；需要更精确时用 NDK 的 `android_getnetworkblockedreason`。
- **Play 政策**：target 37+ 申请该权限要在 Play Console 声明，2027-08-25 起强制（已在 `/tmp/jarvis-redesign/research/android-tech.md` 记录，本笔记不重复核实）。

## 6. TLS

### 6.1 为什么公网直连必须上 TLS【判断】

- Bearer token 走明文，任何一跳（运营商、公共 Wi-Fi、被劫持的 DNS）都能拿到；
- 更具体的风险：前缀换了以后，**hub 的旧地址可能已经分给了别人**（4.3 第 6 条），手机按旧地址连过去会把 token 发给陌生人；
- 所以公网路径上「先验证对方身份，再发 token」是硬要求。内网明文为兼容保留（第 9 节），但新客户端只要 hub 支持就优先走 TLS。

### 6.2 方案 A：hub 自签证书 + 配对码里带公钥指纹（SPKI SHA-256），手机钉扎

**事实**
- 【事实】OkHttp 原文：「CertificatePinner can not be used to pin self-signed certificate if such certificate is not accepted by javax.net.ssl.TrustManager」——**自签证书不能只靠 `CertificatePinner`**，因为默认 TrustManager 先就拒了；`CertificatePinner` 按主机名 / 通配模式配置，pin 格式 `sha256/<base64>`（SPKI）。
  来源：OkHttp master `okhttp3/CertificatePinner.kt` 类注释（查于 2026-10-30）。
- 【事实】OkHttp 默认 `OkHostnameVerifier`：主机是 IP 字面量时只认证书 SAN 里的 **iPAddress**（类型 7）条目，规范化后比对。来源：`okhttp3/internal/tls/OkHostnameVerifier.kt`（查于 2026-10-30）。→ 用 IP 连接且证书 SAN 里没有这个 IP 时，默认主机名校验会失败。
- 【事实】Google Play 对「什么都不校验」的 `X509TrustManager` 和无条件 `SslErrorHandler.proceed()` 的 `onReceivedSslError` 会拦截上架；要求「在证书不符合预期时抛 `CertificateException`」「符合预期才 proceed，否则 cancel」。Lint 有 `TrustAllX509TrustManager` 检查（扫字节码，包括依赖）。
  来源：https://googlesamples.github.io/android-custom-lint-rules/checks/TrustAllX509TrustManager.md.html ；https://experienceleague.adobe.com/en/docs/experience-cloud-kcs/kbarticles/ka-17526 （转述 Google 通知原文）（查于 2026-10-30）。
- 【事实】Apple（以后 iOS 跟进时）：手动服务器信任评估用 `urlSession(_:didReceive:completionHandler:)` 处理 `NSURLAuthenticationMethodServerTrust`，校验通过后 `.useCredential(URLCredential(trust:))`，否则 `.cancelAuthenticationChallenge`；「You cannot loosen server trust requirements for an ATS-protected domain, but you can tighten them」；建议「Compare the server's credentials against a public key, instead of storing a single certificate」「Use a set of keys or CAs, so you can rotate」。现有 iOS App 的 `Info.plist` 是 `NSAllowsArbitraryLoads = true`（本仓库 `ios/Jarvis/Resources/Info.plist`），所以放宽不受 ATS 限制。
  来源：https://developer.apple.com/documentation/foundation/performing-manual-server-trust-authentication （JSON 版，查于 2026-10-30）。

**hub 侧怎么做【判断】**
- 第一次运行生成一把长期的 **hub 身份密钥**（ECDSA P-256），按仓库约定和 token 一样只放 hub 机器（`hub.json` 或同目录 0600 文件，不进数据库）。
- 用它签一张自签证书（有效期例如 1 年，到期前自动用**同一把密钥**重签——钉的是公钥不是证书，重签不影响手机）。SAN 放一个固定名字（如 `<hub_id>.jarvis.invalid`），不放会变的 IP。
- 二维码 / 配对数据带 `fp=sha256/<SPKI 的 base64>` 和 `hub_id`；`/health`、WS `hello` 也下发当前 `fp` 和「下一把密钥」的 `fp_next`（预告轮换，手机同时信两把）。
- 用 IP 字面量连接时客户端不会发 SNI（RFC 6066 §3：「Literal IPv4 and IPv6 addresses are not permitted in "HostName"」，https://www.rfc-editor.org/rfc/rfc6066.html ，查于 2026-10-30），Go `crypto/tls` 的 `GetCertificate` 里 `ServerName` 为空时返回这张证书即可。

**Android 侧怎么做【判断】**
- 给 hub 专用一个 `OkHttpClient`：
  - 自定义 `X509TrustManager`（继承 `X509ExtendedTrustManager` 更稳）：取 `chain[0].publicKey.encoded`（Java 里就是 SubjectPublicKeyInfo 的 DER）做 SHA-256，与配对时存下的 pin 集合做常量时间比较，不匹配就抛 `CertificateException`——满足 Play 的要求；
  - `hostnameVerifier` 只对这个客户端放开（身份已由公钥确认；IP 会变，不能指望 SAN）；
  - 可以再挂 `CertificatePinner`，但它对自签证书不起额外作用。
- 也可以用 okhttp-tls 的 `HandshakeCertificates.Builder().addTrustedCertificate(hubCert)` 把 hub 证书当根信任——但这样钉的是整张证书，重签就要重新配对，不如钉公钥。
- **所有到 hub 的流量都要过这个客户端**：REST、WebSocket（同一个 `OkHttpClient`，TLS 设置相同）、图片加载（Coil / Glide 要注入这个 client）、文件上传下载。
- **WebView 不走 OkHttp**：如果 App 内浏览器要加载 hub 代理的资源（飞书媒体等），用 `shouldInterceptRequest` 经 OkHttp 取，或在 `onReceivedSslError` 里用 `SslError.getCertificate().getX509Certificate()`（API 29+）校验公钥后再 `proceed()`。
- Network Security Config 的 `<pin-set>` 只能按域名写死，hub 地址是配对时才知道的，**不适用**。

**坑【判断】**
- 指纹只能从二维码来：如果用户手动输入地址（没扫码），第一次连接是 TOFU（首次信任），要让用户在 hub 终端上核对指纹的前几位，或者走「一次性配对码 + 双方各自算出的确认码」（第 7 节）。
- 密钥丢了（hub 机器重装）= 所有手机要重新配对：在文档里写清楚，或者支持「旧密钥签名的新密钥公告」。

### 6.3 方案 B：Let's Encrypt 的 IP 地址证书

- 【事实】2026-01-15 起正式可用（GA）：「IP address certificates must be short-lived certificates」，有效期 **160 小时**（约 6 天），只能用 `shortlived` profile；支持 IPv4 和 IPv6。
  来源：https://letsencrypt.org/2026/01/15/6day-and-ip-general-availability ；https://letsencrypt.org/docs/profiles/ （查于 2026-10-30）。
- 【事实】IP 证书「you can't use the DNS challenge method」「only the http-01 and tls-alpn-01 methods can be used」；HTTP-01「can only be done on port 80」，TLS-ALPN-01 走 **443**。
  来源：https://letsencrypt.org/2025/07/01/issuing-our-first-ip-address-certificate ；https://letsencrypt.org/docs/challenge-types/ （查于 2026-10-30）。
- 【判断】**不适合 Jarvis**：
  - 要从公网验证 80 或 443——国内家宽这两个端口经常被封（1.5 节），而且还要再对外开一个端口；
  - 证书绑定 IP，前缀一天变几次时每次都要重签；6 天有效期意味着验证端口要一直开着；
  - hub 换前缀到证书重签之间的这段时间，手机看到的是不匹配的证书。

### 6.4 方案 C：有域名时用 ACME DNS-01

- 【事实】DNS-01 支持通配证书、不要求服务器能从公网访问；缺点是「Keeping API credentials on your web server is risky」，建议用权限受限的令牌。来源：https://letsencrypt.org/docs/challenge-types/ （查于 2026-10-30）。
- 【事实】Let's Encrypt 有效期在缩短：2027-02-10 起默认 **64 天**（授权复用 30 天 → 10 天），2028 年默认 45 天（授权复用 7 小时）；建议用 ARI 或「约 ⅔ 寿命时续期」，别写死「60 天」。
  来源：https://letsencrypt.org/2026/10/07/64-day-certs （查于 2026-10-30）。
- 【事实】Go 库 `github.com/go-acme/lego/v4`：`ObtainRequest.PrivateKey` 可传入自己的私钥（「A new private key is generated for every invocation of the function Obtain. If you do not want that you can supply your own private key」），也有 `ObtainForCSR`；DNS 提供方有 `cloudflare`、`alidns`、`dnspod` 等（`tencentcloud` 本次未确认）。
  来源：https://pkg.go.dev/github.com/go-acme/lego/v4/certificate ；https://go-acme.github.io/lego/dns/ （查于 2026-10-30）。注意 lego 文档近期换了 CLI 风格，引入前确认模块路径还是 `/v4`。
- 【判断】做法：用 **hub 身份密钥**申请证书（续期复用同一把），这样手机仍可按同一个 SPKI 钉扎，同时证书在公共信任链下也有效（WebView、浏览器、curl 都能直接用）。用通配证书 `*.example.com` 避免在 CT 日志里暴露 hub 的具体子域名。DNS 令牌和 DDNS 共用一个、权限收窄到单个 zone。

### 6.5 推荐【判断】

1. **默认：方案 A（自签 + SPKI 钉扎）**。不依赖域名、端口、CA；二维码本来就要扫，指纹放进去零成本。Android 首版就做。
2. **可选增强：方案 C**（用户配了域名和 DNS 令牌时自动开启）。证书换成公共 CA 签的，但**仍用同一把 hub 密钥**，手机端的钉扎逻辑不变，只是多了一条「名字」候选。
3. **不做：方案 B**（IP 证书）。
4. 兼容：明文 `:18660` 继续给内网和现有 iOS App 用；TLS 用单独的端口（如 `:18661`，可配），见第 9 节。

## 7. 个人服务器暴露在公网的加固

### 7.1 暴露面从哪来（事实）

- 【事实】IPv6 的 /64 无法暴力扫描（RFC 7707 §4.2「brute-force address-scanning attack of an entire /64 network would be infeasible」），但地址会从这些地方泄露：
  - 公开 DNS（§5.1.1：公开在 DNS 里的主机「will remain open to probing」），也就是 DDNS 名字；
  - 可预测的接口标识：低字节地址（`::1`、`::2`）搜索空间约 2^8–2^16（§4.1.3.1），实测 56%–93% 的服务器用低字节地址（§4.1.5）；EUI-64（MAC 推出，已知厂商 OUI 时约 2^24）；
  - 应用层泄露（邮件头、P2P 等）。
  来源：https://www.rfc-editor.org/rfc/rfc7707.html （查于 2026-10-30）。
- 【事实】证书透明日志（CT）：IMC 2018 的蜜罐实验里，子域名只经 CT 日志泄露，**发证后 73 秒到约 3 分钟**就出现第一批 DNS 查询，随后有端口扫描；EuroS&P 2023 用 200 天的蜜罐复现，扫描在证书签发和续期后几乎立即激增。
  来源：https://dl.acm.org/doi/10.1145/3278532.3278562 ；https://sites.cs.ucsb.edu/~chris/research/doc/eurosnp23_certvuln.pdf （查于 2026-10-30）。
- 【事实】光猫多数只有「IPv6 防火墙 开 / 关」两档（1.4 节）——关掉后 hub 机器上**所有**监听 `::` 的服务都直接暴露（SSH、lark-connector 等其他容器……）。Docker 官方文档：「Docker and ufw use firewall rules in ways that make them incompatible」，经 Docker 发布的端口「gets diverted before it goes through the ufw firewall settings」。本仓库的 Postgres 容器已经只绑 `127.0.0.1`（`scripts/db.sh`）。
  来源：https://docs.docker.com/engine/network/packet-filtering-firewalls/ （查于 2026-10-30）。
- 【判断】由此：
  1. **固定接口标识用随机的 64 位值**，不要用 `::1` / `::1234` 这种低字节，也不要 EUI-64（2.3 节建议用固定 token 时同样适用）；
  2. 有域名时用通配证书，别让 hub 的子域名进 CT；
  3. 「改端口能挡扫描」只能挡住最粗糙的那一层，真正的保护是第 6 节的 TLS 和下面的鉴权。

### 7.2 加固清单【判断，依据见各条】

1. **一次性配对凭据，替代二维码里的永久主 token**
   - 现状（按 `docs/protocol.md`「鉴权」，代码由另一个 agent 核对）：二维码 / `jarvisd hub pair` 打印的是 `hub.json` 的主 token（什么都能做，永不过期）。设备换自己的 `jd_…` token 的机制已有（`docs/protocol.md`「每台设备自己的凭据」）。
   - 改为：`hub pair` 现场生成一个**高熵（≥128 位）临时配对 token**，存哈希，**有效期短（如 10–15 分钟）**、只能换一次设备 token（换后再宽限一段时间才作废，比如 10 分钟：iOS 的 `pair()` 同时在连 WS、注册推送，`PushRegistrar` 发请求时用的是哪个 token **未核实**，宽限太短可能让推送注册失败）、可在 CLI 撤销。二维码里仍是 `url` + `token` 两个字段（**格式不变**），另加新字段（`fp`、`hub_id`、`alt`…）。
   - **对现有 iOS App 的兼容（已读 iOS 代码核实流程）**：iOS 扫码后先用这个 token 调 `GET /api/v1/machines` 验证、连 WS、注册推送，然后 `POST /api/v1/auth/tokens` 换自己的 token（`ios/Jarvis/App/AppModel.swift` 的 `pair()`、`DeviceCredentials.swift` 的 `upgradeCredentials()`）；`ServerConfig(pairURL:)` 只按名字取 `url` 和 `token`，未知参数忽略。所以临时 token 在有效期内必须**能当完整的 Bearer 用**（不能只允许换 token 一个接口），否则旧 iOS App 配不上。iOS 换 token 失败时会在下次启动重试，到时临时 token 可能已过期——这种情况要重新扫码（可接受；或把「还没换过设备 token」的临时 token 有效期放宽到 24 小时）。
   - 保留 `jarvisd hub pair --legacy`（打印主 token）给 CLI 和特殊情况。
   - 手动输入（不扫码）时：短码（如 8 位）+ 严格限流 + 只在 hub 终端上显示，且首次连接要核对指纹前几位（第 6.2 节「坑」）。
2. **鉴权失败限流 / 封禁**
   - 按来源聚合：IPv4 按单个地址；**IPv6 按 /64 聚合**（家宽一户就拿到 /56–/64，1.2 节——按单个 IPv6 地址限流等于没限）；反复触发再升级到 /56。
   - 规则示例：同一来源 1 分钟内 10 次 401 → 封 15 分钟；指数加长。封禁表放内存 + 定期落库（hub 随时会被重新部署，CLAUDE.md 记录过两分钟三次部署）。
   - 设备 token 和临时配对 token 都是高熵随机数，暴力猜不中；限流主要是**降噪、防日志刷屏和资源消耗**。
   - 401 一律同样的响应体和耗时，不区分「token 不存在」和「已撤销」。
3. **拒绝来自公网地址的明文请求**
   - 明文监听只接受来自环回、RFC 1918、ULA（`fc00::/7`）、链路本地、`100.64/10`（Tailscale 等 VPN）和 `hub.json` 白名单的连接；其余来源的明文请求直接 403（说明要用 TLS），并且**不读 Authorization 头、不记录 token**。
   - 判断依据是 TCP 连接本身，**不信任 `X-Forwarded-For`**（除非显式配置了前置代理）。
   - **`100.64/10` 有歧义**：它既是 Tailscale 的地址段，也是运营商 CGNAT 的地址段。hub 所在的家宽如果在运营商 CGNAT 后面，同一 CGNAT 域里的其他用户连进来时，来源地址也可能是 `100.64/10`。所以要同时看**本端地址**（连接是从哪块网卡 / 哪个本机地址进来的）：只有从内网网卡、VPN 网卡（如 `tailscale0`）或环回进来的，才算内网。
   - TLS 监听可接受任何来源。
4. **日志**：
   - 沿用现状（只记方法、路径、状态码、耗时，不记 query、body、token，`docs/server.md`）；
   - WS 的 `?token=` 只为旧客户端保留，新客户端（Android OkHttp 能设请求头）一律用 `Authorization` 头；
   - 鉴权失败日志记来源前缀（/64）和次数，不记提交的凭据；
   - 公网扫描噪音单独计数，不逐条打日志。
5. **端口**：
   - 不用 80 / 443 / 8080 / 8443（1.5 节：被封概率高，也是扫描重点）；
   - TLS 端口默认一个高位端口、可配置；
   - 明文 18660 不要在路由器上放行（只在内网用）。
6. **HTTP 服务器参数**：
   - 给公网监听设 `ReadHeaderTimeout`、`IdleTimeout`、`MaxHeaderBytes`（Go `net/http` 默认不设读头超时：「If zero, the value of ReadTimeout is used. If negative, or if zero and ReadTimeout is zero or negative, there is no timeout」，来源：Go 1.26.4 `src/net/http/server.go`，查于 2026-10-30）；
   - 每来源最大并发连接数；
   - WS 单条消息大小上限。
7. **主机层**：
   - hub 机器的 nftables 对外网 IPv6 默认拒绝入站，只放行 hub 的 TLS 端口（和用户自己需要的 SSH）；
   - 自检所有监听 `::` / `0.0.0.0` 的端口并在 `jarvisd hub doctor` 里列出来；
   - Docker 发布的端口要绑 `127.0.0.1`。
8. **可达性自检**：`jarvisd hub doctor`（或 App 的诊断页）从外部测一次。外部点见 2.6 节：手机走蜂窝、另一台不在家里的 node。结果给出具体的下一步：「光猫 IPv6 防火墙没放行」「hub 本机防火墙没放行」「TUN 代理吞了回包」「运营商封了这个端口」。
9. **新设备配对通知**：有新设备换到 token 时，给已配对的设备发一条通知（「新设备『xxx』已配对，不是你就去 设置 › 设备 撤销」）。

## 8. 后台长连接保活

### 8.1 事实

**NAT / 防火墙超时**（详见 1.7）
- 2011 年 73 家蜂窝运营商里 4 家 ≤ 5 分钟、7 家 5–10 分钟、48 家 > 30 分钟；
- 微信约 2015 年测得：国内移动 / 联通 2G/3G **5 分钟**，电信 3G > 28 分钟；
- 2025 年测的 23 台家用网关：18 台低于 124 分钟，最低 1 分钟；
- **4G/5G 时代国内的权威数据、IPv6 状态防火墙的超时：未查到。**

**无线电能耗**
- 【事实】LTE：RRC 不活动计时器（尾时间）**11.576 秒**，尾阶段基础功率 **1060 mW**；论文结论「LTE is as much as 23 times less power efficient compared with WiFi」，「the long high power tail is found to be a key contributor」。
  口径：2012 年美国某运营商 LTE 网络，实测功耗模型。
  来源：Huang et al., MobiSys 2012, https://web.eecs.umich.edu/~zmao/Papers/RRC4G_mobisys2012.pdf （经搜索摘要转述，查于 2026-10-30）。
- 【事实】5G：SA 5G 尾计时器约 10 秒，之后在 RRC_INACTIVE 停留约 5 秒再进 IDLE；5G 尾阶段功耗高于 4G（Verizon NSA mmWave 尾功率 1092 mW）。
  口径：2021 年美国运营商实测。
  来源：Narayanan et al., SIGCOMM 2021, https://feng-qian.github.io/paper/5g_sigcomm21.pdf （经搜索摘要转述，查于 2026-10-30）。
- 【事实】心跳间隔与耗电：按 1350 mAh 电池和 3G 功耗模型，NAT 超时 < 5 分钟（心跳要更密）时每天保活耗电 > 17% 电池容量，30 分钟时 < 2%。
  口径：2011 年，3G。
  来源：NetPiculet，SIGCOMM 2011（§4.3.1，查于 2026-10-30）。
- 【判断，依据上面三篇】每次心跳都要把无线电拉进高功率态，并停留约 10 秒的尾时间，所以**心跳频率几乎直接决定后台耗电**。

**IM 的做法**
- 【事实】微信（2015）：
  - 前台用固定心跳；进后台或前台关屏时，先用几次最小心跳维持，再进入自适应计算；
  - 按网络分开：数据网络按 subType、Wi-Fi 按 Wi-Fi 名；
  - 新连接先连续 3 次短心跳成功，才开始试更长的间隔；「成功是绝对的，连续失败多次才可能是失败」（5 次延迟测试确定结论）；
  - 选比最后成功值稍小的值作为稳定心跳；稳定期失败后测 5 次，全失败才重算；每周三重新向上探测（运营商可能调长了超时）；
  - 效果「70% 用户都可以达到我们的心跳上限」。
  - 竞品当时的值：WhatsApp 4 分 45 秒；Line Wi-Fi 3 分 20 秒、蜂窝 7 分钟；GCM Wi-Fi 15 分钟、蜂窝 28 分钟。
  来源：https://cloud.tencent.com/developer/article/1030660 （查于 2026-10-30）。
- 【事实】网易云信（2026-09 的博客）：前台 15–30 秒（超时 45–90 秒）、后台 60–120 秒（超时 180–360 秒）、弱网 30–60 秒；心跳间隔加 ±30% 随机抖动（「部分运营商会拦截固定频率的小包」）；重连退避 1→2→4→…→60 秒上限。文中没有给出 NAT 数据的出处。
  来源：https://yunxin.163.com/dev-blog/698 （查于 2026-10-30）。

**Android 平台行为**
- 【事实】Doze：「Suspends network access」「Ignores wake locks」，标准闹钟推迟到维护窗口；`setAndAllowWhileIdle()` / `setExactAndAllowWhileIdle()`「can't fire alarms more than once per nine minutes, per app」。官方页面**没有**说前台服务不受 Doze 的网络限制，只说前台服务让 App 不进 App Standby。
  来源：https://developer.android.com/training/monitoring-device-state/doze-standby （查于 2026-10-30）。
- 【事实】AOSP 实现：`NetworkPolicyManager.isProcStateAllowedWhileIdleOrPowerSaveMode` 对 `procState <= PROCESS_STATE_BOUND_FOREGROUND_SERVICE`（含前台服务）或带 `PROCESS_CAPABILITY_POWER_RESTRICTED_NETWORK` 的进程返回 true，也就是**前台服务进程在 Doze 下仍有网络**。
  来源：aosp-mirror `core/java/android/net/NetworkPolicyManager.java`（GitHub 镜像，已归档，文件最后修改 2024-07；查于 2026-10-30）。国产 ROM 自己的省电策略可能另行限制，【未查到】。
- 【事实】`System.nanoTime()` 基于 `uptimeMillis` 时钟，「This clock stops when the system enters deep sleep」；OkHttp 的任务调度（包括 WebSocket `pingInterval`）用的就是 `System.nanoTime()`。**所以设备深睡时 OkHttp 的客户端 ping 根本不会触发**，前台服务也不持有 wakelock。
  来源：aosp-mirror `core/java/android/os/SystemClock.java` 类注释；OkHttp master `internal/concurrent/TaskRunner.kt`（`override fun nanoTime() = System.nanoTime()`）（查于 2026-10-30）。
- 【事实】Go：`net.ListenConfig.KeepAlive` 为 0 时「keep-alive are enabled」；`KeepAliveConfig` 的默认值 Idle 15 秒、Interval 15 秒、Count 9。也就是说，**用 Go 默认方式监听的服务端，每条空闲连接 15 秒就会发 TCP keepalive 探测**——这也会唤醒手机的无线电。hub 是否覆盖了这个默认值要看 hub 代码（另一个 agent 在核对）。
  来源：Go 1.26.4 `src/net/dial.go`、`src/net/tcpsock.go`（查于 2026-10-30）。

### 8.2 推论【判断】

1. **Android 后台的保活应由 hub 发起，而不是手机。**
   - 手机深睡时，OkHttp 的定时器不走；`AllowWhileIdle` 闹钟最快 9 分钟一次，比国内 5 分钟级的 NAT 超时还长。所以「手机定时发心跳」在 Doze 下保不住连接。
   - hub 发来的包（WS ping 或 TCP keepalive）到达时，手机被唤醒，OkHttp 自动回 pong / 内核回 ACK，**两个方向的 NAT 表项都被刷新**。
   - 前提是前台服务进程在 Doze 下仍有网络（AOSP 是这样，国产 ROM 要实测）。
2. **现在「每 20 秒一次协议层 ping」对后台常驻连接太密。**
   - 加上 Go 默认的 15 秒 TCP keepalive（若 hub 没改），手机大约每 15–20 秒被唤醒一次。
   - 这个间隔比 LTE / 5G 约 10–11.6 秒的尾时间长不了多少，无线电在两次唤醒之间可能来不及回到空闲。
   - 具体多耗多少电**未实测**，按上面的文献口径属于「非常密」的那一端。
3. **按连接协商心跳**：同一个 hub 同时服务前台的 iPhone、后台的 Android 常驻连接、以后的手表，固定 20 秒不合适。

### 8.3 建议的心跳方案【判断】

**协议**（与 hub-changes 3.8 的 `app_ping` 合并设计）：
- 客户端在 `client_hello` 里带 `heartbeat: {mode: "foreground"|"background", server_ping_s: N}`；前后台切换时发 `{"type":"heartbeat","mode":…,"server_ping_s":N}`。
- hub 按连接调整三样：WS 协议层 ping 的间隔；pong 超时；TCP keepalive（后台连接把 Idle 设成和 ping 间隔一致或关掉，避免两套定时器错开、各唤醒一次）。
- hub 回 `{"type":"heartbeat_ack","server_ping_s":实际值}`；不认识 `heartbeat` 的旧 hub 保持 20 秒（客户端照常工作，只是费电）。
- 旧客户端（现有 iOS）不发这些消息，hub 保持现在的 20 秒 / 15 秒，兼容不变。

**前台**（App 在前台、看得见）：
- 保持现状：hub 20 秒协议层 ping；
- Android 用应用层 `{"type":"ping","id"}` 测弱网（iOS 现有规则：5 秒内回 pong 才算好，慢时每 5 秒一次）；
- OkHttp `pingInterval` 只作兜底（如 30 秒）。

**后台常驻**（Android 前台服务、屏幕关闭）：
- **起点 4 分钟**（240 秒：低于微信测到的 5 分钟，也低于 NetPiculet 里 Carrier B 的 255 秒；WhatsApp 当年用 4 分 45 秒）。在自己实测前不往上调。
- **自适应**（照微信的规则）：
  - 按「网络键」（蜂窝：运营商 + 制式；Wi-Fi：网关 / 前缀哈希，见 4.3）分别记录；
  - 新连接先用起点值稳定 3 次，再按 +1 分钟试探，上限先定 9–10 分钟；
  - 失败的判定：连接在「上次 ping 时间 + 间隔 + 宽限」之后被发现已断。连续失败才下调，下调到比最后成功值小一档；
  - 每周重新向上试探一次。
- **下限要能降到 1 分钟以下**：家用网关有 1 分钟的（2025 年实测）。如果 Wi-Fi 上反复在 1–2 分钟内断，提示用户「你的路由器会很快断开空闲连接」。
- **随机抖动**（云信建议 ±30%），避免整点同步和「固定频率小包」被识别。但**抖动只能往下**（例如在 0.8–1.0 倍间隔里取），否则 4 分钟 +30% 就超过了 5 分钟的已知超时。
- **手机端怎么发现「连接已死」**（自己没有定时器可用）：
  - 每次被唤醒（hub 的 ping、网络变化回调、亮屏、维护窗口）时，检查「距上次收到 hub 任何数据」是否超过「协商间隔 + 宽限」，超过就立即重连；
  - 网络变化（`onAvailable` / `onLost` / `onLinkPropertiesChanged`）立刻重连；
  - 亮屏 / 回到前台时立刻发一次应用层 ping。
- **hub 端怎么发现客户端已死**：ping 发出后超时未回 pong（后台连接给更长的超时，如 30–60 秒，因为对方可能正从深睡里醒来），就关闭连接、标记设备离线。离线期间要发的通知在设备重连后补发（Android 常驻模式的「做完了」等由 hub 判定，见 hub-changes 3.9）。
- **TCP keepalive 还是 WS ping**：都能刷新 NAT。keepalive 由手机内核回 ACK、不需要 App 进程做事；WS ping 能顺便确认 App 进程还活着。建议后台以 WS ping 为主，TCP keepalive 设成同一间隔作冗余或关闭——**哪个更省电没有数据，要实测**。

**iOS**（以后 iOS 跟进时）：现有做法是进后台就断开 WS、靠 APNs 唤醒（CLAUDE.md），没有后台常驻连接，这一节只影响前台心跳，可以不动。

### 8.4 必须实测的（按 CLAUDE.md「性能数字必须实测」）

- **国内三家 4G / 5G 的 IPv4 CGNAT 空闲超时、IPv6 防火墙空闲超时**：hub 加一个测试接口，接受连接后保持完全静默 T 分钟（关掉 keepalive），再从 hub 发一个字节，看手机能不能收到；T 用二分（1、2、4、5、8、15、30 分钟），每个运营商 × 制式 × v4/v6 至少 10 次，记 min / p50 / p90 / max / mean。
- **常驻连接的耗电**：同一台手机、同一张卡，后台心跳分别为 20 秒、2 分钟、4 分钟、自适应，各跑 ≥ 10 段、每段 ≥ 2 小时，屏幕关闭、静置；用 `dumpsys batterystats`（蜂窝无线电活跃时间、唤醒次数、App 耗电估算）和电量下降，记 5 个分位数。
- **国产 ROM**（小米 / 华为 / OPPO / vivo）：前台服务在 Doze 下能不能收到 hub 的 ping，锁屏多久后被冻结。

## 9. 推荐方案（汇总）【判断】

> 范围（2026-10-30 协调方补充）：这一轮只改 **Android 和 hub**，iOS 只读不改；**hub 的任何改动都必须让现有 iOS App 不改代码照常工作**——内网明文连接、旧格式二维码、单地址。下文 iOS 的做法一律标成「以后 iOS 跟进时」。

### 9.1 兼容约束怎么满足

| 现有 iOS 依赖的东西 | hub 新版本怎么保持 | 依据 |
| --- | --- | --- |
| 明文 `http://<内网>:18660`，`Authorization: Bearer` / WS `?token=` | 明文监听原样保留；只对**公网来源**拒绝明文（内网、ULA、链路本地、100.64/10 VPN、白名单照常） | 7.2 第 3 条 |
| 二维码 `jarvis://pair?url=…&token=…` | 格式不变。`url` 仍是内网明文地址（单地址）；新信息放在**附加参数**里（`hub`、`fp`、`tls`、`alt`…）。iOS 的 `ServerConfig(pairURL:)` 只按名字取 `url` 和 `token`，会忽略这些参数 | `ios/Shared/APIClient.swift` 第 7–16 行 |
| 扫码后 `GET /machines` → 连 WS → 注册推送 → `POST /auth/tokens` 换设备 token | 二维码里的临时 token 在有效期内当完整 Bearer 用；换过设备 token 后宽限一会儿再作废；`--legacy` 仍可打印主 token | 7.2 第 1 条；`AppModel.pair()`、`upgradeCredentials()` |
| 固定 20 秒协议层 ping | 不发 `heartbeat` 协商消息的连接保持 20 秒 / 15 秒不变 | 8.3 |
| `/api/v1/health` 免鉴权 | 不变；只加不敏感的字段（`hub_id`、TLS 端口、`fp`），**候选地址不放这里**（免鉴权会泄露家里地址），放在鉴权后的接口和 WS `hello` 里 | 7.1 |

- 唯一会改变现有行为的是「公网来源的明文请求被拒」。文档一直要求 hub 只放在内网或 VPN 里，所以正常用法不受影响；但现有 iOS App 开着 `NSAllowsArbitraryLoads`，如果有人已经在经公网明文用 hub，升级后会连不上。`hub.json` 留一个 `allow_public_plaintext`（默认关）恢复旧行为，并在 `hub doctor` 和日志里提示。

### 9.2 hub（本轮）

1. **地址发现**：
   - netlink 读带标志的地址，规则见 2.3：排除临时、废弃、ULA、链路本地、虚拟网卡；
   - 订阅地址变化 + 每 1–5 分钟全量重扫；
   - 公网 IPv4 用 UPnP / NAT-PMP / PCP 查 WAN 口，再加 STUN / HTTPS 回显，判断有没有 CGNAT。
   - 产出候选列表：`lan`（内网 IPv4 / ULA）、`ipv6`（全局、稳定）、`ipv4`（公网，只有确认可达才发）、`name`（DDNS 名字 / `public_url`），每项带端口、协议（`https` / `http`）、`seen_at`。
2. **下发候选**：
   - 鉴权后的 `GET /api/v1/endpoints`；
   - WS `hello` 的新字段；
   - 变化时推 `{"type":"endpoints"}`；
   - `features` 加 `endpoints`、`tls`、`heartbeat`、`app_ping`。
3. **TLS 监听**：
   - 单独端口（默认高位端口，可配，不用 80/443/8080/8443）；
   - hub 身份密钥（P-256，0600，不进数据库）签自签证书，同一把密钥重签；
   - 配了域名 + DNS 令牌时，用 ACME DNS-01（lego）以同一把密钥换公共 CA 证书（通配名）；
   - 不做 IP 证书。
4. **配对**：`jarvisd hub pair` 默认发临时 token（高熵、短时、换一次），二维码追加 `hub`、`fp`、`tls`、`alt`（只放一两个最可能的外部候选，其余配对后拉取，避免二维码过密）。
5. **DDNS（可选）**：
   - Cloudflare / 阿里云 / DNSPod / 通用回调 URL；
   - 只发布 2.3 规则选出的稳定地址；
   - A 记录只在确认外部可达时发布；
   - 令牌和 ACME 共用，权限收窄到一个 zone。
6. **加固**（7.2）：
   - 公网来源拒绝明文；
   - 鉴权失败按 /64 限流；
   - HTTP 超时；
   - 新设备配对通知；
   - `jarvisd hub doctor` 列出监听端口、防火墙、TUN 代理风险，并请一个外部点（不在家的 node / 手机）测可达性。
7. **心跳协商**（8.3）：
   - `client_hello.heartbeat` 按连接调 WS ping 间隔、pong 超时、TCP keepalive；
   - 应用层 `ping` / `pong`（hub-changes 3.8）；
   - 旧客户端保持 20 秒。

### 9.3 Android（本轮）

1. **配对**：解析新参数，把 `hub_id`、`fp`（以及 `fp_next`）、候选列表存进加密存储；旧 hub（没有 `tls` feature）退回内网明文，并在设置里提示「hub 版本太旧，不能公网直连」。
2. **连接管理**：
   - 按 4.3 错峰并发（250 ms），身份校验（SPKI 钉扎 + `hub_id`）通过才算赢；
   - 按网络键缓存赢家；
   - 网络变化、断线、收到新候选时重新探测；
   - 定期「升级检查」。
3. **TLS**：hub 专用 `OkHttpClient`，自定义 TrustManager 校验 SPKI（不匹配就抛 `CertificateException`）；REST、WS、图片、上传下载都走它；WebView 不直接加载 hub 资源。
4. **本地网络权限**（target 37）：在家连内网或同一 /64 的全局 IPv6 都要 `ACCESS_LOCAL_NETWORK`（5.3）。被拒时按 5.3 的兜底顺序走，并把「被权限拦了」和「hub 不可达」分开显示。
5. **后台常驻**：
   - 后台心跳由 hub 发起，协商间隔起点 4 分钟、按网络自适应，加抖动；
   - 每次被唤醒时检查「距上次收到 hub 数据」是否超时，超时就立即重连；
   - 网络变化立刻重连；
   - 前台改回 20 秒 + 应用层 ping。
6. **诊断页**：显示
   - 当前路径（内网 / IPv6 / IPv4 / 域名）和 RTT；
   - 手机有没有 IPv6、是否经 VPN、是否在强制门户后面；
   - 最近一次失败分类：DNS / 拒绝 / 超时 / 证书不符 / 本地网络权限 / hub 太旧。

### 9.4 iOS（以后 iOS 跟进时）

- 解析新二维码参数；`URLSessionDelegate` 做 SPKI 校验，`URLSessionWebSocketTask` 走同一个 session；
- `NWPathMonitor` + 候选错峰探测；IPv4 字面地址在 NAT64 网络上由高层 API 合成（4.2）；
- 后台继续用 APNs，不做常驻连接。地址变化时 hub 可以用 APNs 后台推送把新候选送到 iPhone（3.3）。

### 9.5 风险

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| 光猫 IPv6 防火墙只有开 / 关，或根本关不了 | 公网直连做不成，或者整个家庭网络暴露 | 引导改桥接 + 自己的路由器按后缀放行；hub 主机防火墙兜底；doctor 自检 |
| 前缀一天变几次、没有域名 | 手机离线期间丢失 hub | 推荐至少一个 DDNS 名字；签名地址公告作为备选；内网地址兜底 |
| 运营商封端口 / 按区域策略不同 | 某些用户的端口不可达 | 端口可配；doctor 给出明确诊断 |
| 4G/5G NAT 超时比 4 分钟还短 | 后台连接频繁断、重连耗电 | 自适应下调；实测数据 |
| 国产 ROM 冻结前台服务或断网 | 收不到通知 | 引导电池白名单 / 自启动（PR #84 已有）；实测；有 Google 服务时切 FCM |
| 自签钉扎的密钥丢失 | 所有设备要重新配对 | 文档说明；预告下一把密钥 |
| hub 机器 TUN 代理吞掉入站回包 | 公网入站静默失败 | doctor 检测；部署文档说明排除规则 |

## 10. 没查清 / 必须实测的

| # | 问题 | 现状 | 怎么补 |
| --- | --- | --- | --- |
| 1 | 国内三家 4G / 5G 的 IPv4 CGNAT 空闲 TCP 超时 | 只有约 2015 年 2G/3G 的数据（5 分钟 / >28 分钟） | 8.4 的二分测量，每组 ≥ 10 次 |
| 2 | 手机侧 IPv6 状态防火墙 / 5GC 的空闲超时 | 未查到任何数据 | 同上，分 v4 / v6 |
| 3 | 前台服务常驻连接的耗电（不同心跳间隔） | 未实测 | 8.4，≥ 10 段 × ≥ 2 小时 |
| 4 | 国产 ROM 在 Doze / 锁屏后对前台服务网络的处理 | AOSP 允许；ROM 未知 | 真机矩阵 |
| 5 | Android 17：同一 /64 的全局 IPv6 是否被本地网络权限拦 | 源码（17.0.0_r1）显示会；文档写「Directly-connected routes」 | 真机 + `dumpsys connectivity trafficcontroller` |
| 6 | Android 17：不在同一网段的私网 IPv4（192.168.10.x）是否被拦 | 源码只拦 on-link 前缀，文档列整个 192.168/16，不一致 | 真机 |
| 7 | 经 VPN（地址掩码较大的 VPN）时本地网络权限的行为 | 推断取决于 VPN 接口的前缀长度 | 真机（Tailscale、WireGuard 各一） |
| 8 | 18660 及其他高位端口有没有被运营商封 | 未查到报告 | doctor 外部探测 + 用户反馈 |
| 9 | 手机流量跨运营商访问家宽 IPv6 的可达率 | 只有零星网友报告 | 三家卡 × 三家家宽的矩阵测试 |
| 10 | IPv6 路径 MTU 问题和合适的 MSS | 只有网友建议 1420 | 用大证书链握手测；收集握手超时分类 |
| 11 | 国内解析器是否遵守 DDNS 的 TTL | 未查到 | 换地址后从多地解析计时 |
| 12 | 免费 DDNS（dynv6 / deSEC / DuckDNS）在国内的可用性和解析速度 | 未查 | 实测 |
| 13 | 光猫是否支持 PCP / UPnP IGD2 的 IPv6 放行 | 未查到 | 有条件时实测，不作为主路径 |
| 14 | lego 是否有 `tencentcloud` 提供方、当前模块路径 | 搜索结果未确认 | 引入前看仓库 |
| 15 | `NWConnection` 对 IPv4 字面地址是否合成 NAT64 地址（以后 iOS） | Apple 文档没写 | iOS 跟进时实测 |
| 16 | Apple 后台推送限频原文 | 页面是 JS 渲染，未抓到原文 | 有需要时人工核对 |
| 17 | hub 当前是否覆盖了 Go 默认的 15 秒 TCP keepalive、20 秒 ping 的具体实现 | 由另一个 agent 核对 hub 代码 | 看 hub 代码 |

## 11. 来源清单（均查于 2026-10-30）

**官方 / 标准**
- 新华网，《中国IPv6发展报告（2026）》报道：https://www.news.cn/info/20260729/e3793c4b1c3a428bb365a1f5a2b64513/c.html
- 中央网信办，《实施方案（2026—2030）》答记者问：https://www.cac.gov.cn/2026-07/21/c_1786380790538547.htm
- 中央网信办，电信 5G SA IPv6 单栈案例：https://www.cac.gov.cn/2021-11/25/c_1639439849620158.htm
- RFC 5382：https://www.rfc-editor.org/rfc/rfc5382.html
- RFC 6092：https://www.rfc-editor.org/rfc/rfc6092.html
- RFC 6887（PCP）：https://www.rfc-editor.org/rfc/rfc6887.html
- RFC 7707（IPv6 侦察）：https://www.rfc-editor.org/rfc/rfc7707.html
- RFC 8305（Happy Eyeballs v2）：https://www.rfc-editor.org/rfc/rfc8305.html
- RFC 8978（闪断重编号）：https://www.rfc-editor.org/rfc/rfc8978.html
- Happy Eyeballs v3 草案：https://datatracker.ietf.org/doc/draft-ietf-happy-happyeyeballs-v3/
- Linux `if_addr.h`、`addrconf.c`、`ip-sysctl.rst`：https://github.com/torvalds/linux
- Linux conntrack sysctl：https://docs.kernel.org/networking/nf_conntrack-sysctl.html
- ip-route(8)：https://man7.org/linux/man-pages/man8/ip-route.8.html
- tcp(7)：https://man7.org/linux/man-pages/man7/tcp.7.html
- systemd.network(5)：https://www.freedesktop.org/software/systemd/man/latest/systemd.network.html
- OpenWrt IPv6 防火墙示例：https://openwrt.org/docs/guide-user/firewall/fw3_configurations/fw3_ipv6_examples
- Cloudflare DNS API：https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/edit/
- 阿里云云解析 TTL：https://help.aliyun.com/zh/dns/pubz-add-parsing-record
- DNSPod TTL：https://docs.dnspod.cn/dns/help-ttl/ ；腾讯云 API：https://cloud.tencent.com/document/api/1427/56158
- dynv6 API：https://dynv6.com/docs/apis ；deSEC：https://desec.readthedocs.io/en/latest/dyndns/update-api.html
- Let's Encrypt：
  - IP / 6 天证书 GA：https://letsencrypt.org/2026/01/15/6day-and-ip-general-availability
  - 第一张 IP 证书：https://letsencrypt.org/2025/07/01/issuing-our-first-ip-address-certificate
  - profiles：https://letsencrypt.org/docs/profiles/
  - 验证方式：https://letsencrypt.org/docs/challenge-types/
  - 64 天证书：https://letsencrypt.org/2026/10/07/64-day-certs
- lego：https://pkg.go.dev/github.com/go-acme/lego/v4/certificate ；https://go-acme.github.io/lego/dns/
- Android：
  - Local Network Definition：https://developer.android.com/privacy-and-security/local-network-definition
  - Local network permission：https://developer.android.com/privacy-and-security/local-network-permission
  - Doze：https://developer.android.com/training/monitoring-device-state/doze-standby
  - LinkProperties：https://developer.android.com/reference/android/net/LinkProperties
- AOSP Connectivity（android-17.0.0_r1）：
  - https://android.googlesource.com/platform/packages/modules/Connectivity/+/refs/tags/android-17.0.0_r1/service/src/com/android/server/ConnectivityService.java
  - https://android.googlesource.com/platform/packages/modules/Connectivity/+/refs/tags/android-17.0.0_r1/bpf/progs/netd.c
  - https://android.googlesource.com/platform/packages/modules/Connectivity/+/refs/tags/android-17.0.0_r1/staticlibs/framework/com/android/net/module/util/NetworkStackConstants.java
- AOSP frameworks/base（GitHub 镜像，已归档）：`NetworkPolicyManager.java`、`SystemClock.java`：https://github.com/aosp-mirror/platform_frameworks_base
- OkHttp（master）：`OkHttpClient.kt`、`CertificatePinner.kt`、`OkHostnameVerifier.kt`、`RealWebSocket.kt`、`TaskRunner.kt`：https://github.com/square/okhttp ；CHANGELOG：https://github.com/square/okhttp/blob/master/CHANGELOG.md
- vishvananda/netlink v1.3.1：https://pkg.go.dev/github.com/vishvananda/netlink
- Go 1.26.4 标准库源码（本机 GOROOT）：`net/netip`、`net/dial.go`、`net/tcpsock.go`、`net/http/server.go`
- golang.org/x/sys v0.47.0（本机模块缓存）：`unix/zerrors_linux.go`、`unix/ztypes_linux.go`
- Apple：
  - NWPath：https://developer.apple.com/tutorials/data/documentation/network/nwpath.json
  - 手动服务器信任：https://developer.apple.com/documentation/foundation/performing-manual-server-trust-authentication
  - DNS64/NAT64：https://developer.apple.com/library/archive/documentation/NetworkingInternetWeb/Conceptual/NetworkingOverview/UnderstandingandPreparingfortheIPv6Transition/UnderstandingandPreparingfortheIPv6Transition.html
- Docker 防火墙：https://docs.docker.com/engine/network/packet-filtering-firewalls/

**论文 / 实测**
- NetPiculet（SIGCOMM 2011）：https://conferences.sigcomm.org/sigcomm/2011/papers/sigcomm/p374.pdf
- Silent TCP Connection Closure（CoNEXT 2013）：https://conferences.sigcomm.org/co-next/2013/program/p211.pdf
- Home Gateway Characteristics（IMC 2010）：https://conferences.sigcomm.org/imc/2010/papers/p260.pdf
- On The Evolution of Home Gateways（ITC 2025）：https://thomasez.folk.ntnu.no/itc36/itc2025-final4.pdf
- LTE 功耗（MobiSys 2012）：https://web.eecs.umich.edu/~zmao/Papers/RRC4G_mobisys2012.pdf
- 5G 功耗（SIGCOMM 2021）：https://feng-qian.github.io/paper/5g_sigcomm21.pdf
- CT 与扫描（IMC 2018）：https://dl.acm.org/doi/10.1145/3278532.3278562 ；EuroS&P 2023：https://sites.cs.ucsb.edu/~chris/research/doc/eurosnp23_certvuln.pdf
- 微信智能心跳（2015）：https://cloud.tencent.com/developer/article/1030660
- 网易云信长连接实践（2026-09）：https://yunxin.163.com/dev-blog/698

**网友报告（非官方，按地区 / 时间差异很大）**
- V2EX：
  - PD 前缀长度：https://v2ex.com/t/930849
  - 上海电信 PD 轮换：https://www.v2ex.com/t/1250020
  - 端口封锁：https://www.v2ex.com/t/1026607 、https://www.v2ex.com/t/1121687 、https://v2ex.com/t/904810 、https://www.v2ex.com/t/1130831
  - 手机访问家宽 IPv6：https://hk.v2ex.com/t/1014000
  - 手机 IPv6 入站：https://www.v2ex.com/t/843359
  - 光猫 PD：https://www.v2ex.com/t/1017699
  - 前缀变化：https://www.v2ex.com/t/775397 、https://v2ex.com/t/706397
  - 公网 IPv4：https://www.v2ex.com/t/808536
- 其他论坛与博客：
  - 光猫防火墙：https://www.chinadsl.net/thread-169397-1-1.html 、https://blog.csdn.net/u013084266/article/details/128358328 、https://www.cnblogs.com/ie123610/p/19026984
  - NodeSeek：https://www.nodeseek.com/post-118403-1 、https://www.nodeseek.com/post-76979-1
  - 通信人家园：https://www.txrjy.com/thread-1240845-1-1.html
  - linux.do：https://linux.do/t/topic/1016716
  - 光猫 PD 博客：https://zhangshuqiao.org/2026-07/XG-140G-TF%E5%85%89%E7%8C%AB%E5%90%AF%E7%94%A8IPv6%20PD%E5%89%8D%E7%BC%80%E5%88%86%E5%8F%91/
  - 公网 IPv4 申请：https://juejin.cn/post/7647395004020932654 、https://mao.fan/article/432
- sing-box issues：#2167、#4605、#4676（https://github.com/SagerNet/sing-box/issues/ ）；#3079（https://github.com/LIghtJUNction/sing-box/issues/3079）
- ddns-go：https://github.com/jeessy2/ddns-go 、https://github.com/jeessy2/ddns-go/discussions/983
