import parser from "@typescript-eslint/parser";
import hooks from "eslint-plugin-react-hooks";

// A correctness lint gate for the paged-list migration. Existing repository tests separately enforce tokens/i18n.
export default [{
  plugins: { "react-hooks": hooks },
  linterOptions: { reportUnusedDisableDirectives: false },
  files: ["**/*.ts", "**/*.tsx"],
  languageOptions: { parser, parserOptions: { ecmaVersion: "latest", sourceType: "module", ecmaFeatures: { jsx: true } } },
  rules: {
    "no-duplicate-case": "error",
    "no-unreachable": "error",
    "no-unsafe-optional-chaining": "error",
    "no-self-compare": "error",
    "valid-typeof": "error",
    "constructor-super": "error",
  },
}];
