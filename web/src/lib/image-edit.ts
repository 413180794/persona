// The image markup editor's geometry (iOS ImageEditor.swift): marks are kept in image pixels, drawn scaled to the
// fitted image on screen, and burned into the bitmap at its own resolution on 完成. No DOM here.

export type MarkTool = "box" | "pen";

export interface Point {
  x: number;
  y: number;
}

export interface Mark {
  tool: MarkTool;
  color: string;
  /** Image pixels; a box is its first and last point. */
  points: Point[];
}

export interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** The image scaled to fit inside the box, centered. */
export function fitRect(image: { width: number; height: number }, box: { width: number; height: number }): Rect {
  if (image.width <= 0 || image.height <= 0 || box.width <= 0 || box.height <= 0) return { x: 0, y: 0, width: 0, height: 0 };
  const s = Math.min(box.width / image.width, box.height / image.height);
  const width = image.width * s,
    height = image.height * s;
  return { x: (box.width - width) / 2, y: (box.height - height) / 2, width, height };
}

/** A point on screen (in the box) as image pixels, held inside the image. */
export function toImage(p: Point, fit: Rect, image: { width: number; height: number }): Point {
  if (fit.width <= 0) return { x: 0, y: 0 };
  const s = image.width / fit.width;
  return { x: Math.min(Math.max((p.x - fit.x) * s, 0), image.width), y: Math.min(Math.max((p.y - fit.y) * s, 0), image.height) };
}

/** An image point on screen. */
export function toScreen(p: Point, fit: Rect, image: { width: number }): Point {
  const s = image.width > 0 ? fit.width / image.width : 0;
  return { x: fit.x + p.x * s, y: fit.y + p.y * s };
}

/** A box mark's rectangle, whichever way it was dragged. */
export function boxRect(a: Point, b: Point): Rect {
  return { x: Math.min(a.x, b.x), y: Math.min(a.y, b.y), width: Math.abs(b.x - a.x), height: Math.abs(b.y - a.y) };
}

/** A mark worth keeping: more than a tap. */
export function keepMark(m: Mark): boolean {
  if (m.points.length < 2) return false;
  if (m.tool === "box") {
    const r = boxRect(m.points[0], m.points[m.points.length - 1]);
    return r.width >= 2 || r.height >= 2;
  }
  return true;
}

/** The stroke width burned in: about what it looked like on screen, at the image's scale. */
export function burnWidth(image: { width: number; height: number }): number {
  return Math.max(2, Math.max(image.width, image.height) / 220);
}
