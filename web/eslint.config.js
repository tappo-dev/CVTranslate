import javascript from "@eslint/js";
import typescript from "typescript-eslint";
import hooks from "eslint-plugin-react-hooks";

export default typescript.config(
  { ignores: ["dist/**"] },
  javascript.configs.recommended,
  ...typescript.configs.recommended,
  {
    files: ["src/**/*.{ts,tsx}"],
    plugins: { "react-hooks": hooks },
    rules: {
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "warn",
    },
  },
);
