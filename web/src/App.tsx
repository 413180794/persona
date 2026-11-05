import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, errorText, getToken, handleUnauthorized, setToken } from "./lib/api";
import { pair, takeToken } from "./lib/auth";
import { useCloseOnBack } from "./lib/back-close";
import { SCRIM_FADE, useLingers } from "./lib/lingers";
import { PageBoundary } from "./views/PageBoundary";
import { drawerA11y, focusBack, useDrawerDrag } from "./lib/drawer";
import { acceptHubs, rememberHub, takeHubs } from "./lib/hubs";
import { closePanel, getPanel } from "./lib/panel";
import { resolveTab, tabOfSessionLink, tabPagePane } from "./lib/herdr-tree";
import { resolveSessionLink, sessionLinkRoute } from "./lib/session-link";
import { href, navigate, onLinkClick, useRoute } from "./lib/router";
import { ago, sessionTitle, shortPath } from "./lib/format";
import type { Session } from "./lib/types";
import { unreachable } from "./lib/shell";
import { NARROW, useKeyboardFrame, useMedia } from "./lib/viewport";
import { connect, disconnect, reset, useStore } from "./lib/store";
import { t } from "./i18n";
import { Button, cx, EASE, IconButton, Loading, Spinner, Toasts } from "./ui/atoms";
import { focusables, trapTab } from "./ui/overlay";
import { Empty } from "./views/session/files/parts";
import { AgentsPage } from "./views/AgentsPage";
import { ChatView } from "./views/chat/ChatView";
import { Home } from "./views/Home";
import { PersonaAssistant } from "./views/PersonaAssistant";
import { Login } from "./views/Login";
import { NewAgent } from "./views/NewAgent";
import { onOpenNewAgent, type NewAgentPreset } from "./lib/new-agent";
import { StartingAgent } from "./views/StartingAgent";
import { Palette } from "./views/Palette";
import { ShortcutHelp } from "./views/ShortcutHelp";
import { closePalette, openPalette, usePalette } from "./lib/palette";
import { sidebarRail, sidebarShown, useLayout } from "./lib/layout";
import { SIDEBAR_LABEL, useShortcuts } from "./lib/use-shortcuts";
import { Sidebar, SidebarRail } from "./views/Sidebar";
import { SidePanel } from "./views/SidePanel";
import { ExtensionNavPage } from "./views/extensions/ExtensionSlots";
import { useExtensionPlace } from "./lib/extension-slots";
import { useAttentionSignals } from "./lib/attention-signals";

// The pages a first screen doesn't need load when they are opened (#1996); 对话, Persona and 总览 stay in the main
// bundle (the extensions' page too: the sidebar already has ExtensionSlots for its icons).
const AgentsView = lazy(() => import("./views/agents/AgentsView").then((m) => ({ default: m.AgentsView })));
const WorkspaceBoard = lazy(() => import("./views/WorkspaceBoard").then((m) => ({ default: m.WorkspaceBoard })));
const Machines = lazy(() => import("./views/Machines").then((m) => ({ default: m.Machines })));
const TerminalPage = lazy(() => import("./views/terminal/TerminalPage").then((m) => ({ default: m.TerminalPage })));
const MachineDetail = lazy(() => import("./views/machines/MachineDetail").then((m) => ({ default: m.MachineDetail })));
const Settings = lazy(() => import("./views/Settings").then((m) => ({ default: m.Settings })));
const History = lazy(() => import("./views/assistant/History").then((m) => ({ default: m.History })));
const Profile = lazy(() => import("./views/assistant/Profile").then((m) => ({ default: m.Profile })));
const Routines = lazy(() => import("./views/assistant/Routines").then((m) => ({ default: m.Routines })));
const Inbox = lazy(() => import("./views/inbox/Inbox").then((m) => ({ default: m.Inbox })));
const Messages = lazy(() => import("./views/messages/Messages").then((m) => ({ default: m.Messages })));
const Knowledge = lazy(() => import("./views/knowledge/Knowledge").then((m) => ({ default: m.Knowledge })));
const TodosView = lazy(() => import("./views/Todos").then((m) => ({ default: m.TodosView })));

