// First: the live screen's token is read before App's module takes any token out of the address bar.
import { screen } from "./screen/take";
import { initI18n } from "./i18n";
import "./styles.css";

// The language's catalog loads before the rest of the app: their modules call t() as they load (docs/web.md「国际化」).
await initI18n();
const [{ StrictMode, createElement }, { createRoot }, { startPWA }] = await Promise.all([import("react"), import("react-dom/client"), import("./lib/pwa")]);

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
