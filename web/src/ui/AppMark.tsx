// Persona's mark on the page (the sidebar's header, the sign-in page): the chosen look's (形象, #3200, lib/look-icon.ts).
// With 团子, the default, it is the iOS app icon itself (apple/Persona/Resources/Assets.xcassets/AppIcon.appiconset
// icon.png / icon-dark.png, scaled into public/), not a redrawing; another look shows its avatar or face. The page's
// theme picks the light or the dark picture (it can differ from the system's, so not a <picture>).
import { BASE } from "../lib/base";
import { useLookIcon } from "../lib/look-icon";
import { cx } from "./atoms";

export function AppMark({ size, radius, className }: { size: number; radius: number; className?: string }) {
  const look = useLookIcon();
  const style = { width: size, height: size, borderRadius: radius };
  const img = "shrink-0 object-cover select-none";
  const light = look?.light ?? `${BASE}/app-icon.png`;
  const dark = look?.dark ?? `${BASE}/app-icon-dark.png`;
  return (
    <span aria-hidden className={cx("relative inline-flex shrink-0 overflow-hidden", className)} style={style}>
      <img src={light} alt="" draggable={false} width={size} height={size} className={cx(img, "dark:hidden")} style={style} />
      <img src={dark} alt="" draggable={false} width={size} height={size} className={cx(img, "hidden dark:block")} style={style} />
    </span>
  );
}