// A link with the hub's token (`?token=…` or `#token=…`) signs in without the form. The token leaves the address bar
// before anything else runs (history, bookmarks, a copied link), and is traded for this browser's own one.
const linked = typeof window === "undefined" ? null : takeToken(window.location.href);
if (linked) window.history.replaceState(window.history.state, "", linked.rest);

// Switching from another hub's page hands its list of hubs over in the fragment (`#hubs=`, lib/hubs.ts): take it into
// this browser's list and out of the address bar the same way.
const carried = typeof window === "undefined" ? null : takeHubs(window.location.href);
if (carried) {
  acceptHubs(carried.hubs);
  window.history.replaceState(window.history.state, "", carried.rest);
}

export function App() {
  const [authed, setAuthed] = useState(() => !linked?.token && Boolean(getToken()));
  const [joining, setJoining] = useState(() => Boolean(linked?.token));
  const [linkError, setLinkError] = useState("");

  useEffect(() => {
    if (!linked?.token) return;
    let live = true;
    // Already signed in here (the link opened again): keep this browser's token instead of minting another one.
    const signedIn = getToken() ? api("/machines").then(() => true, () => false) : Promise.resolve(false);
    signedIn
      .then((ok) => (ok ? undefined : pair(linked.token)))
      .then(() => live && setAuthed(true))
      .catch((err) => {
        if (!live) return;
        const msg = errorText(err);
        setLinkError(msg === "invalid token" ? t("app.link_token_invalid") : msg);
        setAuthed(Boolean(getToken())); // still signed in from before: stay in
      })
      .finally(() => live && setJoining(false));
    return () => {
      live = false;
    };
  }, []);

  useEffect(() => {
    handleUnauthorized(() => {
      setToken("");
      reset();
      setAuthed(false);
    });
  }, []);

  useEffect(() => {
    if (!authed) return;
    connect();
    return () => disconnect();
  }, [authed]);

  return (
    <>
      {joining ? <Joining /> : authed ? <Shell /> : <Login error={linkError} onDone={() => setAuthed(true)} />}
      <Toasts />
    </>
  );
}

