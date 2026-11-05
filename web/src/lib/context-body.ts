// What a reader may take as the end of a block (the hub's closingTag in todo_decisions.go): either name, any case,
// spaces around the slash and before `>`.
const CLOSE_RE = /<\s*\/\s*(?:persona|jarvis)-context\s*>/gi;

/** A text to go inside a `<jarvis-context>` block, with nothing left in it that closes the block: closing tags are
 * removed, and one that the removal itself puts together (`</jarvis-</jarvis-context>context>`) has its `<` written
 * as `&lt;`, which joins nothing. Otherwise what follows would sit outside the block, as the user's own words (a mail
 * or message handed to the assistant comes from someone else). Two passes, linear in the text. */
export function contextBody(text: string): string {
  return text.replace(CLOSE_RE, "").replace(CLOSE_RE, (tag) => `&lt;${tag.slice(1)}`);
}
