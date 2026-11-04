// The site is static files, except /relay/*: the push relay (docs/push-relay.md), which runs elsewhere (`jarvisd relay`)
// and is reached at RELAY_ORIGIN, e.g. a Cloudflare Tunnel hostname. Both values are Worker secrets, not in the repo:
//   npx wrangler secret put RELAY_ORIGIN         # https://<tunnel host>
//   npx wrangler secret put RELAY_PROXY_SECRET   # the relay's proxy_secret
// Without them /relay/* answers 503 and the rest of the site is unchanged.
//
// An entry in front of the Worker (the Hong Kong entry for mainland China, docs/site.md) reaches it at its origin
// hostname, so cf-connecting-ip is the entry's address. The entry sends the client's address in X-Forwarded-For and
// proves itself with x-persona-edge-secret, the Worker secret EDGE_SECRET (`npx wrangler secret put EDGE_SECRET`);
// only then is X-Forwarded-For believed.
function clientAddress(request, env) {
  const own = request.headers.get("cf-connecting-ip") || "";
  const proof = request.headers.get("x-persona-edge-secret");
  if (!env.EDGE_SECRET || !proof || !sameSecret(proof, env.EDGE_SECRET)) return own;
  return (request.headers.get("x-forwarded-for") || "").split(",")[0].trim() || own;
}

function sameSecret(a, b) {
  if (a.length !== b.length) return false;
  let d = 0;
  for (let i = 0; i < a.length; i++) d |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return d === 0;
}

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
    headers.delete("x-persona-edge-secret");
    headers.set("x-relay-proxy-secret", env.RELAY_PROXY_SECRET);
    // The relay rate-limits registration by client address.
    headers.set("x-forwarded-for", clientAddress(request, env));
    return fetch(target, { method: request.method, headers, body: request.body, redirect: "manual" });
  },
};