function Shell() {
  const route = useRoute();
  const conn = useStore((s) => s.conn);
  // this hub goes on the list of hubs to switch between, under the name it gives itself
  const hubName = useStore((s) => s.hubName);
  useEffect(() => void rememberHub(location.origin, hubName), [hubName]);
  // the conversation on the page: a session's (`/s/…`), or the pane a tab (`/t/…`) opens on
  const tabSessions = useStore((s) => (route.name === "tab" || route.name === "session" ? s.sessions[route.mid] : undefined));
  const loaded = useStore((s) => s.loaded);
  const path = href(route);
  // eslint-disable-next-line react-hooks/exhaustive-deps -- `route` is parsed anew each render; its path says it all
  const tabPane = useMemo(() => (route.name === "tab" ? resolveTab(tabSessions ?? [], route) : undefined), [tabSessions, path]);
  // an old `/s/…` link of a pane in a tab becomes that tab's `/t/…` (in place: Back doesn't come here again)
  const tabLink = route.name === "session" ? tabOfSessionLink(tabSessions ?? [], route) : undefined;
  // a `/s/…` link with only the front of an id (the 8-character ones shown everywhere, #2464): the whole id's link,
  // in place; several sessions starting with it are listed to pick from
  const link = route.name === "session" ? resolveSessionLink(tabSessions ?? [], route.mid, route.sid, loaded) : undefined;
  const linkTo = link?.kind === "prefix" && route.name === "session" ? sessionLinkRoute(tabSessions ?? [], link.session, route.view) : undefined;
  const tabLinkPath = tabLink ? href(tabLink) : linkTo ? href(linkTo) : "";
  useEffect(() => {
    if (tabLinkPath) navigate(tabLinkPath, true);
  }, [tabLinkPath]);
  const pane = route.name === "session" ? (link?.kind === "exact" || link?.kind === "unlisted" ? { mid: route.mid, sid: route.sid } : undefined)
    : route.name === "tab" ? tabPagePane(tabPane, route, loaded)
    : undefined;
  const launch = useStore((s) => (pane ? s.launches[pane.sid] : undefined));
  const starting = useStore((s) => (pane ? (s.sessions[pane.mid] ?? []).find((session) => session.session_id === pane.sid)?.starting : undefined));
  const shell = useRef<HTMLDivElement>(null);
  useKeyboardFrame(shell);
  // a tab opened without a pane settles on the one it picked: another pane needing you later does not swap the page
  const picked = route.name === "tab" && !route.sid ? pane?.sid : undefined;
  useEffect(() => {
    if (picked && route.name === "tab") navigate({ ...route, sid: picked }, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [picked]);
  const palette = usePalette();
  const layout = useLayout();
  const [newAgent, setNewAgent] = useState(false);
  const [preset, setPreset] = useState<NewAgentPreset | null>(null);
  useEffect(() => onOpenNewAgent((p) => {
    setPreset(p);
    setNewAgent(true);
  }), []);
  const openNewAgent = useCallback(() => setNewAgent(true), []);

  // ── the sidebar: a column on a wide screen, a drawer over the page on a narrow one (#1996) ──
  const narrow = useMedia(NARROW);
  const [drawer, setDrawer] = useState(false);
  const toggle = useCallback(() => setDrawer((d) => !d), []);
  const close = useCallback(() => setDrawer(false), []);
  // The scrim leaves the page once it has faded out (#2102): kept there at opacity 0, iOS took its colour (the page's
  // under 20% black) for the status bar strip of the installed app.
  const scrimShown = useLingers(drawer, SCRIM_FADE);
  const drawerBox = useRef<HTMLDivElement>(null);
  const scrim = useRef<HTMLDivElement>(null);
  const mainBox = useRef<HTMLElement>(null);
  const a11y = drawerA11y(narrow, drawer);
  // folded to its icons on a wide screen (⌘⌥B, kept in this browser); a narrow screen keeps the full drawer
  const rail = !narrow && sidebarRail(layout);
  useEffect(() => {
    if (!narrow) setDrawer(false);
  }, [narrow]);
  // dragged out and back with a finger anywhere on the page or the drawer (not on a full-screen side panel)
  useDrawerDrag({
    root: shell,
    drawer: drawerBox,
    scrim,
    open: drawer,
    setOpen: setDrawer,
    enabled: narrow,
    scope: (t) => [mainBox, drawerBox, scrim].some((r) => r.current?.contains(t)),
  });
  // Back closes it first
  useCloseOnBack(drawer, close, narrow);
  // Esc closes it (a menu or a dialog over it closes first); Tab stays in it, like a dialog's
  useEffect(() => {
    if (!a11y.modal) return;
    const key = (e: KeyboardEvent) => {
      if (document.querySelector("[role=menu]") || document.querySelectorAll("[role=dialog]").length > 1) return;
      if (e.key === "Tab" && drawerBox.current) {
        const list = focusables(drawerBox.current);
        const to = trapTab(list.length, list.indexOf(document.activeElement as HTMLElement), e.shiftKey);
        if (to === null) return;
        e.preventDefault();
        list[to]?.focus();
        return;
      }
      if (e.key !== "Escape") return;
      e.preventDefault();
      e.stopPropagation();
      close();
    };
    addEventListener("keydown", key, true);
    return () => removeEventListener("keydown", key, true);
  }, [a11y.modal, close]);
  // open, focus moves into it; shut, back to what opened it
  const opener = useRef<HTMLElement | null>(null);
  useEffect(() => {
    const box = drawerBox.current;
    if (!box || !narrow) return;
    if (drawer) {
      opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      focusables(box)[0]?.focus({ preventScroll: true });
      return;
    }
    const back = focusBack(opener.current, document.activeElement, box.contains(document.activeElement), document.body);
    opener.current = null;
    back?.focus({ preventScroll: true });
  }, [drawer, narrow]);

  // the keyboard shortcuts (⌘K and the rest, lib/shortcuts.ts): what they do here, and the listener that runs them
  useShortcuts({ route, pane, narrow, toggleDrawer: toggle });

  useEffect(() => setDrawer(false), [route.name, pane ? pane.sid : route.name === "jarvis" ? route.sid : route.name === "agents" ? route.id : ""]);
  // a session's terminal / files / Git belong to that conversation: moving to another page closes them (a link and a
  // task stay)
  const at = pane ? `${pane.mid}/${pane.sid}` : route.name;
  useEffect(() => {
    const p = getPanel();
    if (p && p.kind !== "link" && p.kind !== "task" && p.kind !== "agent" && !(p.kind !== "file" && pane && p.mid === pane.mid && p.sid === pane.sid)) closePanel();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [at]);

  const extensionTitle = useExtensionPlace(route.name === "extension" ? route.extension : "", route.name === "extension" ? route.ui : "")?.title;
  const title =
    pane ? "Agent" : route.name === "jarvis" ? "Persona" : route.name === "persona-new" ? t("app.title_new_chat") : route.name === "history" ? t("app.title_history") : route.name === "profile" ? t("app.title_profile") : route.name === "inbox" ? t("app.title_inbox") : route.name === "messages" ? t("app.title_messages")
      : route.name === "routines" ? t("app.title_routines") : route.name === "kb" ? t("app.title_kb") : route.name === "workspace-board" ? t("app.title_workspace_board") : route.name === "todos" ? t("app.title_todos") : route.name === "machines" || route.name === "machine" ? t("app.title_machines") : route.name === "terminal" ? t("app.title_terminal") : route.name === "settings" ? t("app.title_settings")
      : route.name === "agents" ? t("app.title_agents") : route.name === "agent-list" ? "Agents" : route.name === "extension" ? extensionTitle || t("app.title_extension") : t("app.title_home");
  useAttentionSignals(title, pane); // tab title with the 需要你 count, favicon badge, optional sounds (#2332)

  // never reached the hub: say so with a way to try again, instead of pages loading forever (kept through a retry)
  const [away, setAway] = useState(false);
  const offline = unreachable(away, conn, loaded);
  if (offline !== away) setAway(offline);
  let main: React.ReactNode;
  if (offline) main = <Offline onToggleSidebar={toggle} trying={conn === "connecting"} />;
  else switch (route.name) {
    case "session":
    case "tab":
      main = link?.kind === "ambiguous" ? <SessionChoices sid={route.name === "session" ? route.sid : ""} candidates={link.candidates} all={tabSessions ?? []} onToggleSidebar={toggle} />
        : !pane ? (loaded && route.name === "tab" ? <TabGone onToggleSidebar={toggle} /> : <Loading />)
        : launch ? <StartingAgent launch={launch} onToggleSidebar={toggle} />
        : <ChatView key={`${pane.mid}/${pane.sid}`} mid={pane.mid} sid={pane.sid} starting={starting} onToggleSidebar={toggle} inPage />;
      break;
    case "jarvis":
      main = <PersonaAssistant sid={route.sid} onToggleSidebar={toggle} />;
      break;
    case "persona-new":
      main = <PersonaAssistant fresh onToggleSidebar={toggle} />;
      break;
    case "history":
      main = <History onToggleSidebar={toggle} />;
      break;
    case "profile":
      main = <Profile onToggleSidebar={toggle} />;
      break;
    case "inbox":
      main = <Inbox id={route.id} onToggleSidebar={toggle} />;
      break;
    case "messages":
      main = <Messages id={route.id} onToggleSidebar={toggle} />;
      break;
    case "routines":
      main = <Routines onToggleSidebar={toggle} />;
      break;
    case "kb":
      main = <Knowledge kind={route.kind} id={route.id} onToggleSidebar={toggle} />;
      break;
    case "workspace-board":
      main = <WorkspaceBoard onToggleSidebar={toggle} />;
      break;
    case "todos":
      main = <TodosView selectedId={route.id} onToggleSidebar={toggle} />;
      break;
    case "agents":
      main = <AgentsView key={route.id ?? ""} id={route.id} onToggleSidebar={toggle} />;
      break;
    case "agent-list":
      main = <AgentsPage route={route} onToggleSidebar={toggle} onNewAgent={openNewAgent} />;
      break;
    case "machines":
      main = <Machines onToggleSidebar={toggle} />;
      break;
    case "terminal":
      main = <TerminalPage route={route} onToggleSidebar={toggle} />;
      break;
    case "machine":
      main = <MachineDetail key={route.mid} mid={route.mid} onToggleSidebar={toggle} />;
      break;
    case "settings":
      main = <Settings onToggleSidebar={toggle} />;
      break;
    case "extension":
      main = <ExtensionNavPage key={`${route.extension}/${route.ui}`} extension={route.extension} ui={route.ui} onToggleSidebar={toggle} />;
      break;
    default:
      main = <Home onNewAgent={openNewAgent} onToggleSidebar={toggle} />;
  }

  return (
    // #root is 100dvh (index.html); with a soft keyboard up (iOS) the shell is only the part above it (lib/viewport.ts)
    <div ref={shell} className="flex h-full overflow-hidden bg-page">
      {/* wide: a fixed column; narrow: a drawer over the page, clear of a landscape notch and the home indicator */}
      <div
        ref={drawerBox}
        role={a11y.modal ? "dialog" : undefined}
        aria-modal={a11y.modal || undefined}
        aria-label={a11y.modal ? SIDEBAR_LABEL : undefined}
        inert={a11y.inert || undefined}
        className={cx(
          "fixed inset-y-0 left-0 z-30 w-[284px] border-r border-line transition-transform duration-300 max-lg:w-[calc(284px+env(safe-area-inset-left))] max-lg:bg-canvas max-lg:pb-[env(safe-area-inset-bottom)] max-lg:pl-[env(safe-area-inset-left)] lg:static lg:z-auto lg:translate-x-0 lg:shadow-none",
          drawer ? "translate-x-0 shadow-overlay" : "-translate-x-full",
          // ⌘⌥B folds it to the rail, ⌘⇧↵ hides it, on a wide screen
          rail && "lg:w-14",
          !sidebarShown(layout) && "lg:hidden",
        )}
        style={{ transitionTimingFunction: EASE }}
      >
        {rail ? <SidebarRail route={route} onSearch={() => openPalette("jump")} onNewAgent={openNewAgent} /> : <Sidebar route={route} onSearch={() => openPalette("jump")} onNewAgent={openNewAgent} onNavigate={close} />}
      </div>
      {/* the scrim fades out as well as in; a drag sets its opacity as it goes (lib/drawer.ts) */}
      <div
        ref={scrim}
        aria-hidden
        className={cx("fixed inset-0 z-20 bg-black/20 transition-opacity duration-200 lg:hidden", drawer ? "opacity-100 starting:opacity-0" : "pointer-events-none opacity-0", !scrimShown && "hidden")}
        onClick={close}
      />
      <main ref={mainBox} inert={a11y.modal || undefined} className="relative min-w-0 flex-1">
        {conn === "offline" && !offline && (
          <div className="absolute inset-x-0 top-14 z-10 flex justify-center px-4 pt-2" style={{ animation: `slide-up 220ms ${EASE} both` }}>
            <div className="flex items-center gap-2 rounded-full bg-tooltip-bg px-3 py-1.5 text-[12px] text-tooltip-fg shadow-overlay">
              <span className="size-1.5 rounded-full bg-red" />
              {t("app.reconnecting")}
            </div>
          </div>
        )}
        <PageBoundary page={href(route)}><Suspense fallback={<div className="flex h-full items-center justify-center"><Loading /></div>}>{main}</Suspense></PageBoundary>
      </main>
      <SidePanel />
      <Palette open={palette.open} onClose={closePalette} onNewAgent={openNewAgent} pane={pane} route={route} />
      <ShortcutHelp />
      <NewAgent open={newAgent} preset={preset} onClose={() => {
        setNewAgent(false);
        setPreset(null);
      }} />
    </div>
  );
}

// The hub was never reached (offline, or it is down): an empty state that says so and tries again on a tap, instead of
// every page loading forever. The socket keeps retrying on its own as well; once in, the page comes back by itself.
function Offline({ onToggleSidebar, trying }: { onToggleSidebar: () => void; trying: boolean }) {
  return (
    <div className="flex h-full flex-col">
      <header className="flex h-14 shrink-0 items-center px-3 sm:px-4">
        <IconButton icon="sidebar" label={t("app.sidebar")} onClick={onToggleSidebar} className="lg:hidden" />
      </header>
      <div className="flex min-h-0 flex-1 flex-col pb-[10vh]" role="status">
        <Empty
          icon="hub"
          title={t("app.offline_title")}
          detail={t("app.offline_detail")}
          action={<Button tone="secondary" icon="retry" busy={trying} onClick={connect}>{trying ? t("app.connecting") : t("app.retry")}</Button>}
        />
      </div>
    </div>
  );
}

// A tab link whose tab is gone (closed, or herdr restarted with new ids) and no pane to fall back to.
function TabGone({ onToggleSidebar }: { onToggleSidebar: () => void }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
      <p className="text-[13px] text-ink-2">{t("app.tab_gone")}</p>
      <button type="button" onClick={onToggleSidebar} className="whitespace-nowrap rounded-control px-2.5 py-1.5 text-[12.5px] font-medium text-accent-ink hover:bg-hover lg:hidden">{t("app.open_sidebar")}</button>
    </div>
  );
}

// A `/s/…` link whose id prefix more than one session on the machine starts with (#2464): pick one, none is guessed.
function SessionChoices({ sid, candidates, all, onToggleSidebar }: { sid: string; candidates: Session[]; all: Session[]; onToggleSidebar: () => void }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
      <p className="text-[13px] text-ink-2">{t("app.session_choices", { count: candidates.length, sid })}</p>
      <ul className="flex w-full max-w-96 flex-col gap-1">
        {candidates.map((s) => {
          const to = sessionLinkRoute(all, s);
          return (
            <li key={s.session_id}>
              <a href={href(to)} onClick={(e) => onLinkClick(e, to)} className="flex flex-col items-start gap-0.5 rounded-control px-3 py-2 text-left hover:bg-hover">
                <span className="w-full truncate text-[13px] font-medium text-ink">{sessionTitle(s)}</span>
                <span className="w-full truncate font-mono text-[11.5px] text-ink-3">{s.session_id}</span>
                {(s.cwd || s.activity_at || s.updated_at) && (
                  <span className="w-full truncate text-[11.5px] text-ink-3">{[shortPath(s.cwd), ago(s.activity_at || s.updated_at)].filter(Boolean).join(" · ")}</span>
                )}
              </a>
            </li>
          );
        })}
      </ul>
      <button type="button" onClick={onToggleSidebar} className="whitespace-nowrap rounded-control px-2.5 py-1.5 text-[12.5px] font-medium text-accent-ink hover:bg-hover lg:hidden">{t("app.open_sidebar")}</button>
    </div>
  );
}

// Signing in with the link's token: a moment, no form.
function Joining() {
  return (
    <div className="flex min-h-full items-center justify-center bg-page" role="status" aria-live="polite">
      <span className="flex items-center gap-2 text-[13px] text-ink-2">
        <Spinner />
        {t("app.joining")}
      </span>
    </div>
  );
}
