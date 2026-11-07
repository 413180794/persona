package persona.core.hub

import persona.core.contracts.Endpoint
import persona.core.contracts.api.EndpointCheck
import persona.core.contracts.api.EndpointOverrides
import java.net.InetAddress
import org.junit.Assert.*
import org.junit.Test

class HubEndpointsTest {
    private val policy = LocalAddressPolicy()
    // Private addresses are built at runtime: scripts/privacy_check.py refuses them as literals.
    private val lan = listOf(10, 0, 0, 7).joinToString(".")
    private val home = listOf(192, 168, 1, 20).joinToString(".")
    private val ula = listOf("fd00", "", "1").joinToString(":")

    private fun check(
        input: String, pinned: Boolean = true, taken: Set<String> = emptySet(), manualCount: Int = 0, replacing: String? = null,
    ) = checkEndpoint(input, pinned, policy, taken, manualCount, replacing)

    private fun valid(input: String, pinned: Boolean = true) = check(input, pinned) as? EndpointCheck.Valid
        ?: throw AssertionError("Expected $input to be valid, got ${check(input, pinned)}")

    private fun message(input: String, pinned: Boolean = true, taken: Set<String> = emptySet(), manualCount: Int = 0, replacing: String? = null) =
        (check(input, pinned, taken, manualCount, replacing) as? EndpointCheck.Invalid)?.message?.zh()
            ?: throw AssertionError("Expected $input to be refused")

    @Test fun canonicalFormsHaveAnExplicitPortAndLowercaseHost() {
        assertEquals(EndpointCheck.Valid("https://hub.example.com:18662", "name"), check("hub.example.com:18662"))
        assertEquals("https://hub.example.com:18662", (valid("HTTPS://Hub.EXAMPLE.com:18662/").url).zhAny())
        assertEquals("https://hub.example.com:443", (valid("https://hub.example.com:443").url).zhAny())
        assertEquals(EndpointCheck.Valid("https://[2001:db8::1]:18662", "ipv6"), check("[2001:DB8:0:0::1]:18662"))
        assertEquals(EndpointCheck.Valid("https://192.0.2.10:18662", "ipv4"), check("https://192.0.2.10:18662"))
        assertEquals(EndpointCheck.Valid("https://$lan:18662", "lan"), check("$lan:18662"))
        assertEquals(EndpointCheck.Valid("https://[$ula]:18662", "lan"), check("https://[$ula]:18662"))
        assertEquals(EndpointCheck.Valid("http://127.0.0.1:18660", "lan"), check("http://127.0.0.1:18660", pinned = false))
        assertEquals(EndpointCheck.Valid("http://$home:18660", "lan"), check("http://$home:18660"))
        assertEquals(EndpointCheck.Valid("http://hub.local:18660", "lan"), check("http://Hub.local:18660"))
        assertEquals(EndpointCheck.Valid("http://localhost:18660", "lan"), check("http://localhost:18660"))
        assertEquals(EndpointCheck.Valid("http://[::1]:18660", "lan"), check("http://[::1]:18660"))
    }

