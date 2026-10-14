import { interpolate, useCurrentFrame } from "remotion";
import { SoundCue } from "../components/SoundCue";
import { SceneBackground } from "../components/WindowFrame";
import { palette } from "../theme";

const privacyPoints = [
  { chinese: "本地保存", english: "Stored locally", detail: "设置与历史 · Settings & history" },
  { chinese: "系统凭据存储", english: "OS credential store", detail: "API Key" },
  { chinese: "无账号、无遥测", english: "No account, no telemetry", detail: "127.0.0.1" },
];

export const PrivacyScene: React.FC = () => {
  const frame = useCurrentFrame();
  // The footnote appears when the narration reaches "翻译内容会发送给你选择的服务".
  const footnoteOpacity = interpolate(frame, [150, 162], [0, 1], { extrapolateLeft: "clamp", extrapolateRight: "clamp" });
  return (
    <SceneBackground>
      {privacyPoints.map((point, pointIndex) => (
        <SoundCue key={`tick-${point.english}`} name="tick" atFrame={pointIndex * 10} volume={0.5} />
      ))}
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
        }}
      >
        <div style={{ display: "flex", gap: 36 }}>
          {privacyPoints.map((point, pointIndex) => {
            const appearFrame = pointIndex * 10;
            const pointOpacity = interpolate(frame, [appearFrame, appearFrame + 10], [0, 1], {
              extrapolateLeft: "clamp",
              extrapolateRight: "clamp",
            });
            return (
              <div
                key={point.english}
                style={{
                  width: 440,
                  padding: "40px 32px",
                  borderRadius: 16,
                  background: palette.surface,
                  border: `1px solid ${palette.border}`,
                  textAlign: "center",
                  opacity: pointOpacity,
                }}
              >
                <div style={{ fontSize: 44, fontWeight: 700, color: palette.accent }}>{point.chinese}</div>
                <div style={{ fontSize: 28, marginTop: 8 }}>{point.english}</div>
                <div style={{ fontSize: 24, marginTop: 16, color: palette.muted }}>{point.detail}</div>
              </div>
            );
          })}
        </div>
        <div style={{ opacity: footnoteOpacity, fontSize: 26, color: palette.warning, textAlign: "center", lineHeight: 1.5 }}>
          <div>翻译文本会发送给所选服务；历史记录未加密</div>
          <div>Text is sent to the selected service. History is stored unencrypted.</div>
        </div>
      </div>
    </SceneBackground>
  );
};
