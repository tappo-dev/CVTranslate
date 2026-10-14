import { Composition } from "remotion";
import { Promo, PROMO_DURATION_IN_FRAMES } from "./Promo";
import { FRAMES_PER_SECOND, VIDEO_HEIGHT, VIDEO_WIDTH } from "./theme";

export const RemotionRoot: React.FC = () => (
  <Composition
    id="CVTranslatePromo"
    component={Promo}
    durationInFrames={PROMO_DURATION_IN_FRAMES}
    fps={FRAMES_PER_SECOND}
    width={VIDEO_WIDTH}
    height={VIDEO_HEIGHT}
  />
);
