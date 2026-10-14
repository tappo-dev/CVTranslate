import { interpolate, spring, useCurrentFrame, useVideoConfig } from "remotion";
import { Logo } from "../components/Logo";
import { SoundCue } from "../components/SoundCue";
import { SceneBackground } from "../components/WindowFrame";
import { palette } from "../theme";

export const IntroScene: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const logoScale = spring({ frame, fps, config: { damping: 14 } });
  const titleOpacity = interpolate(frame, [12, 30], [0, 1], { extrapolateRight: "clamp" });
  const titleOffset = interpolate(frame, [12, 30], [24, 0], { extrapolateRight: "clamp" });
  return (
    <SceneBackground>
      <SoundCue name="chime" atFrame={2} volume={0.6} />
      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          gap: 36,
          paddingBottom: 120,
        }}
      >
        <div style={{ transform: `scale(${logoScale})` }}>
          <Logo size={220} idPrefix="intro" />
        </div>
        <div style={{ opacity: titleOpacity, transform: `translateY(${titleOffset}px)`, textAlign: "center" }}>
          <div style={{ fontSize: 110, fontWeight: 700, letterSpacing: -2 }}>CVTranslate</div>
          <div style={{ fontSize: 40, color: palette.textSecondary, marginTop: 8 }}>
            Windows / macOS 中英翻译小工具 · Chinese-English translator
          </div>
        </div>
      </div>
    </SceneBackground>
  );
};
