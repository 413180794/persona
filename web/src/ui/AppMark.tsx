// Persona's mark on the page (the sidebar's header, the sign-in page): the iOS app icon itself, the same artwork
// (apple/Persona/Resources/Assets.xcassets/AppIcon.appiconset icon.png / icon-dark.png, scaled into public/), not a
// redrawing. The page's theme picks the light or the dark icon (it can differ from the system's, so not a <picture>).
import { BASE } from "../lib/base";
import { cx } from "./atoms";

export function AppMark({ size, radius, className }: { size: number; radius: number; className?: string }) {
  const style = { width: size, height: size, borderRadius: radius };
  const img = "shrink-0 object-cover select-none";
  return (
    <span aria-hidden className={cx("relative inline-flex shrink-0 overflow-hidden", className)} style={style}>
      <img src={`${BASE}/app-icon.png`} alt="" draggable={false} width={size} height={size} className={cx(img, "dark:hidden")} style={style} />
      <img src={`${BASE}/app-icon-dark.png`} alt="" draggable={false} width={size} height={size} className={cx(img, "hidden dark:block")} style={style} />
    </span>
  );
}
