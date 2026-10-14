import { Audio, Sequence, staticFile } from "remotion";

export type SoundEffectName = "key-click" | "tick" | "strike" | "whoosh" | "pop" | "chime";

export const SoundCue: React.FC<{ name: SoundEffectName; atFrame: number; volume?: number }> = ({
  name,
  atFrame,
  volume = 1,
}) => (
  <Sequence from={atFrame} layout="none">
    <Audio src={staticFile(`sfx/${name}.wav`)} volume={volume} />
  </Sequence>
);
