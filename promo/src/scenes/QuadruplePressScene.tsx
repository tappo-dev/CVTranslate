import { interpolate, spring, useCurrentFrame, useVideoConfig } from "remotion";
import { CopyKeycap } from "../components/Keycap";
import { SoundCue } from "../components/SoundCue";
import { SceneBackground, SceneNote } from "../components/WindowFrame";
import { WorkbenchMock } from "../components/WorkbenchMock";

const QUADRUPLE_PRESS_FRAMES = [10, 19, 28, 37];
const WORKBENCH_OPEN_FRAME = 50;
const TRANSLATION_DONE_FRAME = 80;

const sourceText = "The cache is invalidated whenever the config file changes.";
const translatedText = "每当配置文件发生变化时，缓存都会失效。";

export const QuadruplePressScene: React.FC = () => {
  const frame = useCurrentFrame();
  const { fps } = useVideoConfig();
  const workbenchEntrance = spring({ frame: frame - WORKBENCH_OPEN_FRAME, fps, config: { damping: 16 } });
  const keycapOpacity = interpolate(frame, [WORKBENCH_OPEN_FRAME - 6, WORKBENCH_OPEN_FRAME], [1, 0], {
    extrapolateLeft: "clamp",
    extrapolateRight: "clamp",
  });
  const isTranslationVisible = frame >= TRANSLATION_DONE_FRAME;
  return (
    <SceneBackground>
      <SceneNote chinese="示意画面，非实际录屏" english="Illustration, not a screen recording" />
      <div
        style={{
          position: "absolute",
          inset: 0,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          paddingBottom: 150,
        }}
      >
        {frame < WORKBENCH_OPEN_FRAME ? (
          <div style={{ opacity: keycapOpacity }}>
            <CopyKeycap pressFrames={QUADRUPLE_PRESS_FRAMES} />
          </div>
        ) : (
          <div style={{ transform: `translateY(${(1 - workbenchEntrance) * 80}px)`, opacity: workbenchEntrance }}>
            <WorkbenchMock
              scale={0.85}
              sourceText={sourceText}
              translatedText={isTranslationVisible ? translatedText : "…"}
              providerLabel="Microsoft Translator"
              templateLabel="General translation"
            />
          </div>
        )}
      </div>
    </SceneBackground>
  );
};
