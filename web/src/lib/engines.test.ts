import { describe, expect, it } from "vitest";
import {
  canSaveEngine,
  canSelectEngine,
  currentEngine,
  detectionText,
  engineDetail,
  engineFooter,
  engineName,
  engineOptions,
  engineSaveError,
  KNOWN_ENGINES,
} from "./engines";
import type { Machine } from "./types";

const machine = (extra: Partial<Machine> = {}): Machine => ({ id: "computer", name: "电脑", online: false, ...extra });

describe("engine choice (iOS EngineSelectionModel)", () => {
  it("is herdr until one is chosen, whatever happens to be detected", () => {
    expect(currentEngine(machine())).toBe("herdr");
    expect(currentEngine(machine({ engines: [{ id: "herdr-core", name: "Herdr App", available: true }] }))).toBe("herdr");
    expect(currentEngine(machine({ engine: "herdr-core" }))).toBe("herdr-core");
  });

  it("lists what the node reports, names, notes and future engines included", () => {
    const m = machine({
      online: true,
      engine: "orca",
      engines: [
        { id: "orca", name: "Orca Desktop", available: false, note: "请先更新 Orca" },
        { id: "future", name: "新引擎", available: true },
      ],
    });
    expect(engineOptions(m)).toBe(m.engines);
    expect(engineName("orca", m)).toBe("Orca Desktop");
    expect(engineName("future", m)).toBe("新引擎");
    expect(canSelectEngine("orca", m)).toBe(true);
    expect(canSelectEngine("future", m)).toBe(true);
    m.engines![1].available = false;
    expect(canSelectEngine("future", m)).toBe(false);
  });

  it("lists every known engine when the node reports none (offline, older hubs)", () => {
    const m = machine({ engine: "future" });
    expect(engineOptions(m).map((e) => e.id)).toEqual(["herdr", "herdr-core", "herdr-both", "orca", "paseo"]);
    expect(engineOptions(m).every((e) => !e.available)).toBe(true);
    expect(engineOptions({ engines: [] }).length).toBe(KNOWN_ENGINES.length);
    // known ones stay selectable offline; an unknown one only while reported available
    for (const e of KNOWN_ENGINES) expect(canSelectEngine(e.id, m)).toBe(true);
    expect(canSelectEngine("future", m)).toBe(false);
    expect(engineName("future", m)).toBe("future");
  });

  it("names and describes the known engines like iOS", () => {
    expect(engineName("herdr-both")).toBe("herdr + Herdr App");
    expect(engineDetail("herdr-both")).toBe("终端和桌面 App 的会话都显示");
    expect(engineName("paseo")).toBe("Paseo");
    expect(engineDetail("paseo")).toBe("Paseo 里的 Agent");
    expect(engineDetail("future")).toBeUndefined();
  });

  it("says what the node found", () => {
    const engines = [
      { id: "herdr", name: "herdr", available: true, version: "0.9.5" },
      { id: "herdr-core", name: "Herdr App", available: true },
      { id: "orca", name: "Orca", available: false, version: "1.0" },
    ];
    const on = machine({ online: true, engines });
    expect(detectionText("herdr", on)).toBe("检测到可用 · 0.9.5");
    expect(detectionText("herdr-core", on)).toBe("检测到可用");
    expect(detectionText("orca", on)).toBe("没检测到");
    expect(detectionText("paseo", on)).toBe("暂无检测信息");
    expect(detectionText("herdr", machine({ engines }))).toBe("离线，暂无检测信息");
  });

  it("offers 保存 only for a change the hub can take", () => {
    const m = machine({ engine: "herdr-core" });
    expect(canSaveEngine("herdr-core", m)).toBe(false);
    expect(canSaveEngine("herdr", m)).toBe(true);
    expect(canSaveEngine("future", m)).toBe(false);
    expect(canSaveEngine("herdr", machine())).toBe(false);
  });

  it("explains the choice, offline included, and a failed save", () => {
    expect(engineFooter("herdr", true)).toBe("只显示所选引擎里的会话。");
    expect(engineFooter("orca", false)).toBe("机器离线，选择会在连接后生效。只显示所选引擎里的会话。");
    expect(engineFooter("herdr-both", true)).toContain("从网页新建的会话仍开在 herdr 里");
    expect(engineFooter("herdr-both", false)).toBe("机器离线，选择会在连接后生效。同时显示两个引擎里的会话。");
    expect(engineSaveError("暂时无法保存")).toBe("保存失败：暂时无法保存");
  });
});
