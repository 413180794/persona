// Every namespace file (<namespace>.json, flat "name": "text") of this language, keyed "<namespace>.<name>".
import { flatten } from "../flatten";

const files = import.meta.glob<Record<string, string>>("./*.json", { eager: true, import: "default" });

export default flatten(files);
