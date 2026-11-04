// A ```persona-view or ```persona-card block in an assistant's reply, drawn with the extensions' components
// (lib/assistant-view.ts, views/extensions/ViewBody.tsx): the same look as an extension's page, nothing that acts. A
// block that does not read yet (still being written) or at all stays the code block it is.
import { useMemo } from "react";
import { assistantView } from "../lib/assistant-view";
import { INERT, ViewSections } from "../views/extensions/ViewBody";

export function AssistantCard({ lang, code, fallback }: { lang: string; code: string; fallback: React.ReactNode }) {
  const view = useMemo(() => assistantView(lang, code), [lang, code]);
  if (!view) return <>{fallback}</>;
  return (
    <div className="card-ui my-3 flex flex-col gap-3 text-[13px] leading-normal" data-assistant-card={lang}>
      {view.title && <p className="px-1 text-[13px] font-semibold text-ink">{view.title}</p>}
      <ViewSections view={view} {...INERT} />
    </div>
  );
}

/** The fenced blocks drawn as cards. */
export const CARD_LANGUAGES = ["persona-view", "persona-card"];
