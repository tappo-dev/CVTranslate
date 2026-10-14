import { interpolate, useCurrentFrame } from "remotion";
import type { BilingualLine } from "../copy";
import { fontFamily } from "../theme";

const FADE_FRAMES = 8;

export const Subtitle: React.FC<{ line: BilingualLine; durationInFrames: number }> = ({ line, durationInFrames }) => {
  const frame = useCurrentFrame();
  const opacity = interpolate(
    frame,
    [0, FADE_FRAMES, durationInFrames - FADE_FRAMES, durationInFrames],
    [0, 1, 1, 0],
    { extrapolateLeft: "clamp", extrapolateRight: "clamp" },
  );
  return (
    <div
      style={{
        position: "absolute",
        left: 0,
        right: 0,
        bottom: 56,
        display: "flex",
        justifyContent: "center",
        opacity,
      }}
    >
      <div
        style={{
          background: "rgba(24, 48, 57, 0.86)",
          borderRadius: 14,
          padding: "14px 32px",
          textAlign: "center",
          fontFamily,
          color: "#ffffff",
          maxWidth: 1500,
        }}
      >
        <div style={{ fontSize: 40, fontWeight: 600, lineHeight: 1.35 }}>{line.chinese}</div>
        <div style={{ fontSize: 28, lineHeight: 1.35, color: "#d4e6e6" }}>{line.english}</div>
      </div>
    </div>
  );
};
