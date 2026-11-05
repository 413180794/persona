// The hub admin UI's namespace files (admin-*.json) of this language, keyed "<namespace>.<name>". Loaded on top of
// ./index.ts by admin/main.tsx only, so the web client's language chunk does not carry them.
import { flatten } from "../flatten";

const files = import.meta.glob<Record<string, string>>("./admin-*.json", { eager: true, import: "default" });

export default flatten(files);
