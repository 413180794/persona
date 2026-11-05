// @vitest-environment jsdom
// Node 25+ brings its own localStorage, undefined without --localstorage-file, and it hid jsdom's: every test that
// keeps something in this browser failed on Node 26 (#2745). vite.config.ts turns Node's off for the test runs.
import { expect, it } from "vitest";

it("gives a jsdom test the page's localStorage, not Node's", () => {
  localStorage.setItem("k", "v");
  expect(localStorage.getItem("k")).toBe("v");
  localStorage.clear();
  expect(localStorage.length).toBe(0);
});
