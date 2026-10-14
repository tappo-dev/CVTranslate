import { interpolate, useCurrentFrame } from "remotion";
import { SoundCue } from "../components/SoundCue";
import { SceneBackground } from "../components/WindowFrame";
import { palette } from "../theme";

const tediousSteps = [
  { chinese: "复制", english: "Copy" },
  { chinese: "切到翻译器", english: "Switch app" },
  { chinese: "粘贴、等待", english: "Paste, wait" },
  { chinese: "复制译文", english: "Copy result" },
  { chinese: "切回、粘贴", english: "Switch back, paste" },
];

const STEP_INTERVAL_FRAMES = 22;

export const ProblemScene: React.FC = () => {
  const frame = useCurrentFrame();
  const strikeStartFrame = tediousSteps.length * STEP_INTERVAL_FRAMES + 10;
  const strikeProgress = interpolate(frame, [strikeStartFrame, strikeStartFrame + 14], [0, 1], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  return (
    <SceneBackground>
      {tediousSteps.map((step, stepIndex) => (
        <SoundCue key={`tick-${step.english}`} name="tick" atFrame={stepIndex * STEP_INTERVAL_FRAMES} volume={0.5} />
      ))}
      <SoundCue name="strike" atFrame={strikeStartFrame} volume={0.8} />
      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          paddingBottom: 140,
        }}
      >
        <div style={{ position: "relative", display: "flex", alignItems: "center", gap: 20 }}>
          {tediousSteps.map((step, stepIndex) => {
            const appearFrame = stepIndex * STEP_INTERVAL_FRAMES;
            const stepOpacity = interpolate(frame, [appearFrame, appearFrame + 8], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
            });
            return (
              <div key={step.english} style={{ display: "flex", alignItems: "center", gap: 20, opacity: stepOpacity }}>
                <div
                  style={{
                    width: 240,
                    padding: "26px 0",
                    borderRadius: 16,
                    background: palette.surface,
                    border: `1px solid ${palette.border}`,
                    textAlign: "center",
                  }}
                >
                  <div style={{ fontSize: 36, fontWeight: 600 }}>{step.chinese}</div>
                  <div style={{ fontSize: 24, color: palette.muted, marginTop: 6 }}>{step.english}</div>
                </div>
                {stepIndex < tediousSteps.length - 1 && (
                  <div style={{ fontSize: 40, color: palette.muted }}>→</div>
                )}
              </div>
            );
          })}
          <div
            style={{
              position: "absolute",
              left: -20,
              top: "50%",
              height: 8,
              borderRadius: 4,
              background: palette.danger,
              width: `calc(${strikeProgress * 100}% + ${strikeProgress * 40}px)`,
            }}
          />
        </div>
      </div>
    </SceneBackground>
  );
};
