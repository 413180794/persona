// Protocol 1 is opt-in: this module has no socket, endpoint, runtime or capability registration.
// All wire contracts come from the schema generator. Canonical trees never pass through readView.
import streamSchema from "../../../schema/extension/stream.schema.json?raw";
import viewSchema from "../../../schema/extension/view.schema.json?raw";
import protocolSchema from "../../../schema/extension/protocol.schema.json?raw";
import type {
  Action, Component, Section, Tile, UIContext, UIStreamDelta, UIStreamEvent,
  UIStreamEventRequest, UIStreamOperation, UIStreamRequest, UIStreamSnapshot, UIStreamValue, View,
} from "./extension-wire";
import { COMPONENT_TYPES } from "./extension-wire";
import { type UIView } from "./extension-ui";

/** Native tags offered by the real app adapter only after its authenticated hello gate. */
export const NATIVE_STREAM_COMPONENTS: readonly string[] = COMPONENT_TYPES.filter((tag) => tag !== "html");
/** Tag negotiation is not field negotiation. Refuse known tags with fields we cannot draw safely. */
export function nativeStreamGap(view: View): string | undefined {
  if (view.pages !== undefined) return "assistant detail pages";
  if (view.refresh_after) return "legacy refresh_after timing";
  for (const section of view.sections) for (const item of section.items) {
    if (item.type === "chart" && item.kind !== "bar" && item.kind !== "line") return `chart.${item.kind}`;
    if (item.type === "chart" && item.series.some((series) => series.color !== undefined)) return "chart.series.color";
    if (item.type === "html") return "html";
  }
  return undefined;
}
import { uuid } from "./format";

// Allow the manager's bounded confirmation and callback windows plus transport time.
export const STREAM_LIMITS = Object.freeze({ frame: 1024 * 1024, request: 64 * 1024, operations: 256, nodes: 300, depth: 16, pending: 64, delivery: 45_000 });
const bytes = (s: string) => new TextEncoder().encode(s).length;
const object = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === "object" && !Array.isArray(v);
const has = (v: object, key: string) => Object.prototype.hasOwnProperty.call(v, key);
function fail(why: string): never { throw new Error(why); }
const length = (s: string) => Array.from(s).length;
function key(v: unknown): asserts v is string {
  if (typeof v !== "string" || length(v) < 1 || length(v) > 128) fail("Invalid node key");
}

export function decodeStreamEnvelope(input: unknown) {
  const raw = decode(input, STREAM_LIMITS.frame, true);
  schema("UIStreamServerEnvelope", raw);
  return raw as import("./extension-wire").UIStreamServerEnvelope;
}

// Validate the same schema subset as the generator, including reference overlays. It is data, not
// extension JavaScript. Discriminator dispatch avoids scanning all native variants for every item.
type Schema = Record<string, unknown>;
const defs = new Map<string, Schema>();
for (const source of [streamSchema, viewSchema, protocolSchema]) {
  const parsed = JSON.parse(source) as { $defs: Record<string, Schema> };
  for (const [name, schema] of Object.entries(parsed.$defs)) defs.set(name, schema);
}
const ref = (s: Schema): Schema => defs.get(String(s.$ref).split("/$defs/")[1]) ?? fail("Unknown schema reference");
function validates(s: Schema, v: unknown): boolean {
  if (s.$ref && !validates(ref(s), v)) return false;
  if (has(s, "const") && v !== s.const) return false;
  if (Array.isArray(s.enum) && !s.enum.includes(v)) return false;
  if (s.oneOf) {
    let branches = s.oneOf as Schema[];
    if (typeof s["x-discriminator"] === "string" && object(v)) {
      const tag = s["x-discriminator"];
      branches = branches.filter((b) => {
        const target = b.$ref ? ref(b) : b;
        return (target.properties as Record<string, Schema>)?.[tag]?.const === v[tag];
      });
    }
    if (branches.filter((b) => validates(b, v)).length !== 1) return false;
  }
  if (s.type === "null" && v !== null) return false;
  if (s.type === "boolean" && typeof v !== "boolean") return false;
  if (s.type === "string") {
    if (typeof v !== "string") return false;
    const n = length(v);
    if (typeof s.minLength === "number" && n < s.minLength || typeof s.maxLength === "number" && n > s.maxLength) return false;
    if (typeof s.pattern === "string" && !new RegExp(s.pattern).test(v)) return false;
  }
  if (s.type === "number" || s.type === "integer") {
    if (typeof v !== "number" || !Number.isFinite(v) || s.type === "integer" && !Number.isSafeInteger(v)) return false;
    if (typeof s.minimum === "number" && v < s.minimum || typeof s.maximum === "number" && v > s.maximum) return false;
  }
  if (s.type === "array") {
    if (!Array.isArray(v)) return false;
    if (typeof s.minItems === "number" && v.length < s.minItems || typeof s.maxItems === "number" && v.length > s.maxItems) return false;
    if (s.uniqueItems && new Set(v.map((x) => JSON.stringify(x))).size !== v.length) return false;
    if (s.items && !v.every((x) => validates(s.items as Schema, x))) return false;
  }
  // Overlays have properties without repeating type: object. Required-only oneOf branches too.
  if (s.type === "object" || s.properties || s.required) {
    if (!object(v)) return false;
    const names = Object.keys(v);
    if (typeof s.minProperties === "number" && names.length < s.minProperties || typeof s.maxProperties === "number" && names.length > s.maxProperties) return false;
    if (Array.isArray(s.required) && !s.required.every((k) => has(v, k))) return false;
    const props = (s.properties ?? {}) as Record<string, Schema>;
    for (const k of names) {
      if (s.propertyNames && !validates(s.propertyNames as Schema, k)) return false;
      if (has(props, k)) { if (!validates(props[k], v[k])) return false; }
      else if (s.additionalProperties === false) return false;
      else if (object(s.additionalProperties) && !validates(s.additionalProperties, v[k])) return false;
    }
  }
  return true;
}
function schema(name: string, value: unknown) {
  if (!validates(defs.get(name) ?? fail("Unknown schema"), value)) fail(`Invalid ${name}`);
}

