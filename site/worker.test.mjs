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

function relayRequest(headers) {
  return new Request("https://site.example/relay/v1/register", { method: "POST", body: "{}", headers });
}

async function forwardedFor(headers, env) {
  let seen;
  globalThis.fetch = async (url, init) => { seen = init.headers; return new Response("{}"); };
  await worker.fetch(relayRequest(headers), { ASSETS: assets, RELAY_ORIGIN: "https://origin.example", RELAY_PROXY_SECRET: "s3cret", ...env });
  return seen;
}

test("an entry with the edge secret passes the client's address on", async () => {
  const h = await forwardedFor({ "cf-connecting-ip": "203.0.113.9", "x-forwarded-for": "198.51.100.7, 192.0.2.1", "x-persona-edge-secret": "edge" }, { EDGE_SECRET: "edge" });
  assert.equal(h.get("x-forwarded-for"), "198.51.100.7");
  assert.equal(h.get("x-persona-edge-secret"), null);
});

test("X-Forwarded-For is not believed without the right edge secret", async () => {
  for (const [headers, env] of [
    [{ "cf-connecting-ip": "203.0.113.9", "x-forwarded-for": "198.51.100.7" }, { EDGE_SECRET: "edge" }],
    [{ "cf-connecting-ip": "203.0.113.9", "x-forwarded-for": "198.51.100.7", "x-persona-edge-secret": "wrong" }, { EDGE_SECRET: "edge" }],
    [{ "cf-connecting-ip": "203.0.113.9", "x-forwarded-for": "198.51.100.7", "x-persona-edge-secret": "edge" }, {}],
  ]) {
    const h = await forwardedFor(headers, env);
    assert.equal(h.get("x-forwarded-for"), "203.0.113.9");
    assert.equal(h.get("x-persona-edge-secret"), null);
  }
});

// A fake R2 bucket: objects by key, with the parts of R2's API serveDownload uses.
function bucket(objects) {
  const meta = (key) => {
    const data = objects[key];
    return {
      key, size: data.length, httpEtag: `"${key}"`,
      writeHttpMetadata(h) { h.set("content-type", "application/gzip"); },
    };
  };
  return {
    calls: [],
    async head(key) { this.calls.push(["head", key]); return key in objects ? meta(key) : null; },
    async get(key, opts = {}) {
      this.calls.push(["get", key, opts]);
      if (!(key in objects)) return null;
      const m = meta(key);
      const inm = opts.onlyIf?.get?.("if-none-match");
      if (inm && inm === m.httpEtag) return m; // no body: the condition did not hold
      let data = objects[key];
      const header = opts.range?.get?.("range");
      if (header) {
        const [, a, b] = /bytes=(\d*)-(\d*)/.exec(header);
        if (a === "") { m.range = { suffix: Number(b) }; data = data.slice(-Number(b)); }
        else { const s = Number(a), e = b === "" ? data.length - 1 : Number(b); m.range = { offset: s, length: e - s + 1 }; data = data.slice(s, e + 1); }
      }
      return { ...m, body: new Blob([data]).stream() };
    },
  };
}

const dlAssets = (slot) => ({
  fetch: async (req) => {
    const p = new URL(req.url).pathname;
    if (p === "/dl/SLOT") return slot === null ? new Response("nope", { status: 404 }) : new Response(slot + "\n");
    return new Response("asset " + p);
  },
});

test("/dl/*.gz comes from the served slot of the bucket", async () => {
  const DL = bucket({ "b/jarvisd-linux-amd64.gz": "new bytes", "a/jarvisd-linux-amd64.gz": "old bytes" });
  const r = await worker.fetch(new Request("https://site.example/dl/jarvisd-linux-amd64.gz"), { ASSETS: dlAssets("b"), DL });
  assert.equal(r.status, 200);
  assert.equal(await r.text(), "new bytes");
  assert.equal(r.headers.get("content-length"), "9");
  assert.equal(r.headers.get("content-type"), "application/gzip");
  assert.equal(r.headers.get("accept-ranges"), "bytes");
});

test("/dl/*.gz HEAD gives the size without reading the object", async () => {
  const DL = bucket({ "a/jarvisd-linux-amd64.gz": "0123456789" });
  const r = await worker.fetch(new Request("https://site.example/dl/jarvisd-linux-amd64.gz", { method: "HEAD" }), { ASSETS: dlAssets("a"), DL });
  assert.equal(r.status, 200);
  assert.equal(r.headers.get("content-length"), "10");
  assert.deepEqual(DL.calls.map((c) => c[0]), ["head"]);
});

test("/dl/*.gz serves a range so a cut download resumes", async () => {
  const DL = bucket({ "a/x.gz": "0123456789" });
  const env = { ASSETS: dlAssets("a"), DL };
  let r = await worker.fetch(new Request("https://site.example/dl/x.gz", { headers: { range: "bytes=4-" } }), env);
  assert.equal(r.status, 206);
  assert.equal(await r.text(), "456789");
  assert.equal(r.headers.get("content-range"), "bytes 4-9/10");
  assert.equal(r.headers.get("content-length"), "6");
  r = await worker.fetch(new Request("https://site.example/dl/x.gz", { headers: { range: "bytes=-3" } }), env);
  assert.equal(await r.text(), "789");
  assert.equal(r.headers.get("content-range"), "bytes 7-9/10");
});

test("/dl/*.gz answers 304 when the client has it", async () => {
  const DL = bucket({ "a/x.gz": "0123456789" });
  const r = await worker.fetch(new Request("https://site.example/dl/x.gz", { headers: { "if-none-match": '"a/x.gz"' } }), { ASSETS: dlAssets("a"), DL });
  assert.equal(r.status, 304);
});

test("/dl/*.gz is 404 for a missing file or no slot, 405 for other methods", async () => {
  const DL = bucket({ "a/x.gz": "x" });
  let r = await worker.fetch(new Request("https://site.example/dl/y.gz"), { ASSETS: dlAssets("a"), DL });
  assert.equal(r.status, 404);
  r = await worker.fetch(new Request("https://site.example/dl/x.gz"), { ASSETS: dlAssets(null), DL });
  assert.equal(r.status, 404);
  r = await worker.fetch(new Request("https://site.example/dl/x.gz"), { ASSETS: dlAssets("../etc"), DL });
  assert.equal(r.status, 404);
  r = await worker.fetch(new Request("https://site.example/dl/x.gz", { method: "PUT", body: "x" }), { ASSETS: dlAssets("a"), DL });
  assert.equal(r.status, 405);
});

test("the other files under /dl/ stay the site's static files", async () => {
  for (const name of ["VERSION", "SHA256SUMS", "latest.json", "SLOT"]) {
    const r = await worker.fetch(new Request("https://site.example/dl/" + name), { ASSETS: dlAssets("a"), DL: bucket({}) });
    if (name !== "SLOT") assert.equal(await r.text(), "asset /dl/" + name);
  }
  // a name with a slash or a dot-dot is not a download
  const r = await worker.fetch(new Request("https://site.example/dl/a/x.gz"), { ASSETS: dlAssets("a"), DL: bucket({ "a/a/x.gz": "x" }) });
  assert.equal(await r.text(), "asset /dl/a/x.gz");
});
