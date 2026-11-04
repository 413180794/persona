// The chat's reading parts switch on these (#1996, contract §2). One implementation, in lib/viewport.ts (one cached
// list per query, a stable subscribe); this module only names them for ui/.
import { mediaMatches, NARROW, NO_HOVER, useMedia } from "../lib/viewport";

export { NARROW, NO_HOVER, useMedia };

export const isNoHover = () => mediaMatches(NO_HOVER);
