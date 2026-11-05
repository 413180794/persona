// One chunk of a streaming reply (ported from cindy's StreamingMarkdownChunk): memo keeps a sealed chunk's parse and
// DOM, so only the growing last one goes through Markdown again. With `fade` its words fade in on the message's
// timeline; `plain` (a block fading in once): the words already in are plain text.
import { memo, useMemo } from "react";
import ReactMarkdown, { type Components, type UrlTransform } from "react-markdown";
import type { PluggableList } from "unified";
import { FadeListItem, FadeSpan } from "./FadeSpan";
import { chunkState, rehypeStreamWordFade, type WordFadeState } from "./wordFade";

export const StreamChunk = memo(function StreamChunk({
  start,
  content,
  remarkPlugins,
  rehypePlugins,
  components,
  urlTransform,
  fade,
  plain = false,
  wholeDocument,
}: {
  start: number;
  content: string;
  remarkPlugins: PluggableList;
  rehypePlugins: PluggableList;
  components: Components;
  urlTransform: UrlTransform;
  fade: WordFadeState | null;
  plain?: boolean;
  wholeDocument: boolean;
}) {
  const state = fade ? chunkState(fade, String(start), wholeDocument) : null;
  const plugins = useMemo<PluggableList>(() => (state ? [...rehypePlugins, [rehypeStreamWordFade, state, plain]] : rehypePlugins), [rehypePlugins, state, plain]);
  const fadeComponents = useMemo<Components>(
    () =>
      state
        ? {
            ...components,
            span: (props) => <FadeSpan {...props} fade={state} />,
            li: (props) => <FadeListItem {...props} fade={state} />,
          }
        : components,
    [components, state],
  );
  return (
    <ReactMarkdown remarkPlugins={remarkPlugins} rehypePlugins={plugins} components={fadeComponents} urlTransform={urlTransform}>
      {content}
    </ReactMarkdown>
  );
});
