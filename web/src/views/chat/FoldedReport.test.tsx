// @vitest-environment jsdom
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it } from "vitest";
import { FoldedReport, isHandbackText, reportLine } from "./FoldedReport";

// A subagent's report starts folded to one line, its first heading or first words, with "Expand"; opening it is kept
// per card in the browser (#6381).
describe("FoldedReport", () => {
  afterEach(() => localStorage.clear());

  it("folds to the first heading, without its Markdown", () => {
    expect(reportLine("Some intro\n\n## Cursor **fallback**: done\n\nDetails")).toBe("Cursor fallback: done");
  });

  it("folds to the first line when there is no heading, cut to 80 characters", () => {
    const long = "字".repeat(100);
    expect(reportLine(`\n  ${long}\nmore`)).toBe("字".repeat(79) + "…");
    expect(reportLine("All tests pass.\nSecond line")).toBe("All tests pass.");
  });

  it("skips Claude Code's hand-back frame", () => {
    const text = "[Subagent hand-back] The text below is the final report of a subagent. The report follows:\n  ## Summary\n  All good.";
    expect(isHandbackText(text)).toBe(true);
    expect(reportLine(text)).toBe("Summary");
    expect(isHandbackText("Please look at [Subagent hand-back]")).toBe(false);
  });

  it("shows one line and Expand by default, the whole report once opened", () => {
    const folded = renderToStaticMarkup(
      <FoldedReport id="task:t1" text={"## Done\nThe whole report"}>
        <p>The whole report</p>
      </FoldedReport>,
    );
    expect(folded).toContain('data-report="folded"');
    expect(folded).toContain("Done");
    expect(folded).toContain("展开");
    expect(folded).not.toContain("<p>The whole report</p>");

    localStorage.setItem("report:task:t1", "1");
    const open = renderToStaticMarkup(
      <FoldedReport id="task:t1" text={"## Done\nThe whole report"}>
        <p>The whole report</p>
      </FoldedReport>,
    );
    expect(open).toContain('data-report="open"');
    expect(open).toContain("<p>The whole report</p>");
    expect(open).toContain("收起");
  });

  it("keeps each card's choice apart", () => {
    localStorage.setItem("report:task:t1", "1");
    const other = renderToStaticMarkup(
      <FoldedReport id="task:t2" text="Report">
        <p>full</p>
      </FoldedReport>,
    );
    expect(other).toContain('data-report="folded"');
  });

  it("draws nothing for an empty report", () => {
    expect(renderToStaticMarkup(<FoldedReport id="x" text={"  \n "}>{"full"}</FoldedReport>)).toBe("");
  });
});
