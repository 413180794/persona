// Busy indicators under reduced motion. styles.css strips every animation to ~1ms × 1 iteration for people who asked the
// system for less motion; that also froze the spinners and the working dot, so "something is happening" vanished.
// An element that carries this class is exempt from the strip: it keeps going, at a slow 2.4s (the pace Moshi slows
// its loader to), while the movement, scale and slide animations stay off. Mark anything whose only job is to say
// "busy" or "trying": spinners, the working / connecting dots, the loader's shimmer and grid. busy-motion.test.ts
// fails for a looping busy animation in the source that is not marked.

/** The class that exempts an element from the reduced-motion strip (styles.css, `.busy-motion`). */
export const BUSY_MOTION = "busy-motion";

/** How long one cycle of a busy indicator takes under reduced motion, in seconds (styles.css has the same number). */
export const BUSY_MOTION_REDUCED_SECONDS = 2.4;