// Check JSON nesting and version literals BEFORE parsing. JS cannot recover rounded-away digits
// from an already-decoded caller object; callers with raw frames should pass the original string.
function integralVersion(raw: string): boolean {
  const m = /^(-?)(\d+)(?:\.(\d+))?(?:[eE]([+-]?\d+))?$/.exec(raw);
  if (!m || m[1]) return false;
  const digits = (m[2] + (m[3] ?? "")).replace(/^0+/, "");
  if (!digits) return false;
  const shift = Number(m[4] ?? 0) - (m[3]?.length ?? 0);
  if (!Number.isSafeInteger(shift) || shift > 16 || shift < -digits.length) return false;
  if (shift < 0 && !/^0*$/.test(digits.slice(digits.length + shift))) return false;
  const whole = shift < 0 ? digits.slice(0, digits.length + shift) : digits + "0".repeat(shift);
  return whole.length <= 16 && BigInt(whole || "0") > 0n && BigInt(whole || "0") <= 9007199254740991n;
}
function inspectJSON(text: string, versions: boolean) {
  let depth = 0;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (c === '"') {
      const start = i++;
      while (i < text.length && text[i] !== '"') { if (text[i] === "\\") i++; i++; }
      const token = text.slice(start, i + 1);
      if (versions && depth <= 2 && (token === '"version"' || token === '"base_version"' || token.includes("\\"))) {
        const name: unknown = JSON.parse(token);
        if (name === "version" || name === "base_version") {
          const rest = text.slice(i + 1);
          const number = /^\s*:\s*(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(rest);
          if (number && !integralVersion(number[1])) fail("Invalid raw stream version");
        }
      }
    } else if (c === "{" || c === "[") {
      if (++depth > STREAM_LIMITS.depth) fail("Frame nesting limit");
    } else if (c === "}" || c === "]") depth--;
  }
}
function jsonData(v: unknown, depth = 0, ancestors = new Set<object>()) {
  if (typeof v === "string") { if (bytes(v) > STREAM_LIMITS.frame) fail("String limit"); return; }
  if (v === null || typeof v === "boolean" || typeof v === "number" && Number.isFinite(v)) return;
  if (typeof v !== "object" || !v) fail("Non-JSON value");
  if (depth >= STREAM_LIMITS.depth || ancestors.has(v)) fail("Frame nesting/cycle limit");
  if (!Array.isArray(v) && Object.getPrototypeOf(v) !== Object.prototype && Object.getPrototypeOf(v) !== null) fail("Non-JSON object");
  ancestors.add(v);
  for (const x of Object.values(v)) jsonData(x, depth + 1, ancestors);
  ancestors.delete(v);
}
function decode(input: unknown, limit = STREAM_LIMITS.frame, versions = false): unknown {
  let text: string;
  if (typeof input === "string") text = input;
  else { jsonData(input); text = JSON.stringify(input); }
  if (bytes(text) > limit) fail("Frame byte limit");
  inspectJSON(text, versions);
  return JSON.parse(text);
}
function freeze<T>(v: T): T {
  if (v && typeof v === "object" && !Object.isFrozen(v)) {
    for (const x of Object.values(v)) freeze(x);
    Object.freeze(v);
  }
  return v;
}

export function same(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (Array.isArray(a) || Array.isArray(b)) return Array.isArray(a) && Array.isArray(b) && a.length === b.length && a.every((v, i) => same(v, b[i]));
  if (!object(a) || !object(b)) return false;
  const keys = Object.keys(a);
  return keys.length === Object.keys(b).length && keys.every((k) => has(b, k) && same(a[k], b[k]));
}
/** Keep unchanged native display nodes/controls stable, including MoreLine's observer dependency. */
function displayView(before: UIView | null, raw: View): UIView {
  // Validation already ran on the generated schema. Unlike readView, this projection drops no native fields.
  const next = { ...raw, sections: raw.sections.map((s) => ({ ...s, items: [...s.items] })) } as UIView;
  const previous = new Map(before?.sections.map((s) => [s.key, s]) ?? []);
  next.sections = next.sections.map((s) => {
    const old = previous.get(s.key);
    if (!old) return s;
    if (same(old, s)) return old;
    const items = new Map(old.items.map((it) => [it.key, it]));
    return { ...s, ...(same(old.more, s.more) ? { more: old.more } : {}), items: s.items.map((it) => {
      const prior = items.get(it.key); return prior && same(prior, it) ? prior : it;
    }) };
  });
  return next;
}