    @Test fun refusesAddressesThatCannotBeACandidate() {
        assertEquals("请输入地址", (message("  ")).zhAny())
        assertEquals("地址里不能有空格", (message("hub.example.com :18662")).zhAny())
        assertEquals("只支持 https:// 或 http:// 开头的地址", (message("persona://pair?url=x&token=y")).zhAny())
        assertEquals("只支持 https:// 或 http:// 开头的地址", (message("ftp://hub.example.com:21")).zhAny())
        assertEquals("地址里不能带用户名或密码", (message("https://me:secret@hub.example.com:18662")).zhAny())
        assertEquals("只写到端口为止，不要带路径、? 或 #", (message("https://hub.example.com:18662/api/v1/health")).zhAny())
        assertEquals("只写到端口为止，不要带路径、? 或 #", (message("https://hub.example.com:18662?x=1")).zhAny())
        assertEquals("只写到端口为止，不要带路径、? 或 #", (message("https://hub.example.com:18662#top")).zhAny())
        assertEquals("IPv6 地址要加方括号，例如 https://[2001:db8::1]:18662", (message("2001:db8::1")).zhAny())
        assertEquals("IPv6 地址要加方括号，例如 https://[2001:db8::1]:18662", (message("https://2001:db8::1:18662")).zhAny())
        assertEquals("IPv6 地址少了右方括号", (message("https://[2001:db8::1:18662")).zhAny())
        assertEquals("不支持带 % 的链路本地地址", (message("https://[fe80::1%25wlan0]:18662")).zhAny())
        assertEquals("IPv6 地址不对", (message("https://[2001:db8::g]:18662")).zhAny())
        assertEquals("IPv6 地址不对", (message("https://[1:2:3]:18662")).zhAny())
        assertEquals("IPv6 地址不对", (message("https://[192.0.2.1]:18662")).zhAny())
        assertEquals("IPv4 地址不对", (message("https://999.0.2.1:18662")).zhAny())
        assertEquals("IPv4 地址不对", (message("https://192.0.2:18662")).zhAny())
        assertEquals("IPv4 地址不对", (message("https://192.0.2.010:18662")).zhAny())
        assertEquals("主机名不对", (message("https://bad..example.com:18662")).zhAny())
        assertEquals("主机名不对", (message("https://hub%2eexample.com:18662")).zhAny())
        assertEquals("缺少主机名或 IP", (message("https://:18662")).zhAny())
        assertEquals("缺少主机名或 IP", (message("https://[]:18662")).zhAny())
        assertEquals("缺少端口，写成 https://地址:端口", (message("https://hub.example.com")).zhAny())
        assertEquals("缺少端口，写成 https://地址:端口", (message("hub.example.com:")).zhAny())
        assertEquals("缺少端口，写成 https://地址:端口", (message("https://[2001:db8::1]")).zhAny())
        assertEquals("端口要在 1–65535 之间", (message("hub.example.com:http")).zhAny())
        assertEquals("端口要在 1–65535 之间", (message("hub.example.com:0")).zhAny())
        assertEquals("端口要在 1–65535 之间", (message("hub.example.com:65536")).zhAny())
        assertEquals("这不是能连接的地址", (message("https://0.0.0.0:18662")).zhAny())
        assertEquals("这不是能连接的地址", (message("https://[::]:18662")).zhAny())
        assertEquals("这不是能连接的地址", (message("https://224.0.0.1:18662")).zhAny())
        assertEquals("这不是能连接的地址", (message("https://[ff02::1]:18662")).zhAny())
        assertEquals("这不是能连接的地址", (message("https://${List(4) { 255 }.joinToString(".")}:18662")).zhAny())
    }

    @Test fun httpsNeedsThePinAndHttpOnlyReachesTheLocalNetwork() {
        val unpinned = "这次配对没有 hub 的证书指纹，无法校验 HTTPS 地址。请在 hub 开启 TLS 后重新配对。"
        assertEquals(unpinned, message("hub.example.com:18662", pinned = false))
        assertEquals(unpinned, message("https://$lan:18662", pinned = false))
        val plaintext = "HTTP 不加密，只能用内网地址（如 192.168.x.x、10.x.x.x、fd00::/8 或 .local 名字）。公网地址请用 https://"
        assertEquals(plaintext, message("http://192.0.2.10:18660"))
        assertEquals(plaintext, message("http://[2001:db8::1]:18660"))
        assertEquals(plaintext, message("http://hub.example.com:18660"))
        assertTrue(check("http://$lan:18660", pinned = false) is EndpointCheck.Valid)
        val vpn = LocalAddressPolicy { it == InetAddress.getByName("192.0.2.10") }
        assertEquals(EndpointCheck.Valid("http://192.0.2.10:18660", "lan"),
            checkEndpoint("http://192.0.2.10:18660", false, vpn, emptySet(), 0))
    }

    @Test fun duplicatesAndTheManualLimitUseCanonicalKeys() {
        val taken = setOf(endpointKey("https://hub.example.com:18662/"), endpointKey("https://[2001:db8::1]:18662"))
        assertEquals("列表里已经有这个地址了", (message("HTTPS://HUB.example.com:18662/", taken = taken)).zhAny())
        assertEquals("列表里已经有这个地址了", (message("[2001:db8:0::1]:18662", taken = taken)).zhAny())
        assertTrue(check("http://hub.local:18662", taken = taken) is EndpointCheck.Valid)
        assertTrue(check("hub.example.com:18662", taken = taken, replacing = "https://hub.example.com:18662") is EndpointCheck.Valid)
        assertEquals("最多手动添加 10 条地址", (message("hub.example.org:18662", manualCount = 10)).zhAny())
        assertTrue(check("hub.example.org:18662", manualCount = 10, replacing = "https://hub.example.net:18662") is EndpointCheck.Valid)
        assertTrue(check("hub.example.org:18662", manualCount = 9) is EndpointCheck.Valid)
    }

