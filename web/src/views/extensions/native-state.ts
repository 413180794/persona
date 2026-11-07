import { useState, type Dispatch, type SetStateAction } from "react";

/** Optional, page/key-scoped storage. Legacy components keep their original React state. */
export interface NativeLocal {
  /** Stable binding identity; native diff lifetimes must also fence path/revision/epoch. */
  identity?: string;
  get<T>(name: string): T | undefined;
  set<T>(name: string, value: T): void;
  clear(name: string): void;
}
export function scopeLocal(local: NativeLocal | undefined, scope: string): NativeLocal | undefined {
  return local && { get: (name) => local.get(`${scope}/${name}`), set: (name, value) => local.set(`${scope}/${name}`, value), clear: (name) => local.clear(`${scope}/${name}`) };
}
export function useNativeState<T>(local: NativeLocal | undefined, name: string, initial: T | (() => T)): [T, Dispatch<SetStateAction<T>>] {
  const [fallback, setFallback] = useState(initial);
  const stored = local?.get<T>(name);
  const value = stored === undefined ? (local && typeof initial !== "function" ? initial as T : fallback) : stored;
  const set: Dispatch<SetStateAction<T>> = (next) => {
    if (!local) { setFallback(next); return; }
    const before = local.get<T>(name) === undefined ? value : local.get<T>(name)!;
    local.set(name, typeof next === "function" ? (next as (v: T) => T)(before) : next);
  };
  return [value, set];
}