type Node = { kind: "root"; value: View } | { kind: "section"; value: Section; index: number }
  | { kind: "item"; value: Component; section: number; index: number } | { kind: "tile"; value: Tile };
function indexTree(s: UIStreamSnapshot): Map<string, Node> {
  const nodes = new Map<string, Node>();
  const add = (k: unknown, n: Node) => {
    key(k);
    if (nodes.has(k) || nodes.size >= STREAM_LIMITS.nodes) fail("Duplicate key or node limit");
    nodes.set(k, n);
  };
  add(s.root_key, { kind: "root", value: s.view });
  s.view.sections.forEach((section, si) => {
    add(section.key, { kind: "section", value: section, index: si });
    section.items.forEach((item, ii) => {
      add(item.key, { kind: "item", value: item, section: si, index: ii });
      if (item.type === "html") fail("Extension HTML is forbidden");
      if (item.type === "grid" || item.type === "carousel") {
        for (const tile of item.tiles) add(tile.key, { kind: "tile", value: tile });
      }
    });
  });
  if (s.view.state !== undefined && bytes(s.view.state) > 16 * 1024) fail("State byte limit");
  return nodes;
}
function validateTree(s: UIStreamSnapshot) {
  schema("UIStreamSnapshot", s); indexTree(s);
  for (const section of s.view.sections) for (const item of section.items) {
    if (item.type === "chart") {
      const labels = item.series[0].points.map((p) => p.label);
      if (item.series.some((series) => series.points.length !== labels.length || series.points.some((p, i) => p.label !== labels[i]))) fail("Inconsistent chart series labels");
    }
  }
}
function apply(s: UIStreamSnapshot, op: UIStreamOperation): UIStreamSnapshot {
  const nodes = indexTree(s);
  const node = (k: string) => nodes.get(k) ?? fail("Missing node");
  let view = s.view;
  const sections = [...view.sections];
  const editItems = (si: number, edit: (items: Component[]) => void) => {
    const items = [...sections[si].items];
    edit(items);
    sections[si] = { ...sections[si], items };
  };
  if (op.op === "replace") {
    const n = node(op.key);
    if (n.kind === "root" && op.view) view = op.view;
    else if (n.kind === "section" && op.section && op.section.key === op.key) sections[n.index] = op.section;
    else if (n.kind === "item" && op.item && op.item.key === op.key) editItems(n.section, (a) => { a[n.index] = op.item!; });
    else fail("Replacement kind/key mismatch");
  } else if (op.op === "remove") {
    const n = node(op.key);
    if (n.kind === "section") sections.splice(n.index, 1);
    else if (n.kind === "item") editItems(n.section, (a) => { a.splice(n.index, 1); });
    else fail("Cannot remove root/tile");
  } else {
    const parent = node(op.parent);
    const moving = op.op === "move" ? node(op.key) : undefined;
    const value = moving?.value ?? (op.op === "insert" ? op.section ?? op.item : undefined);
    const kind = moving?.kind ?? (op.op === "insert" && op.section ? "section" : "item");
    if (op.op === "move" && (op.before === op.key || op.parent === op.key)) fail("Self destination");
    if (!(kind === "section" && parent.kind === "root" || kind === "item" && parent.kind === "section")) fail("Invalid destination kind");
    if (op.before !== undefined) {
      const sibling = node(op.before);
      if (!(sibling.kind === "section" && parent.kind === "root" || sibling.kind === "item" && parent.kind === "section" && sibling.section === parent.index)) fail("Invalid destination sibling");
    }
    // Remove first, then resolve before against the destination's current ordering, never append on a bad key.
    if (moving?.kind === "section") sections.splice(moving.index, 1);
    else if (moving?.kind === "item") editItems(moving.section, (a) => { a.splice(moving.index, 1); });
    if (kind === "section") {
      const at = op.before === undefined ? sections.length : sections.findIndex((x) => x.key === op.before);
      if (at < 0) fail("Missing destination sibling");
      sections.splice(at, 0, value as Section);
    } else if (parent.kind === "section") {
      editItems(parent.index, (items) => {
        const at = op.before === undefined ? items.length : items.findIndex((x) => x.key === op.before);
        if (at < 0) fail("Missing destination sibling");
        items.splice(at, 0, value as Component);
      });
    }
  }
  const next = { ...s, view: view !== s.view ? view : { ...view, sections } };
  // Operation payloads were schema-checked once at the frame boundary. Check staged topology after
  // each operation, but walk all native payload fields only once for the final atomic materialization.
  indexTree(next);
  return next;
}

