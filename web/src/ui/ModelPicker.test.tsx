import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ModelPicker } from "./ModelPicker";

function render(model: string, name?: string) {
  return renderToStaticMarkup(<ModelPicker
    models={{ status: "ready", list: { agent: "claude", models: name === undefined ? [] : [{ id: model, name }] } }}
    model={model} open custom query="" search={{ current: null }}
    onToggle={() => {}} onQuery={() => {}} onPick={() => {}} onReload={() => {}} failedNote=""
  />);
}

describe("模型选择项的显示名称", () => {
  it("formats a listed or unlisted GPT id and preserves explicit aliases", () => {
    expect(render("gpt-6.1-sol", "gpt-6.1-sol")).toContain("6.1 Sol");
    expect(render("gpt-6.1-sol")).toContain("6.1 Sol");
    expect(render("gpt-6.1-sol", "我的模型")).toContain("我的模型");
  });

  it("keeps non-GPT and unknown ids; an empty selection still says 默认", () => {
    expect(render("model-x")).toContain("model-x");
    expect(render("gpt-")).toContain("gpt-");
    expect(render("")).toContain("默认");
  });
});