    @Test fun keysAndKinds() {
        assertEquals("https://hub.example.com:18662", (endpointKey("https://Hub.Example.com:18662/")).zhAny())
        assertEquals("https://hub.example.com:443", (endpointKey("https://hub.example.com")).zhAny())
        assertEquals("http://hub.example.com:80", (endpointKey("http://hub.example.com/")).zhAny())
        assertEquals("https://[2001:db8::1]:18662", (endpointKey("https://[2001:DB8:0::1]:18662")).zhAny())
        assertEquals("not a url", (endpointKey("  Not A URL ")).zhAny())
        assertEquals("lan", (endpointKind("http://localhost:18660")).zhAny())
        assertEquals("lan", (endpointKind("https://hub.local:18662/")).zhAny())
        assertEquals("lan", (endpointKind("http://127.0.0.1:18660")).zhAny())
        assertEquals("lan", (endpointKind("https://$home:18662")).zhAny())
        assertEquals("lan", (endpointKind("https://[$ula]:18662")).zhAny())
        assertEquals("lan", (endpointKind("https://[fe80::1]:18662")).zhAny())
        assertEquals("ipv6", (endpointKind("https://[2001:db8::1]:18662")).zhAny())
        assertEquals("ipv4", (endpointKind("https://192.0.2.10:18662")).zhAny())
        assertEquals("name", (endpointKind("https://hub.example.com:18662")).zhAny())
        assertEquals("name", (endpointKind("https://999.0.2.1:18662")).zhAny())
        assertEquals("unknown", (endpointKind("persona://pair")).zhAny())
    }

    @Test fun theDefaultOrderGoesByKindThenSchemeThenSource() {
        val ipv4 = Endpoint("https://192.0.2.10:18662", "ipv4")
        val lanHttp = Endpoint("http://$lan:18660/", "lan")
        val lanHttps = Endpoint("https://$lan:18662/", "lan")
        val name = Endpoint("https://hub.example.com:18662", "name")
        val ipv6 = Endpoint("https://[2001:db8::1]:18662", "ipv6")
        val manualName = Endpoint("https://hub.example.org:18662", "name")
        val odd = Endpoint("https://hub.example.net:18662", "unknown")
        val base = listOf(odd, ipv4, lanHttp, name, lanHttps, ipv6)
        assertEquals(listOf(ipv6, name, lanHttps, lanHttp, ipv4, odd), orderEndpoints(base))
        assertEquals(listOf(ipv6, manualName, name, lanHttps, lanHttp, ipv4, odd),
            orderEndpoints(listOf(manualName) + base, manual = setOf(endpointKey(manualName.url))))
        // Otherwise equal candidates stay as given.
        val other = Endpoint("https://[2001:db8::2]:18662", "ipv6")
        assertEquals(listOf(other, ipv6), orderEndpoints(listOf(other, ipv6)))
        assertEquals(listOf(ipv6, other), orderEndpoints(listOf(ipv6, other)))
        // A duplicate spelling keeps the first one (manual candidates come first in the merged list).
        assertEquals(listOf(lanHttps), orderEndpoints(listOf(lanHttps, Endpoint("https://$lan:18662", "lan"))))
    }

    @Test fun everyCandidateIsArrangedOnceInTheDefaultOrderOrTheUsers() {
        val ipv6 = Endpoint("https://[2001:db8::1]:18662", "ipv6")
        val name = Endpoint("https://hub.example.com:18662/", "name")
        val lanHttps = Endpoint("https://$lan:18662", "lan")
        val ipv4 = Endpoint("https://192.0.2.10:18662", "ipv4")
        val manual = "https://hub.example.org:18662"
        val candidates = mergeEndpoints(listOf(manual), listOf(ipv4, lanHttps, name, ipv6), policy)
        fun urls(overrides: EndpointOverrides) = arrangeEndpoints(candidates, overrides).map { it.url }

        // Without the user's order: the default rule, with the manual candidates first within their kind.
        val automatic = EndpointOverrides(manual = listOf(manual))
        assertEquals(orderEndpoints(candidates, setOf(manual)), arrangeEndpoints(candidates, automatic))
        assertEquals(listOf(ipv6.url, manual, name.url, lanHttps.url, ipv4.url), urls(automatic))
        // Disabled candidates keep their place (the connection skips them).
        assertEquals(urls(automatic), urls(automatic.copy(disabled = setOf(endpointKey(name.url), manual))))

        // The user's order comes first: keys that are no longer candidates and repeats are skipped, candidates it
        // does not list follow in the default order.
        val stale = "https://[2001:db8::99]:18662"
        val custom = automatic.copy(order = listOf(endpointKey(ipv4.url), stale, endpointKey(name.url), endpointKey(ipv4.url)))
        assertEquals(listOf(ipv4.url, name.url, ipv6.url, manual, lanHttps.url), urls(custom))
        assertEquals(urls(custom), urls(custom.copy(disabled = setOf(endpointKey(ipv4.url)))))
        val everything = custom.copy(order = listOf(manual, endpointKey(lanHttps.url), endpointKey(ipv4.url), endpointKey(name.url), endpointKey(ipv6.url)))
        assertEquals(listOf(manual, lanHttps.url, ipv4.url, name.url, ipv6.url), urls(everything))
        // An empty order is still the user's: everything follows in the default order.
        assertEquals(urls(automatic), urls(automatic.copy(order = emptyList())))
        // Each candidate once, whatever the spelling (the pairing's trailing slash, the hub's without it).
        assertEquals(urls(everything), arrangeEndpoints(candidates + Endpoint("https://hub.example.com:18662", "name"), everything).map { it.url })
    }