export interface StreamReduction { snapshot: UIStreamSnapshot | null; recover: boolean; reason?: string }
/** Fail-closed and atomic, including for unknown operations. Recovery never repeats an interaction. */
export function reduceUIStream(current: UIStreamSnapshot | null, kind: "snapshot" | "delta", input: unknown): StreamReduction {
  try {
    const raw = decode(input, STREAM_LIMITS.frame, true);
    schema(kind === "snapshot" ? "UIStreamSnapshot" : "UIStreamDelta", raw);
    if (kind === "snapshot") {
      const s = raw as UIStreamSnapshot;
      if (current && (s.session !== current.session || s.epoch !== current.epoch || s.root_key !== current.root_key || s.version < current.version)) fail("Stale snapshot identity/version");
      validateTree(s);
      if (current && s.version === current.version) {
        if (!same(s, current)) fail("Conflicting snapshot at the same version");
        return { snapshot: current, recover: false };
      }
      return { snapshot: freeze(s), recover: false };
    }
    const d = raw as UIStreamDelta;
    if (!current || d.session !== current.session || d.epoch !== current.epoch || d.base_version !== current.version) fail("Delta identity/base mismatch");
    if (d.operations.length === 0 ? d.version !== d.base_version && d.version !== d.base_version + 1 : d.version !== d.base_version + 1) fail("Invalid delta version ordering");
    if (d.operations.length === 0) return { snapshot: d.version === current.version ? current : freeze({ ...current, version: d.version }), recover: false };
    let staged = current;
    for (const op of d.operations) staged = apply(staged, op);
    const next = { ...staged, version: d.version };
    validateTree(next);
    if (bytes(JSON.stringify(next)) > STREAM_LIMITS.frame) fail("Materialized frame byte limit");
    return { snapshot: freeze(next), recover: false };
  } catch (e) {
    return { snapshot: current, recover: true, reason: e instanceof Error ? e.message : "Invalid frame" };
  }
}

type Slot = { key: string; action: Action; event: "press" | "change" | "submit" };
/** Native callback locality; this is not server authorization. IDs may be reused on another node. */
export function nativeStreamSlots(s: UIStreamSnapshot): Slot[] {
  const out: Slot[] = [];
  // Event slots belong to native surfaces; Action.submit only controls value collection.
  const add = (k: string, a: Action | undefined, event: Slot["event"] = "press") => { if (a) out.push({ key: k, action: a, event }); };
  for (const top of [s.view.search, s.view.filter, s.view.tabs]) add(s.root_key, top?.action, "change");
  for (const b of s.view.review?.buttons ?? []) add(s.root_key, b.action, "submit");
  add(s.root_key, s.view.review?.discard);
  const itemSlots = (k: string, n: Component | Tile) => {
    if ("action" in n) add(k, n.action, "type" in n && n.type === "segmented" ? "change" : "press");
    if ("control" in n && !n.control?.disabled) add(k, n.control?.action, "change");
    if ("actions" in n) for (const a of n.actions ?? []) add(k, a.action);
    if ("button" in n) add(k, n.button?.action);
    if ("buttons" in n) for (const b of n.buttons ?? []) add(k, b.action, "submit");
    if ("crumbs" in n) for (const c of n.crumbs) add(k, c.action);
    if ("expand" in n) add(k, n.expand);
    if ("load" in n) add(k, n.load);
    if ("context" in n) add(k, n.context);
    if ("mark_viewed" in n) add(k, n.mark_viewed);
    if ("draft" in n) add(k, n.draft);
    if ("reply" in n) add(k, n.reply);
    if ("comment" in n) add(k, n.comment);
    if ("threads" in n) for (const thread of n.threads ?? []) {
      add(k, thread.reply);
      add(k, thread.resolve);
    }
  };
  for (const section of s.view.sections) {
    add(section.key!, section.more?.action);
    for (const item of section.items) {
      itemSlots(item.key!, item);
      if (item.type === "grid" || item.type === "carousel") for (const tile of item.tiles) itemSlots(tile.key!, tile);
    }
  }
  return out;
}

/** Local partition key only; transport MUST bind authenticated owner and hub group independently. */
export interface StreamPageIdentity { owner: string; hubGroup: string; extension: string; contribution: string; navigation: string }
export const streamPageKey = (i: StreamPageIdentity) => {
  const parts = [i.owner, i.hubGroup, i.extension, i.contribution, i.navigation];
  if (parts.some((x) => typeof x !== "string" || !x || bytes(x) > 4096)) fail("Invalid local page identity");
  return JSON.stringify(parts);
};
export interface UIStreamTransport {
  /** Resolve false for a known unaccepted receipt, void/true for accepted delivery; reject only on uncertainty. */
  send(request: UIStreamRequest): Promise<void | boolean>;
}
export interface StreamPageOptions {
  identity: StreamPageIdentity;
  transport: UIStreamTransport;
  context: UIContext;
  state?: string;
  id?: () => string;
}
export interface StreamPageState {
  snapshot: UIStreamSnapshot | null;
  display: UIView | null;
  phase: "cold" | "opening" | "ready" | "recovering" | "suspended" | "disconnected" | "closed";
  pending: number;
  error?: string;
}
type Pending = { request: UIStreamEventRequest; replay?: boolean; resolve: (accepted: void | boolean) => void; reject: (e: Error) => void };

