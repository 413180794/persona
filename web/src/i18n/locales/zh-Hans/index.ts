// The web client's namespace files (<namespace>.json, flat "name": "text") of this language, keyed "<namespace>.<name>".
// The hub admin UI's admin-*.json are left out: they are in ./admin.ts, which only admin/main.tsx loads.
import { flatten } from "../flatten";

const files = import.meta.glob<Record<string, string>>(["./*.json", "!./admin-*.json"], { eager: true, import: "default" });

export default flatten(files);
