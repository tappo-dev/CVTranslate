import { interpolate, spring, useCurrentFrame, useVideoConfig } from "remotion";
import { repositoryUrl } from "../copy";
import { Logo } from "../components/Logo";
import { SoundCue } from "../components/SoundCue";
import { SceneBackground } from "../components/WindowFrame";
import { palette } from "../theme";

const facts = ["Windows / macOS", "连按 Ctrl+C 翻译 · Press Ctrl+C to translate"];

export const OutroScene: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const logoScale = spring({ frame, fps, config: { damping: 14 } });
  const detailOpacity = interpolate(frame, [14, 28], [0, 1], { extrapolateRight: "clamp" });
  return (
    <SceneBackground>
      <SoundCue name="chime" atFrame={2} volume={0.7} />
      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          gap: 32,
          paddingBottom: 150,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 32, transform: `scale(${logoScale})` }}>
          <Logo size={140} idPrefix="outro" />
          <div style={{ fontSize: 96, fontWeight: 700, letterSpacing: -2 }}>CVTranslate</div>
        </div>
        <div style={{ opacity: detailOpacity, display: "flex", flexDirection: "column", alignItems: "center", gap: 28 }}>
          <div style={{ display: "flex", gap: 20 }}>
            {facts.map((fact) => (
              <div
                key={fact}
                style={{
                  padding: "10px 22px",
                  borderRadius: 999,
                  fontSize: 28,
                  background: palette.accentLight,
                  color: palette.accent,
                  fontWeight: 600,
                }}
              >
                {fact}
              </div>
            ))}
          </div>
          <div
            style={{
              fontSize: 44,
              fontWeight: 600,
              padding: "18px 40px",
              borderRadius: 14,
              background: palette.text,
              color: "#ffffff",
            }}
          >
            {repositoryUrl}
          </div>
          <div style={{ fontSize: 24, color: palette.muted, textAlign: "center", lineHeight: 1.5 }}>
            <div>第三方翻译与模型服务费用自理</div>
            <div>Third-party translation and model fees are yours.</div>
          </div>
        </div>
      </div>
    </SceneBackground>
  );
};
