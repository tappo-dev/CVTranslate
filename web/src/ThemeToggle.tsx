import { useId, useLayoutEffect, useState } from "react";
import { Moon, Sun } from "lucide-react";
import { useFeedback } from "./feedback";
import { applyTheme, readTheme, saveTheme } from "./theme";

export function ThemeToggle() {
  const [theme, setTheme] = useState(readTheme);
  const tooltipId = useId();
  const { notify } = useFeedback();
  const nextTheme = theme === "light" ? "dark" : "light";
  const label = nextTheme === "dark" ? "切换到暗色模式" : "切换到亮色模式";

  useLayoutEffect(() => applyTheme(theme), [theme]);

  return (
    <button
      type="button"
      className="topbar-icon-button tooltip-trigger"
      aria-label={label}
      aria-describedby={tooltipId}
      data-translation-control
      onClick={() => {
        setTheme(nextTheme);
        try {
          saveTheme(nextTheme);
        } catch {
          notify("主题已切换，无法记住选择", true);
        }
      }}
    >
      {nextTheme === "dark" ? <Moon size={20} aria-hidden="true" /> : <Sun size={20} aria-hidden="true" />}
      <span className="control-tooltip" id={tooltipId} role="tooltip">
        {label}
      </span>
    </button>
  );
}
