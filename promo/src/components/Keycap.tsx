import { interpolate, useCurrentFrame } from "remotion";
import { fontFamily, palette } from "../theme";
import { SoundCue } from "./SoundCue";

const PRESS_FRAMES = 6;

// A Ctrl+C keycap that dips on each frame listed in pressFrames.
export const CopyKeycap: React.FC<{ pressFrames: number[]; label?: string }> = ({
  pressFrames,
  label = "Ctrl + C",
}) => {
  const frame = useCurrentFrame();
  const pressDepth = Math.max(
    0,
    ...pressFrames.map((pressFrame) =>
      interpolate(frame, [pressFrame, pressFrame + 2, pressFrame + PRESS_FRAMES], [0, 1, 0], {
        extrapolateLeft: "clamp",
        extrapolateRight: "clamp",
      }),
    ),
  );
  const pressCount = pressFrames.filter((pressFrame) => frame >= pressFrame).length;
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 24, fontFamily }}>
      {pressFrames.map((pressFrame) => (
        <SoundCue key={`click-${pressFrame}`} name="key-click" atFrame={pressFrame} volume={0.9} />
      ))}
      <div
        style={{
          padding: "18px 34px",
          borderRadius: 14,
          background: palette.surface,
          border: `2px solid ${palette.border}`,
          boxShadow: `0 ${8 - pressDepth * 6}px 0 ${palette.border}`,
          transform: `translateY(${pressDepth * 6}px)`,
          fontSize: 40,
          fontWeight: 600,
          color: palette.text,
        }}
      >
        {label}
      </div>
      <div style={{ display: "flex", gap: 10 }}>
        {pressFrames.map((pressFrame, pressIndex) => (
          <div
            key={pressFrame}
            style={{
              width: 22,
              height: 22,
              borderRadius: 11,
              background: pressIndex < pressCount ? palette.accent : palette.border,
            }}
          />
        ))}
      </div>
      <div style={{ fontSize: 40, fontWeight: 700, color: palette.accent, minWidth: 60 }}>
        {pressCount > 0 ? `×${pressCount}` : ""}
      </div>
    </div>
  );
};
