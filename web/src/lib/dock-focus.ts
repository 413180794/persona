// The keyboard's place after a card above the composer is answered (#5154): the button pressed goes (the card turns
// into its Answered receipt) or is disabled while it sends, and the browser drops the focus onto the page's body, so the
// next words typed go nowhere and Tab starts over from the sidebar. The chat puts it back in its composer instead.

/** Whether the focus was lost with `last`, the control above the composer it was on: nothing (the page's body) has it
 *  now, and `last` is gone from the page or can no longer be focused (disabled, or in a disabled fieldset). */
export function lostWith(last: Element | null, active: Element | null, body: Element | null): boolean {
  if (!last) return false;
  if (active && active !== body) return false;
  if (!last.isConnected) return true;
  return last.matches(":disabled") || Boolean(last.closest("fieldset:disabled"));
}
