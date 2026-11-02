// The DSH runtime (docs/dsh.md): DSH's sessions in Jarvis through the runtime SDK (docs/runtimes.md). Only what is
// DSH's own lives here — listing and paging through sessionController, mapping its session events (map.js), and
// putting its approval / question waterfalls in front of the phone; the SDK does the connection, watching, batching,
// live text, asking and the who-answers-first policy. Everything DSH-specific comes in through `deps`, so the tests
// drive it with fakes; index.js wires the real Cordis services.

import { randomUUID } from 'node:crypto'
import { basename } from 'node:path'
import { createRuntime, interactionId, RpcError, summaryOf } from '../../runtime-sdk/index.js'
import { attachmentOf, dshSeq, eventItems, itemsOf, jarvisSeq, lastText, streamText, toolInput, toolName } from './map.js'

export const RUNTIME_VERSION = '0.2.0'

const DAY = 24 * 60 * 60 * 1000

/**
 * @typedef {object} DshDeps
 * @property {object} controller - DSH sessionController: list / page / projections / prompt / cancel / create / rename / attachment.
 * @property {(event: string, listener: Function, options?: object) => () => void} on - ctx.on.
 * @property {{ answer(agent: object, callId: string, answer: object): boolean } | undefined} [userQuestions]
 * @property {() => boolean} [hasLiveClient] - whether a DSH Web GUI is connected.
 * @property {(msg: string) => void} [log]
 * @property {(path: string) => import('node:net').Socket} [connect] - tests.
 */

/**
 * @param {DshDeps} deps
 * @param {{ socketPath?: string, interactions?: 'auto'|'phone'|'browser', listDays?: number, maxSessions?: number,
 *   pushDelayMs?: number, retryMs?: number }} [options]
 */
