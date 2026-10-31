import { useCallback, useEffect, useState } from "react";
import { getToken, handleUnauthorized, setToken } from "./lib/api";
import { useRoute } from "./lib/router";
import { connect, disconnect, reset, useStore } from "./lib/store";
import { cx, EASE, Toasts } from "./ui/atoms";
import { ChatView } from "./views/chat/ChatView";
import { Home } from "./views/Home";
import { Jarvis } from "./views/Jarvis";
import { Login } from "./views/Login";
import { Machines } from "./views/Machines";
import { NewAgent } from "./views/NewAgent";
import { Palette } from "./views/Palette";
import { Sidebar } from "./views/Sidebar";

export function App() {
  const [authed, setAuthed] = useState(() => Boolean(getToken()));

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
      {authed ? <Shell /> : <Login onDone={() => setAuthed(true)} />}
      <Toasts />
    </>
  );
}

function Shell() {
  const route = useRoute();
  const conn = useStore((s) => s.conn);
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

  useEffect(() => setDrawer(false), [route.name, route.name === "session" ? route.sid : route.name === "jarvis" ? route.sid : ""]);

  const title =
    route.name === "session" ? "Agent" : route.name === "jarvis" ? "Jarvis" : route.name === "machines" ? "机器" : "总览";
  useEffect(() => {
    document.title = `${title} · Jarvis`;
  }, [title]);

  let main: React.ReactNode;
  switch (route.name) {
    case "session":
      main = <ChatView key={`${route.mid}/${route.sid}`} mid={route.mid} sid={route.sid} onToggleSidebar={toggle} />;
      break;
    case "jarvis":
      main = <Jarvis sid={route.sid} onToggleSidebar={toggle} />;
      break;
    case "machines":
      main = <Machines onToggleSidebar={toggle} />;
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
