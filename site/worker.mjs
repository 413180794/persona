// The site is static files, except /relay/*: the push relay (docs/push-relay.md), which runs elsewhere (`jarvisd relay`)
// and is reached at RELAY_ORIGIN, e.g. a Cloudflare Tunnel hostname. Both values are Worker secrets, not in the repo:
//   npx wrangler secret put RELAY_ORIGIN         # https://<tunnel host>
//   npx wrangler secret put RELAY_PROXY_SECRET   # the relay's proxy_secret
// Without them /relay/* answers 503 and the rest of the site is unchanged.
export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (!url.pathname.startsWith("/relay/")) return env.ASSETS.fetch(request);
    if (!env.RELAY_ORIGIN || !env.RELAY_PROXY_SECRET) {
      return Response.json({ error: "push relay not configured" }, { status: 503 });
    }
    const target = new URL(url.pathname + url.search, env.RELAY_ORIGIN);
    const headers = new Headers(request.headers);
    headers.delete("cookie");
    headers.delete("x-relay-proxy-secret"); // only the Worker's own counts
    headers.set("x-relay-proxy-secret", env.RELAY_PROXY_SECRET);
    // The relay rate-limits registration by client address.
    headers.set("x-forwarded-for", request.headers.get("cf-connecting-ip") || "");
    return fetch(target, { method: request.method, headers, body: request.body, redirect: "manual" });
  },
};
