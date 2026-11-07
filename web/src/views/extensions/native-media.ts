import { createContext } from "react";
import type { ImageSource } from "../../lib/extension-wire";

/** Owner-bound native resolver only. No endpoint synthesis or credential discovery. */
export type NativeMedia = (source: Readonly<ImageSource>) => string | undefined;
export const NativeMediaContext = createContext<NativeMedia | undefined>(undefined);
