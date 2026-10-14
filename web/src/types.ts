export interface ProviderConfig {
  id: string;
  name: string;
  type: string;
  baseUrl: string;
  model: string;
  timeoutSeconds: number;
  enabled: boolean;
  hasApiKey: boolean;
}
export interface Prompt {
  id: string;
  name: string;
  systemTemplate: string;
  userTemplate: string;
  persona: string;
  revision: number;
  builtIn: boolean;
}
export interface Hotkeys {
  openUi: string;
}
export type SpeechVoiceLanguage = "en" | "zh-CN";
export type SpeechEngine = "edge" | "web-speech";
export interface SpeechSettings {
  engine: SpeechEngine;
  edgeVoices: Record<SpeechVoiceLanguage, string>;
  webSpeechVoices: Record<SpeechVoiceLanguage, string>;
}
export interface SpeechVoice {
  id: string;
  name: string;
  language: SpeechVoiceLanguage;
}
export interface Settings {
  defaultProviderId: string;
  defaultPromptId: string;
  sourceLanguage: string;
  targetLanguage: string;
  port: number;
  hotkeys: Hotkeys;
  speech: SpeechSettings;
}
export interface ShortcutStatus {
  action: string;
  stage: string;
  message: string;
  occurredAt: string;
  taskId?: string;
}
export interface AppState {
  providers: ProviderConfig[];
  providerTypes: {
    type: string;
    name: string;
    defaultModel: string;
    supportsPrompts: boolean;
    supportsApiKey: boolean;
    configurableEndpoint: boolean;
    requiresModel: boolean;
  }[];
  prompts: Prompt[];
  settings: Settings;
  speechVoices: SpeechVoice[];
  languages: { code: string; name: string }[];
  platform: {
    os: string;
    hotkeysAvailable: boolean;
    pasteAvailable: boolean;
    message: string;
    shortcut?: ShortcutStatus;
  };
  version: string;
  development: boolean;
}
export type UpdateStatus =
  "update_available" | "up_to_date" | "no_release" | "not_comparable";
export interface UpdateCheckResult {
  currentVersion: string;
  latestVersion: string;
  status: UpdateStatus;
  releaseUrl: string;
}
export type TaskStatus =
  "queued" | "running" | "succeeded" | "failed" | "cancelled" | "interrupted";
export interface TranslationInput {
  text: string;
  sourceLanguage: string;
  targetLanguage: string;
  providerId: string;
  promptId: string;
  entry: "web";
}
export interface Task {
  id: string;
  text: string;
  translation: string;
  sourceLanguage: string;
  targetLanguage: string;
  entry: string;
  status: TaskStatus;
  providerSnapshot: ProviderConfig;
  promptSnapshot: Prompt;
  errorCode: string;
  errorMessage: string;
  pasteStatus: string;
  createdAt: string;
  updatedAt: string;
  durationMs: number;
  tokenUsage?: TokenUsage;
  reusedFromTaskId?: string;
}

export interface TokenUsage {
  promptTokens: number;
  completionTokens: number;
  totalTokens: number;
}

export interface TokenUsageSummary extends TokenUsage {
  requestCount: number;
}

export interface DailyTokenUsage extends TokenUsageSummary {
  date: string;
}

export interface TokenReport {
  today: TokenUsageSummary;
  last7Days: TokenUsageSummary;
  last30Days: TokenUsageSummary;
  daily: DailyTokenUsage[];
}
export interface TaskPage {
  items: Task[];
  total: number;
  page: number;
  pageSize: number;
}
export const statusLabels: Record<TaskStatus, string> = {
  queued: "排队中",
  running: "翻译中",
  succeeded: "已完成",
  failed: "失败",
  cancelled: "已取消",
  interrupted: "已中断",
};
export const isActive = (task: Task | null) =>
  task?.status === "queued" || task?.status === "running";
