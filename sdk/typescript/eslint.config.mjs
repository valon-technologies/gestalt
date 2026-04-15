import tsParser from "@typescript-eslint/parser";
import tsdoc from "eslint-plugin-tsdoc";

export default [
  {
    files: ["src/**/*.ts", "docs/entrypoints/**/*.ts"],
    languageOptions: {
      parser: tsParser,
      parserOptions: {
        ecmaVersion: "latest",
        sourceType: "module",
      },
    },
    plugins: {
      tsdoc,
    },
    rules: {
      "tsdoc/syntax": "error",
    },
  },
];
