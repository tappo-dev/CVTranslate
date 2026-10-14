import { fontFamily, palette } from "../theme";

const navigationItems = ["翻译工作台", "翻译历史", "Token 报表", "服务提供商", "提示词模板", "应用设置"];

// Simplified re-creation of the real workbench page layout (web/src), not a live capture.
export const WorkbenchMock: React.FC<{
  sourceText: string;
  translatedText: string;
  providerLabel: string;
  templateLabel: string;
  scale?: number;
}> = ({ sourceText, translatedText, providerLabel, templateLabel, scale = 1 }) => (
  <div
    style={{
      width: 1500,
      height: 820,
      transform: `scale(${scale})`,
      display: "flex",
      background: palette.pageBackground,
      borderRadius: 16,
      border: `1px solid ${palette.border}`,
      boxShadow: "0 24px 60px rgba(38, 58, 64, 0.16)",
      overflow: "hidden",
      fontFamily,
      color: palette.text,
    }}
  >
    <div
      style={{
        width: 300,
        background: palette.surface,
        borderRight: `1px solid ${palette.border}`,
        padding: "36px 24px",
      }}
    >
      <div style={{ fontSize: 30, fontWeight: 700, marginBottom: 48 }}>CVTranslate</div>
      {navigationItems.map((navigationLabel, navigationIndex) => (
        <div
          key={navigationLabel}
          style={{
            padding: "16px 20px",
            borderRadius: 10,
            fontSize: 24,
            marginBottom: 8,
            background: navigationIndex === 0 ? palette.accentLight : "transparent",
            color: navigationIndex === 0 ? palette.accent : palette.textSecondary,
            fontWeight: navigationIndex === 0 ? 600 : 400,
          }}
        >
          {navigationLabel}
        </div>
      ))}
    </div>
    <div style={{ flex: 1, padding: "44px 48px" }}>
      <div style={{ fontSize: 44, fontWeight: 700, marginBottom: 32 }}>翻译工作台</div>
      <div
        style={{
          background: palette.surface,
          border: `1px solid ${palette.border}`,
          borderRadius: 14,
          overflow: "hidden",
        }}
      >
        <div
          style={{
            display: "flex",
            justifyContent: "space-around",
            padding: "22px 0",
            borderBottom: `1px solid ${palette.border}`,
            fontSize: 24,
            fontWeight: 600,
          }}
        >
          <span>自动检测</span>
          <span style={{ color: palette.muted }}>⇄</span>
          <span>自动（中 ⇄ 英）</span>
        </div>
        <div style={{ display: "flex", height: 330 }}>
          <div style={{ flex: 1, padding: 28, borderRight: `1px solid ${palette.border}` }}>
            <div style={{ fontSize: 22, color: palette.muted, marginBottom: 16 }}>原文</div>
            <div style={{ fontSize: 28, lineHeight: 1.5 }}>{sourceText}</div>
          </div>
          <div style={{ flex: 1, padding: 28 }}>
            <div style={{ fontSize: 22, color: palette.muted, marginBottom: 16 }}>译文</div>
            <div style={{ fontSize: 28, lineHeight: 1.5 }}>{translatedText}</div>
          </div>
        </div>
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 20,
            padding: "18px 28px",
            borderTop: `1px solid ${palette.border}`,
            fontSize: 22,
          }}
        >
          <span style={{ color: palette.textSecondary }}>自动翻译 · 可能计费</span>
          <span style={{ padding: "10px 18px", borderRadius: 10, background: palette.surfaceMuted }}>
            翻译服务 {providerLabel}
          </span>
          <span style={{ padding: "10px 18px", borderRadius: 10, background: palette.surfaceMuted }}>
            提示词模板 {templateLabel}
          </span>
        </div>
      </div>
    </div>
  </div>
);
