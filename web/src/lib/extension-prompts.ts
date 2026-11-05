// Pure presentation of declarative quick prompts: selecting a row fills the composer, never sends it.
export interface QuickPrompt {
  extension: string;
  extension_name: string;
  id: string;
  title: string;
  text: string;
}
export interface ComposerCommand {
  word: string;
  desc: string;
  text: string;
  confirm?: string;
  key?: string;
}
export function promptCommands(prompts: QuickPrompt[]): ComposerCommand[] {
  return prompts.map((p) => ({
    key: `extension:${p.extension}/${p.id}`,
    word: `/${p.title.replace(/\s+/g, "")}`,
    desc: p.extension_name,
    text: p.text,
  }));
}
export const EXTENSIONS_CHANGED = "persona:extensions-changed";
