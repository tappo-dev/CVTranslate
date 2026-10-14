import { interpolate, useCurrentFrame } from "remotion";
import { SoundCue } from "../components/SoundCue";
import { SceneBackground, SceneNote } from "../components/WindowFrame";
import { fontFamily, palette } from "../theme";

const sourceText = "这个方案能跑，但是有点糙，下周再优化吧。";

const translationCards = [
  {
    providerLabel: "Microsoft Translator",
    personaLabel: "无人设 · No persona",
    output: "This solution works, but it's a bit rough, let's optimize it next week.",
  },
  {
    providerLabel: "OpenAI 兼容模型 · OpenAI-compatible",
    personaLabel: "Serious Developer",
    output: "This approach works but needs refinement. I'll optimize it next week.",
  },
];

// First card on "大模型", second on "人设".
const FIRST_CARD_FRAME = 50;
const CARD_STAGGER_FRAMES = 50;

export const PersonaScene: React.FC = () => {
  const frame = useCurrentFrame();
  const sourceOpacity = interpolate(frame, [0, 10], [0, 1], { extrapolateRight: "clamp" });
  return (
    <SceneBackground>
      <SceneNote chinese="示例译文，实际结果因模型而异" english="Sample output. Results vary by model." />
      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          gap: 40,
          paddingBottom: 150,
          fontFamily,
        }}
      >
        <div
          style={{
            opacity: sourceOpacity,
            fontSize: 44,
            fontWeight: 600,
            padding: "22px 40px",
            borderRadius: 14,
            background: palette.surface,
            border: `1px solid ${palette.border}`,
          }}
        >
          {sourceText}
        </div>
        <div style={{ display: "flex", gap: 40 }}>
          {translationCards.map((card, cardIndex) => {
            const appearFrame = FIRST_CARD_FRAME + cardIndex * CARD_STAGGER_FRAMES;
            const cardProgress = interpolate(frame, [appearFrame, appearFrame + 12], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
            });
            return (
              <div
                key={card.personaLabel}
                style={{
                  width: 720,
                  minHeight: 300,
                  padding: 36,
                  borderRadius: 16,
                  background: palette.surface,
                  border: `1px solid ${palette.border}`,
                  boxShadow: "0 16px 40px rgba(38, 58, 64, 0.10)",
                  opacity: cardProgress,
                  transform: `translateY(${(1 - cardProgress) * 30}px)`,
                }}
              >
                <div style={{ fontSize: 24, color: palette.muted }}>{card.providerLabel}</div>
                <div
                  style={{
                    display: "inline-block",
                    marginTop: 12,
                    padding: "6px 16px",
                    borderRadius: 8,
                    fontSize: 24,
                    fontWeight: 600,
                    background: palette.accentLight,
                    color: palette.accent,
                  }}
                >
                  {card.personaLabel}
                </div>
                <div style={{ marginTop: 24, fontSize: 34, lineHeight: 1.45 }}>{card.output}</div>
              </div>
            );
          })}
        </div>
      </div>
    </SceneBackground>
  );
};
