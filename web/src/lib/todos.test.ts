import { describe, expect, it } from "vitest";
import type { Machine, Session } from "./types";
import {
  buildTodoAgentGroups,
  formatTodoContext,
  makeSessionLookup,
  mostUrgentAgent,
  sortTodos,
  todoStatus,
  type Todo,
} from "./todos";
import type { AgentMutes } from "./agent-groups";

const machines: Machine[] = [
  { id: "mac-1", name: "MacBook", online: true },
  { id: "server-1", name: "服务器", online: true },
  { id: "mac-off", name: "离线电脑", online: false },
];

const mockSession = (mid: string, sid: string, state: Session["state"], patch: Partial<Session> = {}): Session => ({
  machine_id: mid,
  session_id: sid,
  title: sid,
  cwd: `/code/${sid}`,
  status: state ?? "idle",
  state,
  updated_at: "2026-11-02T10:00:00Z",
  ...patch,
});

describe("待办状态计算 (todoStatus)", () => {
  it("用户点了完成（done_at != null），不论 Agent 状态如何均判定为已完成", () => {
    const s = mockSession("mac-1", "s1", "needs_you");
    const lookup = makeSessionLookup([s]);
    const todo: Todo = {
      id: "t1",
      title: "已完成待办",
      created_at: "2026-11-02T10:00:00Z",
      updated_at: "2026-11-02T10:00:00Z",
      done_at: "2026-11-02T11:00:00Z",
      agents: [{ machine_id: "mac-1", session_id: "s1", linked_at: "2026-11-02T10:00:00Z" }],
    };
    expect(todoStatus(todo, lookup)).toBe("done");
  });

  it("没有挂着的 Agent 时为未开始 (not_started)", () => {
    const todoNoAgents: Todo = {
      id: "t1",
      title: "空待办",
      created_at: "2026-11-02T10:00:00Z",
      updated_at: "2026-11-02T10:00:00Z",
      agents: [],
    };
    expect(todoStatus(todoNoAgents)).toBe("not_started");

    // 只有已 released 的 Agent 也算未开始
    const todoReleased: Todo = {
      ...todoNoAgents,
      agents: [{ machine_id: "mac-1", session_id: "s1", linked_at: "2026-11-02T09:00:00Z", released_at: "2026-11-02T09:30:00Z" }],
    };
    expect(todoStatus(todoReleased)).toBe("not_started");
  });

  it("有挂着的 Agent 需要你，状态为等你 (needs_you)", () => {
    const s1 = mockSession("mac-1", "s1", "needs_you");
    const s2 = mockSession("mac-1", "s2", "working");
    const lookup = makeSessionLookup([s1, s2]);
    const todo: Todo = {
      id: "t1",
      title: "待办",
      created_at: "2026-11-02T10:00:00Z",
      updated_at: "2026-11-02T10:00:00Z",
      agents: [
        { machine_id: "mac-1", session_id: "s1", linked_at: "2026-11-02T10:00:00Z" },
        { machine_id: "mac-1", session_id: "s2", linked_at: "2026-11-02T10:00:00Z" },
      ],
    };
    expect(todoStatus(todo, lookup)).toBe("needs_you");
  });

  it("没有等你，但有挂着的 Agent 完成了未看，状态为待查看 (unread)", () => {
    const s1 = mockSession("mac-1", "s1", "unread");
    const s2 = mockSession("mac-1", "s2", "working");
    const lookup = makeSessionLookup([s1, s2]);
    const todo: Todo = {
      id: "t1",
      title: "待办",
      created_at: "2026-11-02T10:00:00Z",
      updated_at: "2026-11-02T10:00:00Z",
      agents: [
        { machine_id: "mac-1", session_id: "s1", linked_at: "2026-11-02T10:00:00Z" },
        { machine_id: "mac-1", session_id: "s2", linked_at: "2026-11-02T10:00:00Z" },
      ],
    };
    expect(todoStatus(todo, lookup)).toBe("unread");
  });

  it("没有等你和待查看，但有在工作或正在启动，状态为进行中 (working)", () => {
    const s1 = mockSession("mac-1", "s1", "idle");
    const s2 = mockSession("mac-1", "s2", "working");
    const lookup = makeSessionLookup([s1, s2]);
    const todo: Todo = {
      id: "t1",
      title: "待办",
      created_at: "2026-11-02T10:00:00Z",
      updated_at: "2026-11-02T10:00:00Z",
      agents: [
        { machine_id: "mac-1", session_id: "s1", linked_at: "2026-11-02T10:00:00Z" },
        { machine_id: "mac-1", session_id: "s2", linked_at: "2026-11-02T10:00:00Z" },
      ],
    };
    expect(todoStatus(todo, lookup)).toBe("working");

    // starting 会话也算工作/启动中
    const sStarting = mockSession("mac-1", "s3", "idle", { starting: true });
    const lookupStarting = makeSessionLookup([sStarting]);
    const todoStarting: Todo = {
      ...todo,
      agents: [{ machine_id: "mac-1", session_id: "s3", linked_at: "2026-11-02T10:00:00Z" }],
    };
    expect(todoStatus(todoStarting, lookupStarting)).toBe("working");
  });

  it("挂着的 Agent 都停着（已看、未运行、启动失败），状态为待跟进 (follow_up)", () => {
    const sIdle = mockSession("mac-1", "s1", "idle");
    const lookup = makeSessionLookup([sIdle]);
    const todo: Todo = {
      id: "t1",
      title: "待办",
      created_at: "2026-11-02T10:00:00Z",
      updated_at: "2026-11-02T10:00:00Z",
      agents: [
        { machine_id: "mac-1", session_id: "s1", linked_at: "2026-11-02T10:00:00Z" },
        { machine_id: "mac-1", session_id: "s2", linked_at: "2026-11-02T10:00:00Z", start_error: "launch failed" },
      ],
    };
    expect(todoStatus(todo, lookup)).toBe("follow_up");
  });
});

