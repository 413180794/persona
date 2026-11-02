// The hub admin UI (docs/admin.md): its own page next to the web client, same tokens and atoms.
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { AdminApp } from "./App";
import "../styles.css";

// Dev only (`npm run dev`, ?mock=<scenario>): canned data instead of a hub, for building and screenshots. The
// production build drops this branch and the mock module with it.
if (import.meta.env.DEV) {
  const q = new URLSearchParams(location.search);
  const theme = q.get("theme");
  if (theme === "dark" || theme === "light") document.documentElement.classList.toggle("dark", theme === "dark");
  const scenario = q.get("mock");
  if (scenario) (await import("./mock")).installMock(scenario);
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AdminApp />
  </StrictMode>,
);
