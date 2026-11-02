import { useCallback, useEffect, useState } from "react";
import { api, errorText, getToken, handleUnauthorized, setToken } from "./lib/api";
import { pair, takeToken } from "./lib/auth";
import { useRoute } from "./lib/router";
import { connect, disconnect, reset, useStore } from "./lib/store";
import { cx, EASE, Spinner, Toasts } from "./ui/atoms";
import { AgentsView } from "./views/agents/AgentsView";
import { ChatView } from "./views/chat/ChatView";
import { Home } from "./views/Home";
import { Jarvis } from "./views/Jarvis";
import { Login } from "./views/Login";
import { Machines } from "./views/Machines";
import { NewAgent } from "./views/NewAgent";
import { StartingAgent } from "./views/StartingAgent";
import { Palette } from "./views/Palette";
import { Settings } from "./views/Settings";
import { Sidebar } from "./views/Sidebar";
import { TodosView } from "./views/Todos";

// A link with the hub's token (`?token=…` or `#token=…`) signs in without the form. The token leaves the address bar
// before anything else runs (history, bookmarks, a copied link), and is traded for this browser's own one.
const linked = typeof window === "undefined" ? null : takeToken(window.location.href);
if (linked) window.history.replaceState(window.history.state, "", linked.rest);

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
        const t = errorText(err);
        setLinkError(t === "invalid token" ? "链接里的 token 不对，贴一个新的" : t);
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
  const launch = useStore((s) => route.name === "session" ? s.launches[route.sid] : undefined);
  const starting = useStore((s) => route.name === "session" ? (s.sessions[route.mid] ?? []).find((session) => session.session_id === route.sid)?.starting : undefined);
  const [palette, setPalette] = useState(false);
  const [newAgent, setNewAgent] = useState(false);
  const [drawer, setDrawer] = useState(false);
  const toggle = useCallback(() => setDrawer((d) => !d), []);

  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPalette((p) => !p);
      }
    };
    addEventListener("keydown", key);
    return () => removeEventListener("keydown", key);
  }, []);

  useEffect(() => setDrawer(false), [route.name, route.name === "session" ? route.sid : route.name === "jarvis" ? route.sid : route.name === "agents" ? route.id : ""]);

  const title =
    route.name === "session" ? "Agent" : route.name === "jarvis" ? "Jarvis" : route.name === "todos" ? "待办" : route.name === "machines" ? "机器" : route.name === "settings" ? "设置" : route.name === "agents" ? "个人 Agent" : "总览";
  useEffect(() => {
    document.title = `${title} · Jarvis`;
  }, [title]);

  let main: React.ReactNode;
  switch (route.name) {
    case "session":
      main = launch ? <StartingAgent launch={launch} onToggleSidebar={toggle} /> : <ChatView key={`${route.mid}/${route.sid}`} mid={route.mid} sid={route.sid} starting={starting} onToggleSidebar={toggle} />;
      break;
    case "jarvis":
      main = <Jarvis sid={route.sid} onToggleSidebar={toggle} />;
      break;
    case "todos":
      main = <TodosView selectedId={route.id} onToggleSidebar={toggle} />;
      break;
    case "agents":
      main = <AgentsView key={route.id ?? ""} id={route.id} onToggleSidebar={toggle} />;
      break;
    case "machines":
      main = <Machines onToggleSidebar={toggle} />;
      break;
    case "settings":
      main = <Settings onToggleSidebar={toggle} />;
      break;
    default:
      main = <Home onNewAgent={() => setNewAgent(true)} onToggleSidebar={toggle} />;
  }

  return (
    <div className="flex h-full overflow-hidden bg-page">
      {/* wide: a fixed column; narrow: a drawer over the page */}
      <div
        className={cx(
          "fixed inset-y-0 left-0 z-30 w-[284px] border-r border-line transition-transform duration-300 lg:static lg:z-auto lg:translate-x-0 lg:shadow-none",
          drawer ? "translate-x-0 shadow-overlay" : "-translate-x-full",
        )}
        style={{ transitionTimingFunction: EASE }}
      >
        <Sidebar route={route} onSearch={() => setPalette(true)} onNewAgent={() => setNewAgent(true)} onNavigate={() => setDrawer(false)} />
      </div>
      {drawer && <div className="fixed inset-0 z-20 bg-black/20 lg:hidden" style={{ animation: "fade-in 150ms ease-out both" }} onClick={() => setDrawer(false)} />}
      <main className="relative min-w-0 flex-1">
        {conn === "offline" && (
          <div className="absolute inset-x-0 top-14 z-10 flex justify-center px-4 pt-2" style={{ animation: `slide-up 220ms ${EASE} both` }}>
            <div className="flex items-center gap-2 rounded-full bg-tooltip-bg px-3 py-1.5 text-[12px] text-tooltip-fg shadow-overlay">
              <span className="size-1.5 rounded-full bg-red" />
              连不上 hub，正在重试…
            </div>
          </div>
        )}
        {main}
      </main>
      <Palette open={palette} onClose={() => setPalette(false)} onNewAgent={() => setNewAgent(true)} />
      <NewAgent open={newAgent} onClose={() => setNewAgent(false)} />
    </div>
  );
}

// Signing in with the link's token: a moment, no form.
function Joining() {
  return (
    <div className="flex min-h-full items-center justify-center bg-page" role="status" aria-live="polite">
      <span className="flex items-center gap-2 text-[13px] text-ink-2">
        <Spinner />
        正在连接 Jarvis…
      </span>
    </div>
  );
}
