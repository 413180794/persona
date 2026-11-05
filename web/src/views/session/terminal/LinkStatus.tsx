// What sits on the terminal while its screen may be old (iOS TerminalLinkStatus): how long since the last fresh
// screen, when the next try is and why — and a button to try now. Gone as soon as fresh screens arrive again.
import { cx, useNow } from "../../../ui/atoms";
import { Icon } from "../../../ui/icons";
import { linkDetail, linkHeadline } from "../../../lib/terminal";
import type { TerminalFeed } from "./feed";
import { t } from "../../../i18n";

export function LinkStatus({ feed }: { feed: TerminalFeed }) {
  const now = useNow(1000);
  const { lastOK, failure, nextTry } = feed.link();
  return (
    // a solid surface under the tint: the terminal must not show through
    <div role="status" className="pointer-events-auto overflow-hidden rounded-control bg-surface shadow-raised" style={{ animation: "fade-in 160ms ease-out both" }}>
      <div className={cx("flex items-center gap-2 py-1.5 pr-1.5 pl-2.5", failure ? "bg-red-tint text-red" : "bg-orange-tint text-orange")}>
        <Icon name={failure ? "info" : "clock"} size={13} stroke={2.2} className="shrink-0" />
        <div className="min-w-0 flex-1 truncate text-[12px]">
          <span className="font-semibold">{linkHeadline(lastOK, now)}</span>
          <span className="opacity-80"> · {linkDetail(failure, nextTry, now)}</span>
        </div>
        <button
          type="button"
          onClick={() => feed.retry()}
          className="h-6 shrink-0 whitespace-nowrap rounded-chip bg-surface px-2.5 text-[12px] font-medium text-ink shadow-btn transition-transform duration-150 active:scale-[0.96]"
        >
          {t("session-terminal-linkstatus.retry")}
        </button>
      </div>
    </div>
  );
}