describe("待办列表排序 (sortTodos)", () => {
  it("未完成在上方，按状态顺序：等你 > 待查看 > 进行中 > 待跟进 > 未开始；已完成在底部", () => {
    const sNeeds = mockSession("mac-1", "s-needs", "needs_you");
    const sUnread = mockSession("mac-1", "s-unread", "unread");
    const sWorking = mockSession("mac-1", "s-work", "working");
    const sIdle = mockSession("mac-1", "s-idle", "idle");
    const lookup = makeSessionLookup([sNeeds, sUnread, sWorking, sIdle]);

    const todos: Todo[] = [
      { id: "done-1", title: "已完成1", created_at: "2026-11-02T12:00:00Z", updated_at: "", done_at: "2026-11-02T13:00:00Z" },
      { id: "empty-1", title: "未开始1", created_at: "2026-11-02T10:00:00Z", updated_at: "", agents: [] },
      { id: "work-1", title: "工作中1", created_at: "2026-11-02T10:00:00Z", updated_at: "", agents: [{ machine_id: "mac-1", session_id: "s-work", linked_at: "" }] },
      { id: "needs-1", title: "等你1", created_at: "2026-11-02T10:00:00Z", updated_at: "", agents: [{ machine_id: "mac-1", session_id: "s-needs", linked_at: "" }] },
      { id: "idle-1", title: "待跟进1", created_at: "2026-11-02T10:00:00Z", updated_at: "", agents: [{ machine_id: "mac-1", session_id: "s-idle", linked_at: "" }] },
      { id: "unread-1", title: "待查看1", created_at: "2026-11-02T10:00:00Z", updated_at: "", agents: [{ machine_id: "mac-1", session_id: "s-unread", linked_at: "" }] },
    ];

    const sorted = sortTodos(todos, lookup);
    expect(sorted.map((t) => t.id)).toEqual([
      "needs-1",
      "unread-1",
      "work-1",
      "idle-1",
      "empty-1",
      "done-1",
    ]);
  });

  it("同状态下按创建时间倒序排（新的在前）", () => {
    const sNeeds = mockSession("mac-1", "s-needs", "needs_you");
    const lookup = makeSessionLookup([sNeeds]);

    const todos: Todo[] = [
      { id: "needs-old", title: "较早创建", created_at: "2026-11-02T09:00:00Z", updated_at: "", agents: [{ machine_id: "mac-1", session_id: "s-needs", linked_at: "" }] },
      { id: "needs-new", title: "较晚创建", created_at: "2026-11-02T11:00:00Z", updated_at: "", agents: [{ machine_id: "mac-1", session_id: "s-needs", linked_at: "" }] },
    ];

    const sorted = sortTodos(todos, lookup);
    expect(sorted.map((t) => t.id)).toEqual(["needs-new", "needs-old"]);
  });

  it("同一秒内、小数位数不同的两个时间按真实时间戳解析正确排序 (R5)", () => {
    // 500ms (1位小数) 晚于 450ms (3位小数)，同为未开始状态
    const tOlder: Todo = { id: "older-subsecond", title: "较早", created_at: "2026-11-02T10:00:00.450Z", updated_at: "", agents: [] };
    const tNewer: Todo = { id: "newer-subsecond", title: "较晚", created_at: "2026-11-02T10:00:00.5Z", updated_at: "", agents: [] };

    const sorted = sortTodos([tOlder, tNewer]);
    expect(sorted.map((t) => t.id)).toEqual(["newer-subsecond", "older-subsecond"]);
  });
});

describe("最要紧的 Agent (mostUrgentAgent)", () => {
  it("选出最高优先级的 Agent", () => {
    const sIdle = mockSession("mac-1", "s1", "idle");
    const sNeeds = mockSession("mac-1", "s2", "needs_you", { last_text: "请选择测试框架" });
    const lookup = makeSessionLookup([sIdle, sNeeds]);

    const todo: Todo = {
      id: "t1",
      title: "待办",
      created_at: "",
      updated_at: "",
      agents: [
        { machine_id: "mac-1", session_id: "s1", linked_at: "" },
        { machine_id: "mac-1", session_id: "s2", linked_at: "" },
      ],
    };

    const best = mostUrgentAgent(todo, lookup);
    expect(best?.agent.session_id).toBe("s2");
    expect(best?.status).toBe("needs_you");
    expect(best?.session?.last_text).toBe("请选择测试框架");
  });
});

