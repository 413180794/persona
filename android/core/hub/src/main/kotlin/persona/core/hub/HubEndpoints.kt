package persona.core.hub

import persona.core.contracts.Endpoint
import persona.core.contracts.api.EndpointCheck
import persona.core.contracts.api.EndpointOverrides
import java.net.Inet6Address
import java.net.InetAddress
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/** At most this many candidates can be added by hand per pairing. */
const val MAX_MANUAL_ENDPOINTS = 10

/**
 * The key every candidate comparison uses (dedupe, disabled, the user's order, the active route):
 * `scheme://host:port`, lowercase, explicit port, IPv6 in brackets, no trailing slash. Pairing addresses are stored as
 * `https://host:port/` while the hub sends `https://host:port`; both get the same key. A hub behind a reverse proxy at
 * a sub-path keeps it (`https://host:443/jarvis`): another prefix on the same host is another endpoint.
 */
fun endpointKey(url: String): String = url.trim().toHttpUrlOrNull()?.key() ?: url.trim().lowercase()

private fun HttpUrl.key() = "$scheme://${if (':' in host) "[$host]" else host}:$port" + encodedPath.trimEnd('/')

/**
 * `lan` for localhost, `*.local` and IP literals [policy] treats as local; `ipv6` / `ipv4` for other literals;
 * `name` for other hosts; `unknown` when [url] is not an http(s) URL. Never touches DNS.
 */
fun endpointKind(url: String, policy: LocalAddressPolicy = LocalAddressPolicy()): String {
    val host = url.trim().toHttpUrlOrNull()?.host ?: return "unknown"
    val literal = literalAddress(host)
    return when {
        host == "localhost" || host.endsWith(".local") || literal?.let(policy::allows) == true -> "lan"
        literal is Inet6Address -> "ipv6"
        literal != null -> "ipv4"
        else -> "name"
    }
}

/** The address of an IP literal host (IPv6 with or without brackets, strict dotted-quad IPv4); null for names. */
internal fun literalAddress(host: String): InetAddress? {
    val bare = host.removePrefix("[").removeSuffix("]")
    if (':' in bare) {
        if (!bare.all { it.isHex() || it == ':' || it == '.' }) return null
        // OkHttp's own parser decides validity; its canonical form is a literal getByName parses without DNS.
        val canonical = "http://[$bare]/".toHttpUrlOrNull()?.host ?: return null
        return runCatching { InetAddress.getByName(canonical) }.getOrNull()
    }
    return dottedQuad(bare)?.let(InetAddress::getByAddress)
}

private fun Char.isHex() = this in '0'..'9' || this in 'a'..'f' || this in 'A'..'F'

/** Four decimal parts 0–255 without leading zeros (`010.0.0.1` is octal to some parsers, so it is refused). */
private fun dottedQuad(text: String): ByteArray? {
    val parts = text.split('.')
    if (parts.size != 4) return null
    val bytes = ByteArray(4)
    for ((index, part) in parts.withIndex()) {
        if (part.isEmpty() || part.length > 3 || !part.all { it in '0'..'9' } || part.length > 1 && part[0] == '0') return null
        val value = part.toInt()
        if (value > 255) return null
        bytes[index] = value.toByte()
    }
    return bytes
}

/** A candidate typed on this phone, stored in its canonical form. */
fun manualEndpoint(url: String, policy: LocalAddressPolicy = LocalAddressPolicy()): Endpoint {
    val parsed = url.toHttpUrlOrNull()
    return Endpoint(url, endpointKind(url, policy), parsed?.port, parsed?.scheme)
}

/** Manual candidates (oldest first), then [base] in its order, deduplicated by key: a manual one wins. */
fun mergeEndpoints(manual: List<String>, base: List<Endpoint>, policy: LocalAddressPolicy = LocalAddressPolicy()): List<Endpoint> =
    (manual.map { manualEndpoint(it, policy) } + base).distinctBy { endpointKey(it.url) }

/**
 * The default order, used until the user arranges one: by kind (ipv6, name, lan, ipv4, anything else), https before
 * http, [manual] keys before the pairing's and the hub's, then as given. A duplicate key keeps its first spelling.
 */
fun orderEndpoints(candidates: List<Endpoint>, manual: Set<String> = emptySet()): List<Endpoint> =
    candidates.map { endpointKey(it.url) to it }.distinctBy { it.first }.sortedWith(
        compareBy<Pair<String, Endpoint>> { (_, endpoint) -> kindRank(endpoint.kind) }
            .thenBy { (key, _) -> if (key.startsWith("https://")) 0 else 1 }
            .thenBy { (key, _) -> if (key in manual) 0 else 1 },
    ).map { it.second }

/**
 * Every candidate in the one order the connection tries them and settings lists them; disabled ones keep their place
 * (the connection skips them). With [EndpointOverrides.order] the user's order wins: keys that are no longer candidates
 * are skipped, and candidates it does not list (new ones from the hub) follow in the default order. Without it, the
 * default order ([orderEndpoints]).
 */
fun arrangeEndpoints(candidates: List<Endpoint>, overrides: EndpointOverrides): List<Endpoint> {
    val manual = overrides.manual.mapTo(HashSet(), ::endpointKey)
    val order = overrides.order ?: return orderEndpoints(candidates, manual)
    val byKey = LinkedHashMap<String, Endpoint>()
    candidates.forEach { byKey.putIfAbsent(endpointKey(it.url), it) }
    val listed = order.distinct().mapNotNull(byKey::get)
    val listedKeys = listed.mapTo(HashSet()) { endpointKey(it.url) }
    return listed + orderEndpoints(byKey.values.filter { endpointKey(it.url) !in listedKeys }, manual)
}

