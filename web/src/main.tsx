// First: the live screen's token is read before App's module takes any token out of the address bar.
import { screen } from "./screen/take";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { startPWA } from "./lib/pwa";
import "./styles.css";
// Copying a selection that contains formulas puts their LaTeX ($…$ / $$…$$) on the clipboard, not KaTeX's glyphs.
// Imported here, not in Markdown.tsx, because it touches `document` on load and tests run without a DOM.
import "katex/contrib/copy-tex";

startPWA();

// The live screen (/screen/<machine>) is a page of its own: no sign-in flow, no sidebar, and noVNC loads only there.
const target = screen;
if (target) {
  import("./screen/ScreenView").then(({ ScreenView }) =>
    createRoot(document.getElementById("root")!).render(
      <StrictMode>
        <ScreenView target={target} />
      </StrictMode>,
    ),
  );
} else {
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}

