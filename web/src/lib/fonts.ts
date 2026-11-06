// Font fallbacks for documents rendered in the browser (Office previews, #1345): what to use when the font a file names
// is not on this machine.

/** A docx page's text with no font of its own: the system's sans, with Chinese and Japanese ones before the browser's serif default. */
export const SANS = `-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Hiragino Sans GB","Noto Sans CJK SC","Source Han Sans SC","Microsoft YaHei",sans-serif`;
export const SERIF = `"Songti SC","STSong","Noto Serif CJK SC","Source Han Serif SC",SimSun,serif`;

/**
 * A font list from the document with fallbacks after it: a font this machine lacks (Calibri, DengXian on a Mac) would
 * otherwise fall to the browser's default, a heavy serif Song for Chinese. Serif only when the document names a serif.
 */
export function withFallback(family: string): string {
  const f = family.trim();
  if (!f || /(^|,)\s*(sans-serif|serif|monospace)\s*$/i.test(f)) return f;
  const first = f.split(",")[0].replace(/["']/g, "").trim().toLowerCase();
  const serif = /^(times( new roman)?|georgia|cambria|garamond|book antiqua|palatino( linotype)?|baskerville|century( schoolbook)?|simsun|nsimsun|宋体|新宋体|仿宋|fangsong|simfang|仿宋_gb2312|楷体|kaiti|kaiti sc|楷体_gb2312|stsong|stkaiti|stfangsong|songti sc|华文宋体|华文仿宋|华文楷体|mingliu|pmingliu|細明體|新細明體|noto serif.*|source han serif.*)$/.test(first);
  return `${f},${serif ? SERIF : SANS}`;
}
