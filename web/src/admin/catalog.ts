// The admin UI's own strings (src/i18n/locales/<lang>/admin.ts), loaded by initI18n on top of the client catalog:
// the admin pages also use common.* and the shared components' namespaces. Kept here, not in src/i18n, so only the
// admin entry references these chunks (vite.config.ts puts them under admin/assets/).
import type { CatalogLoaders } from "../i18n";

export const adminCatalog: CatalogLoaders = {
  "zh-Hans": () => import("../i18n/locales/zh-Hans/admin"),
  en: () => import("../i18n/locales/en/admin"),
  ja: () => import("../i18n/locales/ja/admin"),
};
