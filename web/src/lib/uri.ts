/** `s` percent-decoded; a malformed escape (a truncated or mistyped address: %E0%A4%A, %ZZ) leaves it as it is. */
export function decodeURIComponentSafe(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}
