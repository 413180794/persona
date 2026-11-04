import JSZip from "jszip";
import { describe, expect, it } from "vitest";
import { IWORK_PREVIEWS, readZipEntry } from "./zip-range";

async function pages(files: Record<string, Uint8Array | string>, compression: "STORE" | "DEFLATE" = "STORE") {
  const z = new JSZip();
  for (const [name, data] of Object.entries(files)) z.file(name, data, { compression });
  return z.generateAsync({ type: "uint8array" });
}

function reader(bytes: Uint8Array) {
  const calls: [number, number][] = [];
  return {
    calls,
    read: async (s: number, e: number) => {
      calls.push([s, e]);
      return bytes.slice(s, e);
    },
  };
}

describe("readZipEntry", () => {
  it("reads the best preview with a few ranges, not the whole file", async () => {
    const big = new Uint8Array(400_000).map((_, i) => (i * 7) & 255); // the document's own data, not read
    const jpg = new Uint8Array([0xff, 0xd8, 0xff, 1, 2, 3]);
    const bytes = await pages({ "Index/Document.iwa": big, "preview.jpg": jpg, "preview-micro.jpg": new Uint8Array([9]) });
    const r = reader(bytes);
    const got = await readZipEntry(r.read, bytes.length, IWORK_PREVIEWS);
    expect(got?.name).toBe("preview.jpg");
    expect([...got!.data]).toEqual([...jpg]);
    expect(r.calls.length).toBeLessThanOrEqual(3);
    expect(r.calls.reduce((n, [s, e]) => n + e - s, 0)).toBeLessThan(bytes.length / 3);
  });

  it("prefers the PDF of every page, inflates deflated entries, matches names without case", async () => {
    const pdf = new TextEncoder().encode("%PDF-1.4 " + "x".repeat(5000));
    const bytes = await pages({ "QuickLook/preview.PDF": pdf, "preview.jpg": "jpg" }, "DEFLATE");
    const got = await readZipEntry(reader(bytes).read, bytes.length, IWORK_PREVIEWS);
    expect(got?.name).toBe("QuickLook/preview.PDF");
    expect(new TextDecoder().decode(got!.data)).toBe(new TextDecoder().decode(pdf));
  });

  it("says null for no preview or no zip", async () => {
    const bytes = await pages({ "Index/Document.iwa": "x" });
    expect(await readZipEntry(reader(bytes).read, bytes.length, IWORK_PREVIEWS)).toBeNull();
    const junk = new TextEncoder().encode("not a zip at all");
    expect(await readZipEntry(reader(junk).read, junk.length, IWORK_PREVIEWS)).toBeNull();
  });
});
