const allowedTokens = new Set(["source_language", "target_language", "text"]);

export function validateProviderEndpoint(baseUrl: string): {
  error: string;
  warning: string;
} {
  let endpoint: URL;
  try {
    endpoint = new URL(baseUrl);
  } catch {
    return {
      error: "请输入完整的 HTTP / HTTPS 服务地址。",
      warning: "",
    };
  }
  if (
    !endpoint.hostname ||
    endpoint.username ||
    endpoint.password ||
    baseUrl.includes("?") ||
    baseUrl.includes("#") ||
    (endpoint.protocol !== "https:" && endpoint.protocol !== "http:")
  ) {
    return {
      error: "服务地址必须为 HTTP 或 HTTPS，且不能包含凭据、查询参数或片段。",
      warning: "",
    };
  }
  return {
    error: "",
    warning: endpoint.protocol === "http:"
      ? "HTTP 不加密，翻译内容和 API 密钥将以明文发送。"
      : "",
  };
}

export function validatePersona(persona: string): string {
  return Array.from(persona).length > 4000 ? "人设最多 4,000 字符。" : "";
}

export function validateTemplates(
  systemTemplate: string,
  userTemplate: string,
): string {
  const parsed: string[][] = [];
  for (const template of [systemTemplate, userTemplate]) {
    if (Array.from(template).length > 16000)
      return "每个模板最多 16,000 字符。";
    const tokens: string[] = [];
    for (let position = 0; position < template.length;) {
      if (template[position] !== "{" && template[position] !== "}") {
        position++;
        continue;
      }
      if (!template.startsWith("{{", position)) return "模板含有未匹配的括号。";
      const end = template.indexOf("}}", position + 2);
      if (end < 0) return "模板含有未匹配的括号。";
      const name = template.slice(position + 2, end).trim();
      if (!allowedTokens.has(name)) return "模板包含不支持的变量。";
      tokens.push(name);
      position = end + 2;
    }
    parsed.push(tokens);
  }
  if (parsed[0].includes("text")) return "{{text}} 仅可用于用户模板。";
  if (parsed[1].filter((token) => token === "text").length !== 1)
    return "用户模板必须且只能包含一个 {{text}}。";
  if (!parsed.flat().includes("target_language"))
    return "模板必须包含 {{target_language}}。";
  return "";
}

function normalizeHotkeyForComparison(value: string): string {
  const parts = value.trim().toUpperCase().split("+").map((part) => part.trim());
  const key = (parts.pop() ?? "").split("*").map((part) => part.trim()).join("*");
  const modifiers = parts.map((modifier) => {
    switch (modifier) {
      case "CONTROL": return "CTRL";
      case "OPTION": return "ALT";
      case "COMMAND": return "CMD";
      case "SUPER": return "WIN";
      default: return modifier;
    }
  });
  return [...modifiers.sort(), key].join("+");
}

export function validateHotkeys(values: readonly string[], platform: string): string {
  const enabledKeys = values.map(normalizeHotkeyForComparison).filter(Boolean);
  const repeatedKeys = enabledKeys.filter((value) => value.includes("*"));
  if (repeatedKeys.length)
    return "剪贴板连按触发为固定功能，不能用于自定义快捷键。";
  if (new Set(enabledKeys).size !== enabledKeys.length)
    return "快捷键不能重复。";
  if (platform === "windows" && enabledKeys.includes("CTRL+C"))
    return "普通 Ctrl+C 与固定的剪贴板连按触发冲突。";
  if (platform === "darwin" && enabledKeys.includes("CMD+C"))
    return "普通 Command+C 与固定的剪贴板连按触发冲突。";
  return "";
}

export interface KeyInput {
  key: string;
  repeat?: boolean;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
}
export function recordHotkey(
  event: KeyInput,
  isMac: boolean,
): { value?: string; cancel?: boolean; error?: string } {
  if (event.repeat) return {};
  if (event.key === "Escape") return { cancel: true };
  if (event.key === "Delete" || event.key === "Backspace") return { value: "" };
  if (["Control", "Meta", "Alt", "Shift"].includes(event.key)) return {};
  if ((isMac && event.ctrlKey) || (!isMac && event.metaKey))
    return {
      error: isMac
        ? "请使用 Command 而不是 Control。"
        : "请使用 Ctrl 而不是 Windows 键。",
    };
  if (!(event.ctrlKey || event.metaKey || event.altKey))
    return { error: "请包含 Ctrl / Command 或 Alt 修饰键。" };
  let key = event.key.toUpperCase();
  switch (event.key) {
    case " ":
    case "Spacebar":
      key = "Space";
      break;
    case "Enter":
      key = "Return";
      break;
    case "ArrowLeft":
      key = "Left";
      break;
    case "ArrowRight":
      key = "Right";
      break;
    case "ArrowUp":
      key = "Up";
      break;
    case "ArrowDown":
      key = "Down";
      break;
    default:
      if (!/^[a-z0-9]$/i.test(event.key) && !/^F([1-9]|1[0-2])$/.test(event.key))
        return { error: "请选择字母、数字、F1–F12、空格、回车或方向键。" };
  }
  return {
    value: [
      event.ctrlKey && "Ctrl",
      event.metaKey && "Cmd",
      event.altKey && "Alt",
      event.shiftKey && "Shift",
      key,
    ]
      .filter(Boolean)
      .join("+"),
  };
}