export function createDshRuntime(deps, options = {}) {
  const listDays = options.listDays ?? 7
  const maxSessions = options.maxSessions ?? 80
  const log = deps.log ?? (() => {})
  const known = new Map() // session id -> { lastSeq (DSH), lastText, running }
  const keep = new Set() // blank sessions started from the phone: listed before their first prompt
  const calls = new Map() // session id -> Map(callId -> { name, args }): recent tool calls, for approval cards
  const texts = new Map() // session id -> the assistant text being generated
  const disposers = []

  const state = (id) => {
    let s = known.get(id)
    if (!s) {
      s = { lastSeq: -1, lastText: '', running: false }
      known.set(id, s)
    }
    return s
  }
  const history = (id) => `dsh:${id}`
  const withTimeout = async (ms, fn) => {
    const ac = new AbortController()
    const timer = setTimeout(() => ac.abort(), ms)
    try {
      return await fn(ac.signal)
    } finally {
      clearTimeout(timer)
    }
  }

  // ---- the driver ----

  async function listSessions() {
    const { items } = await withTimeout(20_000, (signal) => deps.controller.list({}, signal))
    const since = Date.now() - listDays * DAY
    const rows = []
    for (const s of items ?? []) {
      const id = String(s.sessionId)
      if (s.origin === 'subagent') continue
      const st = known.get(id)
      const running = s.running === true || st?.running === true
      const waiting = activeQuestions(s) > 0 // asked in the Web GUI; cards on the phone the SDK marks itself
      if (s.blank && !keep.has(id)) continue
      if (!running && !waiting && !keep.has(id) && (s.updatedAt ?? 0) < since) continue
      rows.push({ s, id, running, waiting })
    }
    rows.sort((a, b) => (b.s.updatedAt ?? 0) - (a.s.updatedAt ?? 0))
    return rows.slice(0, maxSessions).map(({ s, id, running, waiting }) => {
      const st = known.get(id)
      const hints = s.projections
      const title = typeof hints?.values?.title === 'string' ? hints.values.title : ''
      const seq = Math.max(st?.lastSeq ?? -1, typeof hints?.asOfSeq === 'number' ? hints.asOfSeq : -1)
      const row = {
        session_id: id,
        title: title || (s.cwd ? basename(s.cwd) : ''),
        cwd: s.cwd ?? '',
        status: waiting ? 'blocked' : running ? 'working' : 'done',
        updated_at: new Date(s.updatedAt ?? Date.now()).toISOString(),
      }
      if (s.cwd) {
        row.space_id = s.cwd
        row.space_name = basename(s.cwd)
      }
      if (seq >= 0) row.rev = jarvisSeq(seq)
      if (st?.lastText) row.last_text = st.lastText
      return row
    })
  }

  function activeQuestions(summary) {
    const q = summary?.projections?.values?.userQuestions
    return Array.isArray(q?.active) ? q.active.length : 0
  }

  async function cursorOf(sessionId, signal) {
    const base = await deps.controller.projections({ sessionId }, signal)
    if (base === null || base === undefined) throw new RpcError(`DSH 里没有会话 ${sessionId}`, 'not_found')
    return base.asOfSeq
  }

  /** Events of a backwards page (oldest first), with has_more. */
  async function pageEvents(sessionId, throughSeq, beforeSeq, maxMessages, turnWindow, signal) {
    if (throughSeq < 0) return { events: [], hasMore: false }
    const req = { address: { kind: 'session', sessionId }, throughSeq, maxMessages }
    if (beforeSeq !== undefined) req.beforeSeq = beforeSeq
    if (turnWindow) req.turnWindow = turnWindow
    const page = await deps.controller.page(req, signal)
    const events = (page.records ?? []).filter((r) => r?.type === 'event').map((r) => r.event)
    return { events, hasMore: page.hasMore === true }
  }

  /** Events with DSH seq > afterSeq up to throughSeq, oldest first; reached false when they go back too far. */
  async function eventsAfter(sessionId, afterSeq, throughSeq, signal) {
    let events = []
    let before
    for (let i = 0; i < 10; i++) {
      const page = await pageEvents(sessionId, throughSeq, before, 200, undefined, signal)
      events = page.events.concat(events)
      if (!page.hasMore || page.events.length === 0 || page.events[0].seq <= afterSeq + 1) {
        return { events: events.filter((e) => e.seq > afterSeq), reached: true }
      }
      before = page.events[0].seq
    }
    return { events: events.filter((e) => e.seq > afterSeq), reached: false }
  }

  function items(params) {
    const sessionId = params.session_id
    return withTimeout(25_000, async (signal) => {
      const cursor = await cursorOf(sessionId, signal)
      const top = jarvisSeq(cursor)
      if (typeof params.through_seq === 'number' && params.through_seq > 0) {
        // A fold being opened: the events of [from_seq, through_seq].
        const from = dshSeq(params.from_seq ?? 1)
        const through = Math.min(dshSeq(params.through_seq), cursor)
        const { events } = await eventsAfter(sessionId, from - 1, through, signal)
        return { items: itemsOf(events.filter((e) => e.seq <= through)), more: false }
      }
      if (params.after_seq !== undefined && params.after_seq !== null) {
        const after = dshSeq(params.after_seq)
        if (after >= cursor) return { items: [], has_more: false, more_after: false, history: history(sessionId), from_seq: params.after_seq, to_seq: Math.max(top, params.after_seq) }
        const { events, reached } = await eventsAfter(sessionId, after, cursor, signal)
        const out = []
        let to = params.after_seq
        let cut = false
        for (const e of events) {
          const its = eventItems(e)
          if (out.length > 0 && out.length + its.length > 500) {
            cut = true
            break
          }
          out.push(...its)
          to = jarvisSeq(e.seq)
        }
        if (!cut) to = top
        const from = reached ? params.after_seq : jarvisSeq(events[0]?.seq ?? cursor) - 1
        return { items: out, has_more: false, more_after: cut, history: history(sessionId), from_seq: from, to_seq: to }
      }
      const before = params.before_seq > 0 ? dshSeq(params.before_seq) : undefined
      const turns = params.turns > 0 ? Math.min(params.turns, 10) : 0
      const limit = params.limit > 0 ? Math.min(params.limit, 500) : 80
      const window = turns > 0 ? { minMessages: 1, minTurns: turns } : undefined
      const { events, hasMore } = await pageEvents(sessionId, cursor, before, turns > 0 ? 400 : limit, window, signal)
      const last = events.at(-1)
      return { items: itemsOf(events), has_more: hasMore, history: history(sessionId), to_seq: before === undefined ? top : last ? jarvisSeq(last.seq) : 0 }
    })
  }

  async function eventAt(sessionId, seq, signal) {
    const page = await pageEvents(sessionId, await cursorOf(sessionId, signal), seq + 1, 1, undefined, signal)
    return page.events.find((e) => e.seq === seq)
  }

  function splitItemID(itemID) {
    const [seqText, idxText] = String(itemID).split('#')
    const seq = Number(seqText)
    const idx = Number(idxText)
    if (!Number.isSafeInteger(seq) || !Number.isSafeInteger(idx) || seq < 1) throw new RpcError(`bad item id ${itemID}`, 'bad_request')
    return { seq: dshSeq(seq), idx }
  }

  function image(params) {
    const { seq, idx } = splitItemID(params.item_id)
    return withTimeout(25_000, async (signal) => {
      const event = await eventAt(params.session_id, seq, signal)
      // A tool result's images sit after its text item: block i is item i + 1.
      const attachmentId = attachmentOf(event, event?.type === 'tool/result' ? idx - 1 : idx)
      if (!attachmentId) throw new RpcError(`no image ${params.item_id}`, 'not_found')
      const res = await deps.controller.attachment({ sessionId: params.session_id, attachmentId })
      return { mime: res.attachment?.mediaType ?? 'image/png', data: res.data }
    })
  }

  function item(params) {
    const { seq } = splitItemID(params.item_id)
    return withTimeout(25_000, async (signal) => {
      const event = await eventAt(params.session_id, seq, signal)
      const it = eventItems(event, { full: true }).find((x) => x.id === params.item_id)
      if (!it) throw new RpcError(`no item ${params.item_id}`, 'not_found')
      return it
    })
  }

  async function send(params) {
    const content = []
    if (typeof params.text === 'string' && params.text.trim() !== '') content.push({ type: 'text', text: params.text })
    for (const img of params.images ?? []) {
      if (img?.data) content.push({ type: 'image', mediaType: img.mime || 'image/jpeg', data: img.data, name: 'phone' })
    }
    if (content.length === 0) throw new RpcError('消息是空的', 'bad_request')
    // Queued while idle (a new turn), steered while working (into this turn, like Claude's queued input).
    const running = known.get(params.session_id)?.running === true
    await deps.controller.prompt({
      requestId: params.client_msg_id || randomUUID(), // DSH dedupes on it: a resend is not a second prompt
      sessionId: params.session_id,
      mode: running ? 'steer' : 'queue',
      content,
    }, AbortSignal.timeout(25_000))
  }

  async function start(params) {
    if (!params.cwd) throw new RpcError('要选一个目录', 'bad_request')
    const res = await deps.controller.create({ cwd: params.cwd })
    const id = String(res.sessionId)
    keep.add(id)
    if (typeof params.prompt === 'string' && params.prompt.trim() !== '') await send({ session_id: id, text: params.prompt })
    return { session: { session_id: id, cwd: params.cwd, title: basename(params.cwd), status: 'idle',
      space_id: params.cwd, space_name: basename(params.cwd), updated_at: new Date().toISOString() } }
  }

  const rt = createRuntime({
    listSessions,
    items,
    cursor: (sessionId) => withTimeout(25_000, async (signal) => jarvisSeq(await cursorOf(sessionId, signal))),
    image,
    item,
    send,
    interrupt: (params) => deps.controller.cancel({ sessionId: params.session_id }),
    start,
    rename: (params) => deps.controller.rename({ sessionId: params.session_id, title: params.title }),
    hasLocalClient: typeof deps.hasLiveClient === 'function' ? deps.hasLiveClient : undefined,
  }, {
    runtime: 'dsh',
    name: 'DSH',
    version: RUNTIME_VERSION,
    launcher: 'dsh',
    launcherModel: 'DeepSeek Harness',
    socketPath: options.socketPath,
    answerFirst: options.interactions === 'browser' ? 'local' : options.interactions ?? 'auto',
    log,
    connect: deps.connect,
    pushDelayMs: options.pushDelayMs,
    retryMs: options.retryMs,
  })

  // ---- DSH's events ----

  function onSessionEvent(session, event) {
    const id = String(session?.id ?? '')
    if (!id || typeof event?.seq !== 'number') return
    const st = state(id)
    if (event.seq > st.lastSeq) st.lastSeq = event.seq
    if (event.type === 'assistant/message') {
      const t = lastText([event])
      if (t) st.lastText = t
      rememberCalls(id, event)
    } else if (event.type === 'turn/start') {
      st.running = true
      rt.sessionsChanged()
    } else if (event.type === 'turn/end') {
      st.running = false
      rt.sessionsChanged()
    }
    // Every event moves the seq, even one the chat does not show.
    rt.append(id, { items: rt.watching(id) ? eventItems(event) : [], fromSeq: event.seq, toSeq: jarvisSeq(event.seq), history: history(id) })
  }

  function rememberCalls(id, event) {
    let m = calls.get(id)
    if (!m) {
      m = new Map()
      calls.set(id, m)
    }
    for (const b of event.data?.message?.content ?? []) {
      if (b?.type === 'tool-call' && b.id) m.set(b.id, { name: b.name, args: b.arguments })
    }
    while (m.size > 64) m.delete(m.keys().next().value)
  }

  function onAssistantStream({ agent, frame }) {
    const id = String(agent?.session?.id ?? agent?.id ?? '')
    if (!id) return
    if (frame?.type === 'start') {
      texts.set(id, '')
      return
    }
    if (frame?.type === 'end') {
      texts.delete(id)
      rt.streamEnd(id)
      return
    }
    const add = streamText(frame)
    if (!add || !rt.watching(id)) return
    const text = (texts.get(id) ?? '') + add
    texts.set(id, text)
    rt.stream(id, text)
  }

  async function onApproval(req, next) {
    const agent = req?.agent
    const sessionId = String(agent?.session?.id ?? agent?.id ?? '')
    if (!sessionId || !rt.phoneFirst(sessionId)) return next()
    const call = req.callId ? calls.get(sessionId)?.get(req.callId) : undefined
    const name = call?.name ?? req.toolName
    const input = call ? toolInput(call.name, call.args) : undefined
    const ix = {
      id: interactionId('dsh', sessionId, 'permission', req.callId ?? randomUUID()),
      kind: 'permission',
      session_id: sessionId,
      tool_name: toolName(name),
      tool_use_id: req.callId,
      summary: summaryOf(name, input), // what it will run; DSH's own words on why go in reason, as on a Codex card
    }
    if (typeof req.reason === 'string' && req.reason.trim() !== '') ix.reason = req.reason
    if (input) ix.tool_input = input
    let res
    try {
      res = await rt.ask(ix, { signal: req.signal })
    } catch (err) {
      if (req.signal?.aborted) return 'cancelled'
      throw err
    }
    if (res?.dismiss || typeof res?.allow !== 'boolean') return next() // handed back: the Web GUI asks
    if (!res.allow) steerReason(sessionId, ix.id, ix.tool_name, res.message)
    return res.allow ? 'allowed-once' : 'rejected'
  }

  /**
   * A refusal with words: DSH's approval carries no reason, so the words go into the running turn right after the
   * rejection (steer), as Jarvis does for Codex — the agent reads why, the way Claude reads a hook's deny message.
   */
  function steerReason(sessionId, ixId, tool, message) {
    const text = typeof message === 'string' ? message.trim() : ''
    if (!text) return
    setTimeout(() => {
      deps.controller.prompt({
        requestId: `${ixId}-reason`, // once, however often the card is answered
        sessionId,
        mode: 'steer',
        content: [{ type: 'text', text: `（在手机上拒绝了 ${tool || '这个工具调用'}）${text}` }],
      }, AbortSignal.timeout(25_000)).catch((err) => log(`jarvis: refusal reason for ${sessionId}: ${err.message}`))
    }, 0)
  }

  async function onQuestion(request, next) {
    const agent = request?.agent
    const sessionId = String(agent?.session?.id ?? agent?.id ?? '')
    const questions = request?.questions ?? []
    if (!sessionId || questions.length === 0 || !rt.phoneFirst(sessionId)) return next()
    const callId = request.wait?.callId
    const plan = questions.length === 1 && questions[0].intent?.kind === 'plan-review' ? questions[0] : undefined
    const ix = {
      id: interactionId('dsh', sessionId, plan ? 'plan' : 'question', callId ?? questions.map((q) => q.id).join(',')),
      kind: plan ? 'plan' : 'question',
      session_id: sessionId,
    }
    if (callId) ix.tool_use_id = callId
    if (plan) {
      ix.plan = plan.detail ?? ''
      ix.tool_name = 'ExitPlanMode'
    } else {
      ix.tool_name = 'AskUserQuestion'
      ix.questions = questions.map((q) => ({
        question: q.question,
        header: q.header ?? '',
        multi_select: q.multiSelect === true,
        options: (q.options ?? []).map((o) => ({ label: o.label, description: o.description ?? '' })),
      }))
    }
    // The phone keeps the card past DSH's timed wait: a late answer goes in through userQuestions.answer.
    const phoneAnswer = new AbortController()
    const asked = rt.ask(ix, { signal: phoneAnswer.signal })
    const aborted = new Promise((resolve) => {
      if (!request.signal) return
      if (request.signal.aborted) resolve()
      else request.signal.addEventListener('abort', () => resolve(), { once: true })
    })
    const first = await Promise.race([asked.then((res) => ({ res }), (err) => ({ err })), aborted.then(() => ({ aborted: true }))])
    if (first.err) throw first.err
    if (!first.aborted) {
      if (first.res?.dismiss) return next()
      return { answers: answerOf(questions, plan, first.res) }
    }
    const late = request.wait?.timed === true && callId && deps.userQuestions
    if (!late) {
      phoneAnswer.abort() // takes the card off the phone
      asked.catch(() => {})
      throw request.signal.reason ?? new Error('aborted')
    }
    asked.then((res) => {
      if (res?.dismiss) return
      try {
        deps.userQuestions.answer(agent, callId, { answers: answerOf(questions, plan, res) })
      } catch (err) {
        log(`jarvis: late answer for ${callId}: ${err.message}`)
      }
    }, () => {})
    throw request.signal.reason ?? new Error('aborted')
  }

  return {
    start() {
      const listen = (event, fn, opts = { global: true }) => {
        const dispose = deps.on(event, fn, opts)
        if (typeof dispose === 'function') disposers.push(dispose)
      }
      listen('session/event', onSessionEvent)
      listen('agent/assistant-stream', onAssistantStream)
      listen('api-session/added', () => rt.sessionsChanged())
      listen('api-session/removed', () => rt.sessionsChanged())
      listen('api-session/activity', () => rt.sessionsChanged())
      listen('api-session/status', (sessionId, running) => {
        state(String(sessionId)).running = running === true
        rt.sessionsChanged()
      })
      // First on DSH's answerer chains (the Web GUI registers when a browser connects): see docs/dsh.md.
      listen('approval/request', onApproval, { global: true, prepend: true })
      listen('user-questions/request', onQuestion, { global: true, prepend: true })
      rt.start()
    },
    stop() {
      for (const d of disposers.splice(0)) {
        try {
          d()
        } catch {
          // already gone
        }
      }
      rt.stop()
    },
    get connected() {
      return rt.connected
    },
    pushNow: rt.pushNow,
  }
}

/** The answer batch DSH wants, from the phone's resolution. */
export function answerOf(questions, plan, res) {
  if (plan) {
    const approve = plan.intent.approve
    if (res?.approve === true) return [{ id: plan.id, selected: [approve] }]
    const other = (plan.options ?? []).map((o) => o.label).find((l) => l !== approve)
    const item = { id: plan.id, selected: other ? [other] : [] }
    const feedback = typeof res?.feedback === 'string' ? res.feedback.trim() : ''
    if (feedback) item.custom = feedback
    return [item]
  }
  const answers = res?.answers ?? {}
  return questions.map((q) => {
    const value = typeof answers[q.question] === 'string' ? answers[q.question] : ''
    const labels = new Set((q.options ?? []).map((o) => o.label))
    const parts = q.multiSelect ? value.split(', ') : [value]
    const selected = parts.filter((p) => labels.has(p))
    const item = { id: q.id, selected }
    if (value && selected.length === 0) item.custom = value
    return item
  })
}
