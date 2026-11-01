// node --test site/worker.test.mjs
import assert from "node:assert/strict";
import test from "node:test";
import worker from "./worker.mjs";

const assets = { fetch: async (req) => new Response("asset " + new URL(req.url).pathname) };

test("static paths are the site's files", async () => {
  const r = await worker.fetch(new Request("https://site.example/privacy/"), { ASSETS: assets });
  assert.equal(await r.text(), "asset /privacy/");
});

test("/relay/* without the secrets is 503", async () => {
  const r = await worker.fetch(new Request("https://site.example/relay/v1/health"), { ASSETS: assets });
  assert.equal(r.status, 503);
});

test("/relay/* goes to the relay with the proxy secret and the client address", async () => {
  let seen;
  globalThis.fetch = async (url, init) => {
    seen = { url: String(url), init };
    return new Response("{}", { status: 200 });
  };
  const req = new Request("https://site.example/relay/v1/push?x=1", {
    method: "POST", body: '{"type":"alert"}',
    headers: { authorization: "Bearer g", cookie: "a=b", "x-relay-proxy-secret": "forged", "cf-connecting-ip": "198.51.100.7" },
  });
  const r = await worker.fetch(req, { ASSETS: assets, RELAY_ORIGIN: "https://origin.example", RELAY_PROXY_SECRET: "s3cret" });
  assert.equal(r.status, 200);
  assert.equal(seen.url, "https://origin.example/relay/v1/push?x=1");
  const h = seen.init.headers;
  assert.equal(h.get("x-relay-proxy-secret"), "s3cret");
  assert.equal(h.get("x-forwarded-for"), "198.51.100.7");
  assert.equal(h.get("authorization"), "Bearer g");
  assert.equal(h.get("cookie"), null);
  assert.equal(seen.init.method, "POST");
});
