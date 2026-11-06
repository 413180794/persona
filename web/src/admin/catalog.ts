// The admin UI's own strings (src/i18n/locales/<lang>/admin-*.json), loaded by initI18n on top of the client catalog:
// the admin pages also use common.* and the shared components' namespaces. Kept here, not in src/i18n, so only the
// admin entry references these chunks (vite.config.ts puts them under admin/assets/).
import { byLanguage, type CatalogModule } from "../i18n";

export const adminCatalog = byLanguage(import.meta.glob<CatalogModule>("../i18n/locales/*/_meta.json", { query: "?catalog=admin" }));
