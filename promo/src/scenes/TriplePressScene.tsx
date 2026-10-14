import { interpolate, useCurrentFrame } from "remotion";
import { CopyKeycap } from "../components/Keycap";
import { SoundCue } from "../components/SoundCue";
import { SceneBackground, SceneNote, WindowFrame } from "../components/WindowFrame";
import { palette } from "../theme";

// Timed to the narration: presses land on "连按三次", replacement on "直接替换刚才的中文".
export const TRIPLE_PRESS_FRAMES = [130, 139, 148];
const REPLACE_FRAME = 215;

const sourceSentence = "这个函数在空数组时会报错，能帮我加一个边界检查吗？";
const translatedSentence = "This function throws on an empty array. Could you add a bounds check?";

export const TriplePressScene: React.FC = () => {
  const frame = useCurrentFrame();
  const selectionOpacity = interpolate(frame, [6, 14], [0, 1], { extrapolateRight: "clamp" });
  const isTranslating = frame >= TRIPLE_PRESS_FRAMES[2] && frame < REPLACE_FRAME;
  const hasReplaced = frame >= REPLACE_FRAME;
  const replaceFlash = interpolate(frame, [REPLACE_FRAME, REPLACE_FRAME + 20], [1, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const spinnerRotation = (frame * 12) % 360;
  return (
    <SceneBackground>
      <SceneNote chinese="示意动画；粘贴前请勿切换窗口" english="Illustration. Stay in the window until it pastes." />
      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          gap: 48,
          paddingBottom: 150,
        }}
      >
        <WindowFrame title="Chat" width={1300} height={360}>
          <div style={{ padding: "40px 56px", fontSize: 34, lineHeight: 1.6 }}>
            <div style={{ fontSize: 24, color: palette.muted }}>Reply…</div>
            <div
              style={{
                marginTop: 16,
                padding: "20px 24px",
                borderRadius: 14,
                border: `1px solid ${palette.border}`,
                background: palette.surfaceMuted,
              }}
            >
              {hasReplaced ? (
                <span
                  style={{
                    background: `rgba(185, 224, 219, ${replaceFlash})`,
                    borderRadius: 6,
                  }}
                >
                  {translatedSentence}
                </span>
              ) : (
                <span
                  style={{
                    background: `rgba(185, 224, 219, ${selectionOpacity})`,
                    borderRadius: 6,
                  }}
                >
                  {sourceSentence}
                </span>
              )}
              <span
                style={{
                  display: "inline-block",
                  width: 3,
                  height: 38,
                  marginLeft: 2,
                  verticalAlign: "middle",
                  background: palette.text,
                  opacity: Math.floor(frame / 15) % 2 === 0 ? 1 : 0,
                }}
              />
            </div>
          </div>
          {isTranslating && (
            <div
              style={{
                position: "absolute",
                right: 32,
                bottom: 28,
                display: "flex",
                alignItems: "center",
                gap: 12,
                fontSize: 24,
                color: palette.accent,
              }}
            >
              <div
                style={{
                  width: 24,
                  height: 24,
                  borderRadius: 12,
                  border: `3px solid ${palette.accentLight}`,
                  borderTopColor: palette.accent,
                  transform: `rotate(${spinnerRotation}deg)`,
                }}
              />
              翻译中 · Translating
            </div>
          )}
        </WindowFrame>
        <CopyKeycap pressFrames={TRIPLE_PRESS_FRAMES} />
      </div>
    </SceneBackground>
  );
};
