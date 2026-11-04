// The plugins' places among the web's own pages (docs/plugins.md「UI 插槽」): a nav_page's full page (/plugin/<id>/<ui>,
// its entry is on the sidebar), the cards on 总览 (overview_card) and the sections on a machine's page
// (machine_section). Each draws the plugin's declarative View with PluginUI; the list comes from lib/plugin-slots.ts.
import { useMemo, useState } from "react";
import { uiIcon, type UIIcon, type UIPlace, type UITone } from "../../lib/plugin-ui";
import { usePluginPlace, usePluginSlot } from "../../lib/plugin-slots";
import { href, navigate, onLinkClick, type Route } from "../../lib/router";
import { cx, EASE, IconButton } from "../../ui/atoms";
import { Icon, type IconName } from "../../ui/icons";
import { PluginUI } from "../settings/PluginView";

/** A nav_page's icon from the fixed vocabulary, as one of the web's icons (unknown: the plugin's). */
const NAV_ICON: Record<UIIcon, IconName> = { plugin: "plugin", chart: "chart", gauge: "gauge", list: "list", bell: "bell", doc: "read", calendar: "calendar", money: "money" };
export const navIcon = (icon?: string): IconName => NAV_ICON[uiIcon(icon)];

/** A badge's tone as the sidebar's badge colors. */
export const BADGE_TONE: Record<UITone, "red" | "orange" | "green" | "accent"> = { danger: "red", caution: "orange", positive: "green", neutral: "accent" };

/** Where a plugin's page opens: its own route (a nav_page, or a plugin_page a card opens). */
export const pageRoute = (plugin: string, ui: string): Route => ({ name: "plugin", plugin, ui });

/** A plugin's page, full size (a nav_page opened from the sidebar, or what a card opens). */
export function PluginNavPage({ plugin, ui, onToggleSidebar }: { plugin: string; ui: string; onToggleSidebar: () => void }) {
  const place = usePluginPlace(plugin, ui);
  const [n, setN] = useState(0);
  const title = place?.title || ui;
  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center gap-2 border-b border-line px-3 sm:px-4">
        <IconButton icon="sidebar" label="侧边栏" onClick={onToggleSidebar} className="lg:hidden" />
        <span className="flex size-7 shrink-0 items-center justify-center rounded-[8px] bg-field text-ink-2">
          <Icon name={navIcon(place?.icon)} size={14} />
        </span>
        <h1 className="min-w-0 truncate text-[14px] font-semibold text-ink">{title}</h1>
        {place?.plugin_name && <span className="hidden min-w-0 truncate text-[12px] text-ink-3 sm:inline">{place.plugin_name}</span>}
        <span className="flex-1" />
        <IconButton icon="retry" label="重新画" onClick={() => setN((x) => x + 1)} />
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-[880px] flex-col px-4 py-6 sm:px-8">
          <PluginUI key={`${plugin}/${ui}/${n}`} plugin={plugin} ui={ui} title={title} bare />
        </div>
      </div>
    </div>
  );
}

/** The cards on 总览 (overview_card), one row under the agents' summary: one card full width, more two a row. */
export function OverviewCards() {
  const cards = usePluginSlot("overview_card");
  if (!cards.length) return null;
  return (
    <section aria-label="插件" className={cx("grid grid-cols-1 gap-3", cards.length > 1 && "md:grid-cols-2")}>
      {cards.map((c, i) => (
        <OverviewCard key={`${c.plugin}/${c.id}`} place={c} i={i} />
      ))}
    </section>
  );
}

const INTERACTIVE = "a,button,input,select,textarea,label,[role=radio],[role=switch],[tabindex]";

function OverviewCard({ place, i }: { place: UIPlace; i: number }) {
  const [gone, setGone] = useState(false);
  const context = useMemo(() => ({ size: "compact" as const }), []);
  const to = place.opens ? pageRoute(place.plugin, place.opens) : null;
  if (gone) return null;
  // the whole card opens the page; a button, a row or the chart inside it keeps its own click
  const open = (e: React.MouseEvent) => {
    if (!to || (e.target as HTMLElement).closest(INTERACTIVE)) return;
    if (window.getSelection()?.toString()) return;
    navigate(to);
  };
  return (
    <div
      data-plugin-card={`${place.plugin}/${place.id}`}
      onClick={open}
      className={cx("flex min-w-0 flex-col rounded-card bg-surface pb-1.5 shadow-card", to && "cursor-pointer transition-colors hover:bg-inset")}
      style={{ animation: `fade-up 420ms ${EASE} ${Math.min(i, 6) * 50}ms both` }}
    >
      <div className="flex min-w-0 items-center gap-2 px-4 pt-3 pb-0.5">
        {to ? (
          <a href={href(to)} onClick={(e) => onLinkClick(e, to)} className="group flex min-w-0 flex-1 items-center gap-1 outline-none focus-visible:underline">
            <span className="truncate text-[13px] font-medium text-ink">{place.title}</span>
            <Icon name="chevronRight" size={12} stroke={2.2} className="shrink-0 text-ink-3 transition-transform duration-150 group-hover:translate-x-0.5" />
          </a>
        ) : (
          <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-ink">{place.title}</span>
        )}
        {place.plugin_name && <span className="max-w-[45%] shrink-0 truncate text-[11px] text-ink-3">{place.plugin_name}</span>}
      </div>
      <PluginUI plugin={place.plugin} ui={place.id} context={context} compact onMissing={() => setGone(true)} />
    </div>
  );
}

/** The plugins' sections on a machine's page (machine_section), after its own; the plugin is told which machine. */
export function MachinePluginSections({ mid, heading }: { mid: string; heading: (title: string, right?: React.ReactNode) => React.ReactNode }) {
  const sections = usePluginSlot("machine_section");
  return (
    <>
      {sections.map((s) => (
        <MachineSection key={`${s.plugin}/${s.id}`} place={s} mid={mid} heading={heading} />
      ))}
    </>
  );
}

function MachineSection({ place, mid, heading }: { place: UIPlace; mid: string; heading: (title: string, right?: React.ReactNode) => React.ReactNode }) {
  const [gone, setGone] = useState(false);
  const context = useMemo(() => ({ machine: mid }), [mid]);
  if (gone) return null;
  return (
    <section data-plugin-section={`${place.plugin}/${place.id}`}>
      {heading(
        place.title,
        place.plugin_name && (
          <span className="flex min-w-0 items-center gap-1 text-[11.5px] text-ink-3">
            <Icon name="plugin" size={11} className="shrink-0" />
            <span className="truncate">{place.plugin_name}</span>
          </span>
        ),
      )}
      <PluginUI plugin={place.plugin} ui={place.id} context={context} bare title={place.title} onMissing={() => setGone(true)} />
    </section>
  );
}
