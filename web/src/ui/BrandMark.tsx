// The connectors' own marks, used as they come (CLAUDE.md: brand marks are not redrawn): Feishu's and Telegram's in
// their colors; GitHub's is a template (one color), drawn in the text color so it reads in light and dark.
import type { BrandId } from "../lib/connectors";
import larkMark from "../views/inbox/lark.svg";
import githubMark from "./brands/github.svg";
import telegramMark from "./brands/telegram.svg";

export function BrandMark({ brand, size, className = "" }: { brand: BrandId; size: number; className?: string }) {
  if (brand === "github") {
    const mask = `url("${githubMark}") center / contain no-repeat`;
    return <span aria-hidden className={`inline-block shrink-0 bg-ink ${className}`} style={{ width: size, height: size, mask, WebkitMask: mask }} />;
  }
  return <img src={brand === "lark" ? larkMark : telegramMark} alt="" aria-hidden width={size} height={size} className={`shrink-0 ${className}`} style={{ width: size, height: size }} />;
}
