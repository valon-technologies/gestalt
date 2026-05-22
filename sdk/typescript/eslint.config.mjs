import tsParser from "@typescript-eslint/parser";
import tsdoc from "eslint-plugin-tsdoc";

export default [
  {
    ignores: ["src/internal/gen/**/*.ts"],
  },
  {
    files: ["src/**/*.ts", "docs/entrypoints/**/*.ts"],
    languageOptions: {
      parser: tsParser,
      parserOptions: {
        ecmaVersion: "latest",
        sourceType: "module",
      },
    },
    apps: {
      tsdoc,
    },
    rules: {
      "tsdoc/syntax": "error",
    },
  },
];
