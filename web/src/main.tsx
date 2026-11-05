// First: the live screen's token is read before App's module takes any token out of the address bar.
import { screen } from "./screen/take";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { ExtensionThemeProvider } from "./ui/ExtensionThemePicker";
import { App } from "./App";
import { startPWA } from "./lib/pwa";
import "./styles.css";

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
      <ExtensionThemeProvider><App /></ExtensionThemeProvider>
    </StrictMode>,
  );
}

