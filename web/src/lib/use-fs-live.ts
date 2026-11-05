import { useEffect, useRef } from "react";
import { fsLive, type FsChange, type FsWatchHandle } from "./fs-live";

/**
 * Watch a session folder's open folders (`paths`) and, with `git`, its Git status while the component is mounted;
 * `onChange` gets what changed, debounced (fs-live.ts). `paths` may be a new array every render.
 */
export function useFsLive(mid: string, cwd: string, paths: string[], git: boolean, onChange: (c: FsChange) => void) {
  const cb = useRef(onChange);
  cb.current = onChange;
  const handle = useRef<FsWatchHandle | null>(null);
  const key = JSON.stringify([paths, git]);
  const latest = useRef({ paths, git });
  latest.current = { paths, git };

  useEffect(() => {
    if (!mid || !cwd) return;
    const h = fsLive.watch({ mid, cwd, ...latest.current }, (c) => cb.current(c));
    handle.current = h;
    return () => {
      h.stop();
      handle.current = null;
    };
  }, [mid, cwd]);

  useEffect(() => {
    handle.current?.update(latest.current);
  }, [key]);
}
