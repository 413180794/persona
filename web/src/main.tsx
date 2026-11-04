import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import "./styles.css";
// Copying a selection that contains formulas puts their LaTeX ($…$ / $$…$$) on the clipboard, not KaTeX's glyphs.
// Imported here, not in Markdown.tsx, because it touches `document` on load and tests run without a DOM.
import "katex/contrib/copy-tex";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