describe("Agents 侧边栏「按待办」分组 (buildTodoAgentGroups)", () => {
  const mutes: AgentMutes = { spaces: [], sessions: ["mac-1/muted-work"] };

  it("每个挂了 live agent 的未完成待办一段，没挂待办的放最后「没有待办」", () => {
    const sWeatherTests = mockSession("mac-1", "weather-tests", "needs_you", { todo: { id: "todo-weather", title: "天气 App" } });
    const sWeatherWidget = mockSession("mac-1", "weather-widget", "working", { todo: { id: "todo-weather", title: "天气 App" } });
    const sBlog = mockSession("mac-1", "blog-migrate", "unread", { todo: { id: "todo-blog", title: "博客迁移" } });
    const sSolo = mockSession("server-1", "solo-task", "idle"); // 未关联待办

    const all = [sWeatherTests, sWeatherWidget, sBlog, sSolo];

    const todos: Todo[] = [
      {
        id: "todo-weather",
        title: "天气 App 发 1.3 版",
        created_at: "2026-11-02T10:00:00Z",
        updated_at: "",
        agents: [
          { machine_id: "mac-1", session_id: "weather-tests", linked_at: "" },
          { machine_id: "mac-1", session_id: "weather-widget", linked_at: "" },
        ],
      },
      {
        id: "todo-blog",
        title: "博客迁移到 Astro",
        created_at: "2026-11-02T09:00:00Z",
        updated_at: "",
        agents: [{ machine_id: "mac-1", session_id: "blog-migrate", linked_at: "" }],
      },
    ];

    const groups = buildTodoAgentGroups(all, machines, todos, mutes);
    expect(groups.map((g) => g.name)).toEqual(["天气 App 发 1.3 版", "博客迁移到 Astro", "没有待办"]);
    expect(groups[0].todoStatus).toBe("needs_you");
    expect(groups[0].sessions.map((s) => s.session_id)).toEqual(["weather-tests", "weather-widget"]);
    expect(groups[1].todoStatus).toBe("unread");
    expect(groups[1].sessions.map((s) => s.session_id)).toEqual(["blog-migrate"]);
    expect(groups[2].sessions.map((s) => s.session_id)).toEqual(["solo-task"]);
  });

  it("已完成的待办不生成 live 待办组", () => {
    const sDoneTask = mockSession("mac-1", "done-task", "idle", { todo: { id: "todo-done", title: "已完成" } });
    const todos: Todo[] = [
      {
        id: "todo-done",
        title: "已完成待办",
        created_at: "2026-11-02T08:00:00Z",
        updated_at: "",
        done_at: "2026-11-02T12:00:00Z",
        agents: [{ machine_id: "mac-1", session_id: "done-task", linked_at: "" }],
      },
    ];

    const groups = buildTodoAgentGroups([sDoneTask], machines, todos, mutes);
    // 因为 todo-done 已完成，不作为未完成待办生成段，该 session 归入「没有待办」
    expect(groups.map((g) => g.name)).toEqual(["没有待办"]);
    expect(groups[0].sessions.map((s) => s.session_id)).toEqual(["done-task"]);
  });

  it("屏蔽的 Agent 在待办组内降级，但同组的需要你仍然排在最前面", () => {
    const sNeeds = mockSession("mac-1", "s-needs", "needs_you");
    const sMuted = mockSession("mac-1", "muted-work", "working");
    const sNormalWork = mockSession("mac-1", "s-work", "working");

    const todos: Todo[] = [
      {
        id: "todo-1",
        title: "待办 1",
        created_at: "2026-11-02T10:00:00Z",
        updated_at: "",
        agents: [
          { machine_id: "mac-1", session_id: "muted-work", linked_at: "" },
          { machine_id: "mac-1", session_id: "s-needs", linked_at: "" },
          { machine_id: "mac-1", session_id: "s-work", linked_at: "" },
        ],
      },
    ];

    const groups = buildTodoAgentGroups([sMuted, sNeeds, sNormalWork], machines, todos, mutes);
    expect(groups[0].sessions.map((s) => s.session_id)).toEqual([
      "s-needs",     // needs_you (rank 0)
      "s-work",      // normal working (rank 2)
      "muted-work",  // muted working (rank 4)
    ]);
  });
});

describe("格式化上下文标签 (formatTodoContext)", () => {
  it("转义 HTML 特殊字符并拼装标准 jarvis-context", () => {
    const xml = formatTodoContext('天气 "App" <1.3>', "测试重试 & 超时\n周五前完成");
    expect(xml).toBe(
      '<jarvis-context kind="context" title="待办 · 天气 &quot;App&quot; &lt;1.3&gt;" url="">测试重试 & 超时\n周五前完成</jarvis-context>',
    );
  });
});
