// First: the live screen's token is read before App's module takes any token out of the address bar.
import { screen } from "./screen/take";
import { initI18n } from "./i18n";
import "./styles.css";
import { applyLook } from "./ui/look";

// 主题色 and 字号 (docs/web.md「外观」) before anything draws.
applyLook();

// The language's catalog loads before the rest of the app: their modules call t() as they load (docs/web.md「国际化」).
// React and the PWA helper say no t(), so they load alongside it rather than after it.
// An overlay's history entry a reload left behind goes before the app reads the address (lib/back-close.ts, #5134).
const [, { StrictMode, createElement }, { createRoot }, { startPWA }] = await Promise.all([
  initI18n(),
  import("react"),
  import("react-dom/client"),
  import("./lib/pwa"),
  import("./lib/back-close").then((m) => m.settleOverlayEntry()),
]);

startPWA();

// The live screen (/screen/<machine>) is a page of its own: no sign-in flow, no sidebar, and noVNC loads only there.
const target = screen;
const root = createRoot(document.getElementById("root")!);
if (target) {
  const { ScreenView } = await import("./screen/ScreenView");
  root.render(createElement(StrictMode, null, createElement(ScreenView, { target })));
} else {
  const [{ ExtensionThemeProvider }, { App }] = await Promise.all([import("./ui/ExtensionThemePicker"), import("./App")]);
  root.render(createElement(StrictMode, null, createElement(ExtensionThemeProvider, null, createElement(App))));
}