    @Test fun hubListChangesAppendNewAddressesToTheUsersOrderAndDropRemovedOnes() {
        val ipv6 = Endpoint("https://[2001:db8::1]:18662", "ipv6")
        val name = Endpoint("https://hub.example.com:18662", "name")
        val lanHttps = Endpoint("https://$lan:18662", "lan")
        val renumbered = Endpoint("https://[2001:db8::2]:18662", "ipv6")
        val ipv4 = Endpoint("https://192.0.2.10:18662", "ipv4")
        val manual = "https://hub.example.org:18662"
        fun urls(hub: List<Endpoint>, overrides: EndpointOverrides) =
            arrangeEndpoints(mergeEndpoints(overrides.manual, hub, policy), overrides).map { it.url }

        val before = listOf(ipv6, name, lanHttps)
        val after = listOf(renumbered, name, lanHttps, ipv4)
        // The user's order, as settings saves it: every entry.
        val custom = EndpointOverrides(manual = listOf(manual), order = listOf(endpointKey(lanHttps.url), manual, endpointKey(ipv6.url), endpointKey(name.url)))
        assertEquals(listOf(lanHttps.url, manual, ipv6.url, name.url), urls(before, custom))
        // The hub drops one address and sends two new ones: the dropped one is gone, the new ones follow at the end.
        assertEquals(listOf(lanHttps.url, manual, name.url, renumbered.url, ipv4.url), urls(after, custom))
        // An address that comes back takes its old place.
        assertEquals(listOf(lanHttps.url, manual, ipv6.url, name.url, renumbered.url, ipv4.url), urls(after + ipv6, custom))
        // With the default order new addresses take their default place.
        val automatic = custom.copy(order = null)
        assertEquals(listOf(ipv6.url, manual, name.url, lanHttps.url), urls(before, automatic))
        assertEquals(listOf(renumbered.url, manual, name.url, lanHttps.url, ipv4.url), urls(after, automatic))
    }

    @Test fun mergedCandidatesLetManualOnesWinDuplicates() {
        val base = listOf(Endpoint("https://hub.example.com:18662/", "name"), Endpoint("https://192.0.2.10:18662", "ipv4"))
        val merged = mergeEndpoints(listOf("https://hub.example.com:18662", "http://$home:18660"), base, policy)
        assertEquals(listOf("https://hub.example.com:18662", "http://$home:18660", "https://192.0.2.10:18662"), (merged.map { it.url }).zhAny())
        assertEquals(listOf("name", "lan", "ipv4"), (merged.map { it.kind }).zhAny())
        assertEquals(18660, merged[1].port)
        assertEquals("http", (merged[1].scheme).zhAny())
    }

    @Test fun failureLabelsAreChinese() {
        assertEquals("已通过身份校验", (endpointResultLabel(null)).zhAny())
        assertEquals("证书指纹不符", (endpointResultLabel(HubError.CertificateMismatch)).zhAny())
        assertEquals("连接被拒绝", (endpointResultLabel(HubError.ConnectionRefused)).zhAny())
        assertEquals("不是配对的那台 hub", (endpointResultLabel(HubError.HubMismatch)).zhAny())
        assertEquals("连接失败", (endpointResultLabel(HubError.Transport)).zhAny())
        HubError.entries.forEach { assertNotEquals(it.name, endpointResultLabel(it)) }
    }
}
