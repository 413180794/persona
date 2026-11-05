// GFM (tables, strikethrough, task lists, autolink literals, footnotes) for rendering only. remark-gfm also registers
// the Markdown serializer (gfmToMarkdown, which pulls in mdast-util-to-markdown and markdown-table), but the web only
// turns Markdown into HTML and never writes mdast back, so this registers just the parser half (#2687).
import { gfmFromMarkdown } from "mdast-util-gfm";
import { gfm } from "micromark-extension-gfm";
import type { Processor } from "unified";

// The two lists remark-parse reads from the processor data (remark-parse declares them on unified's Data; spelled
// out here so this file does not depend on that transitive type augmentation).
type ParseData = {
  micromarkExtensions?: ReturnType<typeof gfm>[];
  fromMarkdownExtensions?: ReturnType<typeof gfmFromMarkdown>[];
};

export default function remarkGfmParse(this: Processor): void {
  const data = this.data() as ParseData;
  (data.micromarkExtensions ??= []).push(gfm());
  (data.fromMarkdownExtensions ??= []).push(gfmFromMarkdown());
}