export class ExtensionUIStreamSession {
  readonly identity: Readonly<StreamPageIdentity>;
  readonly pageKey: string;
  private state: StreamPageState = { snapshot: null, display: null, phase: "cold", pending: 0 };
  private listeners = new Set<() => void>();
  private queue: Pending[] = [];
  private inFlight = false;
  private active?: Pending;
  private controls = 0;
  private deferredControl = false;
  private connected = true;
  private visible = true;
  private generation = 0;
  private awaitingSnapshot = true;
  private pendingRead = false;
  private openingRequest?: UIStreamRequest;
  private retiredEpochs = new Set<string>();
  private drafts = new Map<string, string>();
  private local = new Map<string, unknown>();
  private observedMore = new Map<string, Section["more"]>();
  private uncertain = new Map<string, UIStreamEventRequest>();
  private id: () => string;
  constructor(private options: StreamPageOptions) {
    this.identity = Object.freeze({ ...options.identity });
    this.pageKey = streamPageKey(this.identity);
    this.id = options.id ?? uuid;
    if (options.state !== undefined && (typeof options.state !== "string" || bytes(options.state) > 16 * 1024)) fail("State byte limit");
    if (!options.context.stream || options.context.stream.protocol !== 1) fail("Explicit stream capability required");
    schema("UIStreamCapability", options.context.stream);
    if (options.context.stream.components.some((t) => !NATIVE_STREAM_COMPONENTS.includes(t))) fail("Unsupported native display capability");
    // Validate before retaining any caller-provided context; it cannot be changed underneath an open.
    this.options = { ...options, context: freeze(decode(options.context) as UIContext) };
  }
  getState = (): StreamPageState => this.state;
  subscribe = (fn: () => void) => { this.listeners.add(fn); return () => { this.listeners.delete(fn); }; };
  private publish(patch: Partial<StreamPageState>) {
    const pending = this.state.phase === "closed" ? 0 : this.queue.length + (this.inFlight ? 1 : 0) + this.controls;
    this.state = Object.freeze({ ...this.state, ...patch, pending: patch.phase === "closed" ? 0 : pending });
    for (const listener of this.listeners) listener();
  }
  private async request(r: UIStreamRequest): Promise<void | boolean> {
    schema("UIStreamRequest", decode(r, STREAM_LIMITS.request));
    // The caller settles delivery on disconnect; the deadline also bounds a stalled injected adapter.
    let timer: ReturnType<typeof setTimeout> | undefined;
    try {
      return await Promise.race([
        this.options.transport.send(freeze(decode(r, STREAM_LIMITS.request) as UIStreamRequest)),
        new Promise<never>((_, reject) => { timer = setTimeout(() => reject(new Error("Stream delivery timeout")), STREAM_LIMITS.delivery); }),
      ]);
    } finally { if (timer !== undefined) clearTimeout(timer); }
  }
  private control(r: UIStreamRequest) {
    const mine = this.generation;
    if (this.controls >= 8 || this.queue.length + (this.inFlight ? 1 : 0) + this.controls >= STREAM_LIMITS.pending) {
      this.deferredControl = true;
      this.pendingRead = false;
      this.awaitingSnapshot = true;
      this.cancelQueued("Lifecycle pending limit");
      this.publish({ error: "Lifecycle pending limit", phase: !this.connected ? "disconnected" : !this.visible ? "suspended" : "recovering" });
      return;
    }
    this.controls++;
    this.publish({});
    void this.request(r).catch((e) => {
      const current = this.state.snapshot;
      const readSatisfied = (r.request === "open" || r.request === "resume" || r.request === "snapshot") && !this.awaitingSnapshot;
      if (mine === this.generation && this.state.phase !== "closed" && !readSatisfied && !("epoch" in r && current && (r.session !== current.session || r.epoch !== current.epoch))) {
        this.pendingRead = false;
        this.publish({ error: String(e) });
      }
    }).finally(() => {
      this.controls--;
      if (mine !== this.generation || this.state.phase === "closed") return;
      this.publish({});
      if (this.deferredControl && this.connected) {
        this.deferredControl = false;
        if (this.visible) this.refresh();
        else {
          const s = this.state.snapshot;
          if (s) this.control({ request: "suspend", protocol: 1, request_id: this.id(), session: s.session, epoch: s.epoch });
        }
      }
    });
  }
  open() {
    if (this.state.phase !== "cold" || !this.connected || !this.visible) return;
    this.openingRequest = { request: "open", protocol: 1, request_id: this.id(), extension: this.identity.extension, contribution: this.identity.contribution, context: this.options.context, state: this.options.state ?? "" };
    this.publish({ phase: "opening", error: undefined });
    this.control(this.openingRequest);
  }
  private recover(reason: string) {
    if (this.state.phase === "closed") return;
    this.awaitingSnapshot = true;
    this.cancelQueued(reason);
    this.publish({ phase: !this.connected ? "disconnected" : !this.visible ? "suspended" : "recovering", error: reason });
    const s = this.state.snapshot;
    if (this.pendingRead || !this.connected || !this.visible) return;
    this.pendingRead = true;
    if (s) this.control({ request: "snapshot", protocol: 1, request_id: this.id(), session: s.session, epoch: s.epoch, version: s.version });
    else if (this.openingRequest) this.control(this.openingRequest);
  }
  /** Explicit read-only recovery retry after a failed lifecycle send. Never retries an interaction. */
  refresh() {
    if (this.state.phase === "closed") return;
    if (!this.state.snapshot && !this.openingRequest) { this.publish({ phase: "cold" }); this.open(); return; }
    this.pendingRead = false; this.recover("Snapshot requested");
  }
  /** Only a correlated, manager-declared missing session permits a fresh open. Never replay actions. */
  reopenExpired(session: string, epoch: string) {
    const previous = this.state.snapshot;
    if (this.state.phase === "closed" || previous?.session !== session || previous.epoch !== epoch) return;
    this.options = { ...this.options, state: previous.view.state ?? this.options.state };
    this.generation++;
    this.active?.reject(new Error("Stream session expired"));
    this.active = undefined; this.inFlight = false;
    this.cancelQueued("Stream session expired");
    this.awaitingSnapshot = true; this.pendingRead = false; this.deferredControl = false;
    this.openingRequest = undefined;
    this.drafts.clear(); this.local.clear(); this.uncertain.clear(); this.retiredEpochs.clear(); this.observedMore.clear();
    this.publish({ snapshot: null, display: null, phase: !this.connected ? "disconnected" : !this.visible ? "suspended" : "cold", error: undefined });
    this.open();
  }
  /** Caller routes by this page's authenticated owner/group and correlates open results, not a guessed WS envelope. */
  receiveSnapshot(input: unknown): boolean {
    if (this.state.phase === "closed" || !this.connected || !this.visible || this.state.phase === "cold") return false;
    try {
      const raw = decode(input, STREAM_LIMITS.frame, true) as UIStreamSnapshot;
      schema("UIStreamSnapshot", raw);
      const previous = this.state.snapshot;
      if (previous && (raw.session !== previous.session || raw.root_key !== previous.root_key || this.retiredEpochs.has(raw.epoch))) fail("Stale snapshot session/epoch");
      if (previous && raw.epoch !== previous.epoch && !this.awaitingSnapshot) fail("Unrequested epoch");
      const result = reduceUIStream(previous?.epoch === raw.epoch ? previous : null, "snapshot", input);
      if (result.recover || !result.snapshot) fail(result.reason ?? "Invalid snapshot");
      const supported = this.options.context.stream!.components;
      if (result.snapshot.view.sections.some((s) => s.items.some((it) => !supported.includes(it.type)))) fail("Unnegotiated native component");
      if (previous && previous.epoch !== raw.epoch) {
        if (this.retiredEpochs.size >= 64) fail("Epoch history limit; reopen session");
        this.retiredEpochs.add(previous.epoch);
        this.uncertain.clear();
        this.observedMore.clear();
      }
      this.commit(result.snapshot);
      this.awaitingSnapshot = false;
      this.pendingRead = false;
      this.deferredControl = false;
      this.publish({ phase: "ready", error: undefined });
      void this.pump();
      return true;
    } catch (e) { this.recover(String(e)); return false; }
  }
  receiveDelta(input: unknown): boolean {
    if (this.state.phase !== "ready" || this.awaitingSnapshot || !this.visible || !this.connected) return false;
    const result = reduceUIStream(this.state.snapshot, "delta", input);
    if (result.recover || !result.snapshot) { this.recover(result.reason ?? "Invalid delta"); return false; }
    if (result.snapshot.view.sections.some((s) => s.items.some((it) => !this.options.context.stream!.components.includes(it.type)))) { this.recover("Unnegotiated native component"); return false; }
    this.commit(result.snapshot);
    this.publish({ error: undefined });
    return true;
  }
  private commit(snapshot: UIStreamSnapshot) {
    const nodes = indexTree(snapshot);
    const previousNodes = this.state.snapshot && indexTree(this.state.snapshot);
    for (const k of this.drafts.keys()) {
      const n = nodes.get(k);
      const before = previousNodes?.get(k);
      if (!n || n.kind !== "item" || n.value.type !== "field" && n.value.type !== "composer" && n.value.type !== "segmented"
        || before?.kind !== "item" || before.value.type !== n.value.type || !("name" in before.value) || before.value.name !== n.value.name
        || n.value.type === "field" && (before.value as Component & { kind?: string }).kind !== n.value.kind
        || !same((before.value as { value?: string }).value, n.value.value)) this.drafts.delete(k);
    }
    for (const k of this.local.keys()) {
      const n = nodes.get(k), old = previousNodes?.get(k);
      if (!n || !old || n.kind !== old.kind || n.kind === "item" && old.kind === "item" &&
        (n.value.type !== old.value.type || (n.value.type === "diff" && old.value.type === "diff" && (n.value.path !== old.value.path || n.value.revision !== old.value.revision)))) this.local.delete(k);
    }
    if (this.state.snapshot && this.state.snapshot.epoch !== snapshot.epoch) {
      const rootLocal = this.local.get(snapshot.root_key);
      if (object(rootLocal) && has(rootLocal, "confirming")) { const next = { ...rootLocal }; delete next.confirming; this.local.set(snapshot.root_key, freeze(next)); }
    }
    for (const k of this.observedMore.keys()) {
      const n = nodes.get(k);
      if (n?.kind !== "section" || !n.value.more) this.observedMore.delete(k);
    }
    this.publish({ snapshot, display: freeze(displayView(this.state.display, snapshot.view)) });
  }
  /** Drafts and native local state are scoped to this page and stable node keys, never display array positions. */
  setDraft(k: string, value: string) {
    const n = this.state.snapshot && indexTree(this.state.snapshot).get(k);
    if (!n || n.kind !== "item" || n.value.type !== "field" && n.value.type !== "composer" && n.value.type !== "segmented") fail("Missing draft node");
    if (n.value.type === "field" && n.value.kind === "secret") fail("Secret draft forbidden");
    if (bytes(value) > STREAM_LIMITS.request) fail("Draft limit");
    this.drafts.set(k, value);
    this.publish({});
  }
  getDraft(k: string): string | undefined { return this.drafts.get(k); }
  setLocal(k: string, value: unknown) {
    if (!this.state.snapshot || !indexTree(this.state.snapshot).has(k)) fail("Missing local node");
    this.local.set(k, freeze(decode(value, STREAM_LIMITS.request)));
    this.publish({});
  }
  getLocal<T>(k: string): T | undefined { return this.local.get(k) as T | undefined; }
  /** An observer may attempt an unchanged More callback only once, even across native remount/recovery.
   * Explicit presses are separate; observing never automatically retries an uncertain callback. */
  observeMore(k: string): boolean {
    if (this.state.phase !== "ready" || !this.visible || !this.connected || this.awaitingSnapshot) return false;
    const n = this.state.snapshot && indexTree(this.state.snapshot).get(k);
    if (n?.kind !== "section" || !n.value.more || same(this.observedMore.get(k), n.value.more)) return false;
    this.observedMore.set(k, n.value.more);
    return true;
  }
  callback(k: string, action: string, event: string): Action | undefined {
    return this.state.snapshot ? nativeStreamSlots(this.state.snapshot).find((x) => x.key === k && x.action.id === action && x.event === event)?.action : undefined;
  }
  private validateEvent(e: UIStreamEvent) {
    schema("UIStreamEvent", e);
    const s = this.state.snapshot;
    if (!s || e.session !== s.session || e.epoch !== s.epoch || !this.callback(e.key, e.action, e.event)) fail("Stale/locality-mismatched callback");
    // No secret inputs, including submit maps and action-provided values.
    const secretNames = s.view.sections.flatMap((x) => x.items).flatMap((x) => x.type === "field" && x.kind === "secret" ? [x.name] : []);
    if (secretNames.length && (secretNames.some((n) => object(e.value) && has(e.value, n)) || indexTree(s).get(e.key)?.kind === "item" && secretNames.includes((indexTree(s).get(e.key)!.value as { name?: string }).name ?? ""))) fail("Secret interaction forbidden");
  }
  /** Explicit display fence; confirmations and delayed native controls may not borrow a new tree. */
  async eventDisplayed(displayed: UIStreamSnapshot, k: string, action: string, event: "press" | "change" | "submit", value: UIStreamValue): Promise<UIStreamEvent | null> {
    if (displayed !== this.state.snapshot || this.state.pending) throw new Error("Displayed version changed or control busy");
    const request = this.eventRequest(displayed, k, action, event, value);
    if (this.state.phase !== "ready" || !this.visible || !this.connected || this.awaitingSnapshot) throw new Error("Stream not ready");
    const accepted = await this.enqueue(request);
    return accepted === false ? null : request.interaction;
  }
  private eventRequest(s: UIStreamSnapshot, k: string, action: string, event: "press" | "change" | "submit", value: UIStreamValue): UIStreamEventRequest {
    return freeze(decode({ request: "event", protocol: 1, request_id: this.id(), interaction: { protocol: 1, session: s.session, epoch: s.epoch, version: s.version, event_id: this.id(), key: k, action, event, value } }, STREAM_LIMITS.request) as UIStreamEventRequest);
  }
  event(k: string, action: string, event: "press" | "change" | "submit", value: UIStreamValue): Promise<void> {
    const s = this.state.snapshot;
    if (!s || this.state.phase !== "ready" || !this.visible || !this.connected || this.awaitingSnapshot) return Promise.reject(new Error("Stream not ready"));
    try { return this.enqueue(this.eventRequest(s, k, action, event, value)).then(() => {}); }
    catch (error) { return Promise.reject(error); }
  }
  /** Explicit receipt replay preserves the event fence/value, but uses fresh request correlation.
   * It may follow an advanced recovery snapshot; it must never borrow the new tree's callback. */
  retryEvent(eventID: string): Promise<void> {
    const request = this.uncertain.get(eventID), snapshot = this.state.snapshot;
    if (!request || this.state.phase !== "ready" || !this.visible || !this.connected || request.interaction.session !== snapshot?.session || request.interaction.epoch !== snapshot.epoch) return Promise.reject(new Error("No retryable event"));
    return this.enqueue({ ...request, request_id: this.id() }, true).then(() => { this.uncertain.delete(eventID); });
  }
  private enqueue(request: UIStreamEventRequest, replay = false): Promise<void | boolean> {
    try {
      if (!replay) this.validateEvent(request.interaction);
      schema("UIStreamRequest", decode(request, STREAM_LIMITS.request));
      request = freeze(decode(request, STREAM_LIMITS.request) as UIStreamEventRequest);
      if (this.queue.length + (this.inFlight ? 1 : 0) + this.controls >= STREAM_LIMITS.pending) { this.recover("Pending event limit"); fail("Pending event limit"); }
    } catch (e) { return Promise.reject(e); }
    return new Promise<void | boolean>((resolve, reject) => { this.queue.push({ request, replay, resolve, reject }); this.publish({}); void this.pump(); });
  }
  private async pump() {
    if (this.inFlight || this.state.phase !== "ready" || this.awaitingSnapshot || !this.connected || !this.visible) return;
    const next = this.queue.shift();
    if (!next) return;
    this.inFlight = true;
    this.active = next;
    const mine = this.generation;
    let attempted = false;
    this.publish({});
    try {
      if (!next.replay) {
        this.validateEvent(next.request.interaction);
        if (next.request.interaction.version !== this.state.snapshot?.version) fail("Queued callback version changed");
      }
      attempted = true;
      next.resolve(await this.request(next.request));
    } catch (e) {
      next.reject(e instanceof Error ? e : new Error(String(e)));
      const current = this.getState().snapshot;
      if (mine === this.generation && this.getState().phase !== "closed" && current?.session === next.request.interaction.session && current.epoch === next.request.interaction.epoch) {
        if (attempted && this.uncertain.size < STREAM_LIMITS.pending) this.uncertain.set(next.request.interaction.event_id, next.request);
        this.recover("Interaction delivery uncertain; snapshot required");
      }
    } finally {
      if (mine === this.generation) {
        this.inFlight = false;
        this.active = undefined;
        this.publish({}); void this.pump();
      }
    }
  }
  private cancelQueued(why: string) { for (const p of this.queue.splice(0)) p.reject(new Error(why)); }
  setVisible(visible: boolean) {
    if (this.state.phase === "closed" || this.visible === visible) return;
    this.visible = visible;
    this.awaitingSnapshot = true;
    this.cancelQueued("Visibility changed");
    const s = this.state.snapshot;
    if (!visible) {
      this.publish({ phase: "suspended" });
      if (s && this.connected) this.control({ request: "suspend", protocol: 1, request_id: this.id(), session: s.session, epoch: s.epoch });
    } else if (this.connected) this.refreshConnection();
  }
  setConnected(connected: boolean) {
    if (this.state.phase === "closed" || this.connected === connected) return;
    this.connected = connected;
    this.awaitingSnapshot = true;
    this.pendingRead = false;
    this.cancelQueued("Connection changed");
    if (!connected) this.publish({ phase: "disconnected" });
    else if (this.visible) this.refreshConnection();
  }
  private refreshConnection() {
    const s = this.state.snapshot;
    if (!s) {
      this.publish({ phase: "cold" });
      // An uncertain open reuses its request ID; the authenticated manager correlates/deduplicates it.
      if (this.openingRequest) { this.publish({ phase: "opening" }); this.control(this.openingRequest); }
      else this.open();
    } else {
      this.pendingRead = true;
      this.publish({ phase: "recovering" });
      this.control({ request: "resume", protocol: 1, request_id: this.id(), session: s.session, epoch: s.epoch, version: s.version });
    }
  }
  /** Generation disposal is explicit and routed by the caller; no status/envelope fields are invented here. */
  close() {
    if (this.state.phase === "closed") return;
    const s = this.state.snapshot;
    this.generation++;
    this.active?.reject(new Error("Session closed"));
    this.active = undefined;
    this.inFlight = false;
    this.deferredControl = false;
    this.cancelQueued("Session closed");
    this.drafts.clear(); this.local.clear(); this.uncertain.clear(); this.retiredEpochs.clear(); this.observedMore.clear();
    this.publish({ phase: "closed", snapshot: null, display: null });
    if (s && this.connected) void this.request({ request: "close", protocol: 1, request_id: this.id(), session: s.session, epoch: s.epoch }).catch(() => {});
  }
}

/** Explicit available-adapter guard: no fallback path is changed or advertised without an adapter. */
export class ExtensionUIStreamPages {
  private pages = new Map<string, ExtensionUIStreamSession>();
  available(options: StreamPageOptions | undefined): ExtensionUIStreamSession | undefined {
    if (!options) return undefined;
    const k = streamPageKey(options.identity);
    const existing = this.pages.get(k);
    if (existing && existing.getState().phase !== "closed") return existing;
    if (existing) this.pages.delete(k);
    if (this.pages.size >= 8) fail("Local page session limit");
    const session = new ExtensionUIStreamSession(options);
    this.pages.set(k, session);
    return session;
  }
  close(identity: StreamPageIdentity) { const k = streamPageKey(identity); this.pages.get(k)?.close(); this.pages.delete(k); }
  closeAll() { for (const s of this.pages.values()) s.close(); this.pages.clear(); }
}
