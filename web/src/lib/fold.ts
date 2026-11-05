// Whether a sidebar section is folded: what the user chose for it (fold:<key> is "1" or "0"), else its default.
export function foldedNow(stored: string | null, initial: boolean): boolean {
  return stored === null ? initial : stored === "1";
}
