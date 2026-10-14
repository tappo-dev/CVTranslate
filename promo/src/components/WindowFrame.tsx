import { fontFamily, palette } from "../theme";

export const WindowFrame: React.FC<{
  title: string;
  width: number;
  height: number;
  children: React.ReactNode;
}> = ({ title, width, height, children }) => (
  <div
    style={{
      width,
      height,
      background: palette.surface,
      borderRadius: 16,
      border: `1px solid ${palette.border}`,
      boxShadow: "0 24px 60px rgba(38, 58, 64, 0.16)",
      overflow: "hidden",
      display: "flex",
      flexDirection: "column",
      fontFamily,
    }}
  >
    <div
      style={{
        height: 52,
        display: "flex",
        alignItems: "center",
        gap: 10,
        padding: "0 20px",
        borderBottom: `1px solid ${palette.border}`,
        background: palette.surfaceMuted,
      }}
    >
      {["#e5e9eb", "#e5e9eb", "#e5e9eb"].map((dotColor, dotIndex) => (
        <div key={dotIndex} style={{ width: 14, height: 14, borderRadius: 7, background: dotColor }} />
      ))}
      <div style={{ marginLeft: 12, fontSize: 22, color: palette.textSecondary }}>{title}</div>
    </div>
    <div style={{ flex: 1, position: "relative" }}>{children}</div>
  </div>
);

export const SceneBackground: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <div
    style={{
      position: "absolute",
      inset: 0,
      background: `radial-gradient(circle at 30% 20%, ${palette.accentLight}, ${palette.pageBackground} 60%)`,
      fontFamily,
      color: palette.text,
    }}
  >
    {children}
  </div>
);

export const SceneNote: React.FC<{ chinese: string; english: string }> = ({ chinese, english }) => (
  <div
    style={{
      position: "absolute",
      top: 48,
      right: 64,
      zIndex: 1,
      padding: "10px 18px",
      borderRadius: 10,
      background: palette.warningBackground,
      color: palette.warning,
      fontSize: 22,
      lineHeight: 1.4,
      textAlign: "right",
    }}
  >
    <div>{chinese}</div>
    <div>{english}</div>
  </div>
);