private fun kindRank(kind: String) = when (kind) {
    "ipv6" -> 0
    "name" -> 1
    "lan" -> 2
    "ipv4" -> 3
    else -> 4
}

/** What a probe or a settings test found, in Chinese; null is a candidate that passed the identity check. */
fun endpointResultLabel(error: HubError?): String = when (error) {
    null -> "已通过身份校验"
    HubError.AddressNotFound -> "找不到地址"
    HubError.ConnectionRefused -> "连接被拒绝"
    HubError.Unreachable -> "无法到达"
    HubError.Timeout -> "连接超时"
    HubError.CertificateMismatch -> "证书指纹不符"
    HubError.HubMismatch -> "不是配对的那台 hub"
    HubError.TlsRequired -> "HTTP 只能连内网地址"
    HubError.NoNetwork -> "没有网络"
    HubError.Server -> "hub 返回错误"
    HubError.Protocol -> "响应格式不对"
    HubError.PairingExpired -> "hub 拒绝了请求"
    HubError.RateLimited -> "请求太频繁，稍后再试"
    else -> "连接失败"
}

/**
 * Validates an address typed in settings without DNS or network. [taken] holds the keys of every current candidate
 * (disabled ones too), [manualCount] how many were added by hand; [replacingKey] is the manual candidate being edited.
 */
fun checkEndpoint(
    input: String, pinned: Boolean, policy: LocalAddressPolicy, taken: Set<String>, manualCount: Int,
    replacingKey: String? = null,
): EndpointCheck {
    fun invalid(message: String) = EndpointCheck.Invalid(message)
    if (input.isBlank()) return invalid("请输入地址")
    val text = input.trim()
    if (text.any { it.isWhitespace() }) return invalid("地址里不能有空格")
    val separator = text.indexOf("://")
    val scheme = if (separator < 0) "https" else text.substring(0, separator).lowercase()
    if (scheme != "https" && scheme != "http") return invalid("只支持 https:// 或 http:// 开头的地址")
    val rest = if (separator < 0) text else text.substring(separator + 3)
    val end = rest.indexOfFirst { it == '/' || it == '?' || it == '#' || it == '\\' }.let { if (it < 0) rest.length else it }
    val authority = rest.substring(0, end)
    if ('@' in authority) return invalid("地址里不能带用户名或密码")
    if (rest.substring(end).let { it.isNotEmpty() && it != "/" }) return invalid("只写到端口为止，不要带路径、? 或 #")

    val missingPort = "缺少端口，写成 https://地址:端口"
    val host: String
    val port: String?
    if (authority.startsWith("[")) {
        val close = authority.indexOf(']')
        if (close < 0) return invalid("IPv6 地址少了右方括号")
        host = authority.substring(1, close)
        val after = authority.substring(close + 1)
        port = if (after.isEmpty()) null else if (after.startsWith(":")) after.substring(1) else return invalid(missingPort)
        if (host.isEmpty()) return invalid("缺少主机名或 IP")
        if ('%' in host) return invalid("不支持带 % 的链路本地地址")
        if (':' !in host || literalAddress(host) == null) return invalid("IPv6 地址不对")
    } else {
        if (authority.count { it == ':' } > 1) return invalid("IPv6 地址要加方括号，例如 https://[2001:db8::1]:18662")
        val colon = authority.indexOf(':')
        host = if (colon < 0) authority else authority.substring(0, colon)
        port = if (colon < 0) null else authority.substring(colon + 1)
        if (host.isEmpty()) return invalid("缺少主机名或 IP")
        if (host.all { it in '0'..'9' || it == '.' } && dottedQuad(host) == null) return invalid("IPv4 地址不对")
        // No percent-escapes: OkHttp would decode them into a different host than the one shown.
        if ('%' in host || "http://$host/".toHttpUrlOrNull() == null) return invalid("主机名不对")
    }
    if (port.isNullOrEmpty()) return invalid(missingPort)
    val number = port.takeIf { it.all { char -> char in '0'..'9' } }?.toIntOrNull()?.takeIf { it in 1..65535 }
        ?: return invalid("端口要在 1–65535 之间")

    // Runs on every keystroke: never throw, whatever was typed.
    val url = runCatching { HttpUrl.Builder().scheme(scheme).host(host).port(number).build() }.getOrNull()
        ?: return invalid("主机名不对")
    val literal = literalAddress(url.host)
    if (literal != null && (literal.isAnyLocalAddress || literal.isMulticastAddress ||
            literal.address.size == 4 && literal.address.all { it == 0xff.toByte() })) {
        return invalid("这不是能连接的地址")
    }
    if (scheme == "https" && !pinned) {
        return invalid("这次配对没有 hub 的证书指纹，无法校验 HTTPS 地址。请在 hub 开启 TLS 后重新配对。")
    }
    // The same rule as HubHttpClient's guard, which still checks the resolved peer at runtime.
    if (scheme == "http" && !(literal?.let(policy::allows) ?: (url.host == "localhost" || url.host.endsWith(".local")))) {
        return invalid("HTTP 不加密，只能用内网地址（如 192.168.x.x、10.x.x.x、fd00::/8 或 .local 名字）。公网地址请用 https://")
    }
    val key = url.key()
    if (key in taken && key != replacingKey) return invalid("列表里已经有这个地址了")
    if (replacingKey == null && manualCount >= MAX_MANUAL_ENDPOINTS) return invalid("最多手动添加 $MAX_MANUAL_ENDPOINTS 条地址")
    return EndpointCheck.Valid(key, endpointKind(key, policy))
}
