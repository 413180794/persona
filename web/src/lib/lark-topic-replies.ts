// The newest replies of a Feishu topic group's topics, shown on their cards as the Lark gateway does (#2903, iOS
// LarkTopicReplies.swift). A topic's are read when its card comes on screen, `PARALLEL` topics at a time (a screen holds
// a dozen cards, and the hub passes each read on to the Lark gateway), and kept with the reply count they were read at: a topic
// is read again only when its count changes (also while it is being read), or after a read failed: on retry, or when
// its card comes on screen again.
import type { LarkMessage } from "./inbox";

/** The newest replies of one topic, oldest first, and the reply count they were read at. */
export interface TopicPreview {
  count: number;
  replies: LarkMessage[];
  failed?: boolean;
}

/** Topics read at the same time. */
export const PARALLEL = 3;
/** Replies shown on a card. */
export const SHOWN = 2;

/** Replies a topic has: Feishu counts them in two places, and either can lag. */
export function topicReplyCount(topic: LarkMessage): number {
  return Math.max(topic.thread_reply_count ?? 0, topic.reply_count ?? 0);
}

/** The newest `SHOWN` replies of a thread page (its topic left out) and how many replies come before them. */
export function topicPreview(page: LarkMessage[], topic: string, count: number): { replies: LarkMessage[]; earlier: number } {
  const replies = page.filter((m) => m.id !== topic).slice(-SHOWN);
  return { replies, earlier: Math.max(0, count - replies.length) };
}

export type TopicFetch = (id: string, limit: number) => Promise<LarkMessage[]>;

export class LarkTopicReplies {
  private previews = new Map<string, TopicPreview>();
  private waiting: { id: string; count: number }[] = [];
  /** Topics being read, with the count each read is for. */
  private reading = new Map<string, number>();
  /** A newer count asked for while its topic was being read: read again once that read is in. */
  private again = new Map<string, number>();
  private listeners = new Set<() => void>();
  private closed = false;

  constructor(private readonly fetch: TopicFetch) {}

  preview(id: string): TopicPreview | undefined {
    return this.previews.get(id);
  }

  /** For useSyncExternalStore-style redraws: called after any preview changes. */
  subscribe(fn: () => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  /** A card came on screen or its reply count changed: read the topic's newest replies unless known at that count. */
  want(topic: LarkMessage) {
    const count = topicReplyCount(topic);
    if (count <= 0 || this.closed) return;
    const read = this.reading.get(topic.id);
    if (read !== undefined) {
      if (read === count) this.again.delete(topic.id);
      else this.again.set(topic.id, count);
      return;
    }
    const p = this.previews.get(topic.id);
    if (p && p.count === count && !p.failed) return;
    this.waiting = this.waiting.filter((w) => w.id !== topic.id);
    this.waiting.push({ id: topic.id, count });
    this.next();
  }

  /** Retry after a read failed. */
  retry(topic: LarkMessage) {
    const p = this.previews.get(topic.id);
    if (p) this.previews.set(topic.id, { ...p, failed: false, count: -1 });
    this.want(topic);
  }

  /** The card went off screen before its turn came: not read for now (nor again after a read under way). */
  drop(id: string) {
    this.waiting = this.waiting.filter((w) => w.id !== id);
    this.again.delete(id);
  }

  /** The chat is shown (again): reads go ahead. */
  open() {
    this.closed = false;
    this.next();
  }

  /** The chat closed: nothing new starts (reads under way still land, for when it shows again). */
  close() {
    this.closed = true;
    this.waiting = [];
    this.again.clear();
  }

  private next() {
    while (!this.closed && this.reading.size < PARALLEL && this.waiting.length) {
      const { id, count } = this.waiting.shift()!;
      this.reading.set(id, count);
      void this.read(id, count);
    }
  }

  private async read(id: string, count: number) {
    try {
      const page = await this.fetch(id, SHOWN + 1);
      this.previews.set(id, { count, replies: topicPreview(page, id, count).replies });
    } catch {
      this.previews.set(id, { count, replies: this.previews.get(id)?.replies ?? [], failed: true });
    }
    this.reading.delete(id);
    const newer = this.again.get(id);
    if (newer !== undefined) {
      this.again.delete(id);
      this.waiting.push({ id, count: newer });
    }
    for (const fn of this.listeners) fn();
    this.next();
  }
}
