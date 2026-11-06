// A stylesheet's page color tokens, built by page-tokens.ts (the Vite plugin beside vite.config.ts).
declare module "*.css?page-tokens" {
  const tokens: { light: Record<string, string>; dark: Record<string, string> };
  export default tokens;
}
