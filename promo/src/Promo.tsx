import { AbsoluteFill, Audio, Sequence, staticFile } from "remotion";
import { subtitles, type BilingualLine } from "./copy";
import { Subtitle } from "./components/Subtitle";
import narrationDurations from "./narration-durations.json";
import { IntroScene } from "./scenes/IntroScene";
import { OutroScene } from "./scenes/OutroScene";
import { PersonaScene } from "./scenes/PersonaScene";
import { PrivacyScene } from "./scenes/PrivacyScene";
import { ProblemScene } from "./scenes/ProblemScene";
import { QuadruplePressScene } from "./scenes/QuadruplePressScene";
import { TriplePressScene } from "./scenes/TriplePressScene";
import { FRAMES_PER_SECOND } from "./theme";

type NarrationId = keyof typeof narrationDurations;
type PromoScene = { id: NarrationId; Component: React.FC; minimumSeconds: number; subtitle: BilingualLine };

const promoScenes: PromoScene[] = [
  { id: "intro", Component: IntroScene, minimumSeconds: 3.5, subtitle: subtitles.intro },
  { id: "problem", Component: ProblemScene, minimumSeconds: 4, subtitle: subtitles.problem },
  { id: "triplePress", Component: TriplePressScene, minimumSeconds: 5.5, subtitle: subtitles.triplePress },
  { id: "quadruplePress", Component: QuadruplePressScene, minimumSeconds: 5, subtitle: subtitles.quadruplePress },
  { id: "providers", Component: PersonaScene, minimumSeconds: 6, subtitle: subtitles.providers },
  { id: "privacy", Component: PrivacyScene, minimumSeconds: 4.5, subtitle: subtitles.privacy },
  { id: "outro", Component: OutroScene, minimumSeconds: 5, subtitle: subtitles.outro },
];

const NARRATION_LEAD_IN_SECONDS = 0.3;
const NARRATION_TAIL_SECONDS = 0.7;

const toFrames = (seconds: number) => Math.round(seconds * FRAMES_PER_SECOND);

const sceneDurationInFrames = (scene: PromoScene) =>
  toFrames(
    Math.max(scene.minimumSeconds, NARRATION_LEAD_IN_SECONDS + narrationDurations[scene.id] + NARRATION_TAIL_SECONDS),
  );

export const PROMO_DURATION_IN_FRAMES = promoScenes.reduce((total, scene) => total + sceneDurationInFrames(scene), 0);

export const Promo: React.FC = () => {
  let sceneStartFrame = 0;
  return (
    <AbsoluteFill>
      {promoScenes.map((scene) => {
        const { id, Component, subtitle } = scene;
        const durationInFrames = sceneDurationInFrames(scene);
        const startFrame = sceneStartFrame;
        sceneStartFrame += durationInFrames;
        return (
          <Sequence key={id} from={startFrame} durationInFrames={durationInFrames}>
            <Component />
            <Subtitle line={subtitle} durationInFrames={durationInFrames} />
            <Sequence from={toFrames(NARRATION_LEAD_IN_SECONDS)} layout="none">
              <Audio src={staticFile(`narration/${id}.mp3`)} />
            </Sequence>
          </Sequence>
        );
      })}
    </AbsoluteFill>
  );
};
