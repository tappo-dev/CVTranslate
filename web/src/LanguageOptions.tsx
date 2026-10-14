import type { AppState } from "./types";

export function LanguageOptions({
  state,
  source = false,
  automaticTarget = false,
}: {
  state: AppState;
  source?: boolean;
  automaticTarget?: boolean;
}) {
  return (
    <>
      {source && <option value="auto">自动检测</option>}
      {automaticTarget && <option value="auto">自动（中 ⇄ 英）</option>}
      {state.languages.map((language) => (
        <option key={language.code} value={language.code}>
          {language.code === "en"
            ? "英语"
            : language.code === "zh-CN"
              ? "简体中文"
              : language.name}
        </option>
      ))}
    </>
  );
}
