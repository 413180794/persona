// The connectors' own marks, used as they come (CLAUDE.md: brand marks are not redrawn): Feishu's and Telegram's in
// their colors; GitHub's is a template (one color), drawn in the text color so it reads in light and dark.
// A connector with no mark of its own (see `connectorBrand`) draws the generic icon where it is shown, not one of these.
import type { BrandId } from "../lib/connectors";
import larkMark from "../views/inbox/lark.svg";
import githubMark from "./brands/github.svg";
import telegramMark from "./brands/telegram.svg";

/** Where a mark is drawn: the tile of a settings row, or the badge in an avatar's corner. */
export type BrandFit = "tile" | "badge";

// Each mark's file and the share of its box (the tile, the badge) it is drawn at: Telegram's fills its viewBox, the others
// sit smaller inside theirs.
const MARKS: Record<BrandId, { src: string; template?: boolean; tile: number; badge: number }> = {
  github: { src: githubMark, template: true, tile: 0.6, badge: 0.72 },
  telegram: { src: telegramMark, tile: 0.78, badge: 1 },
  lark: { src: larkMark, tile: 0.6, badge: 0.72 },
};

/** `size` is the box the mark sits in (the tile or the badge); the mark is drawn at the share of it that its brand takes. */
export function BrandMark({ brand, size, fit, className = "" }: { brand: BrandId; size: number; fit: BrandFit; className?: string }) {
  const { src, template, ...share } = MARKS[brand];
  const px = Math.round(size * share[fit]);
  if (template) {
    const mask = `url("${src}") center / contain no-repeat`;
    return <span aria-hidden className={`inline-block shrink-0 bg-ink ${className}`} style={{ width: px, height: px, mask, WebkitMask: mask }} />;
  }
  return <img src={src} alt="" aria-hidden width={px} height={px} className={`shrink-0 ${className}`} style={{ width: px, height: px }} />;
}
