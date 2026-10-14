// Synthesizes the promo sound effects with ffmpeg so no third-party audio licenses are involved.
import { spawnSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const outputDirectory = join(dirname(fileURLToPath(import.meta.url)), "..", "public", "sfx");
mkdirSync(outputDirectory, { recursive: true });

const SAMPLE_RATE = 48000;

const soundEffects = [
  {
    name: "key-click",
    source: `aevalsrc='0.55*sin(2*PI*1900*t)*exp(-t*110)+0.35*(random(0)*2-1)*exp(-t*180)':d=0.09:s=${SAMPLE_RATE}`,
  },
  {
    name: "tick",
    source: `aevalsrc='0.35*sin(2*PI*1050*t)*exp(-t*35)':d=0.2:s=${SAMPLE_RATE}`,
  },
  {
    name: "strike",
    source: `aevalsrc='0.4*sin(2*PI*(420-260*t)*t)*exp(-t*5)':d=0.5:s=${SAMPLE_RATE}`,
  },
  {
    name: "whoosh",
    source: `anoisesrc=d=0.5:c=pink:a=0.6:r=${SAMPLE_RATE}`,
    filters: "bandpass=f=1400:width_type=h:w=1800,afade=t=in:d=0.22,afade=t=out:st=0.22:d=0.28",
  },
  {
    name: "pop",
    source: `aevalsrc='0.35*sin(2*PI*880*t)*exp(-t*14)+0.22*sin(2*PI*1320*t)*exp(-t*20)':d=0.4:s=${SAMPLE_RATE}`,
  },
  {
    name: "chime",
    source: `aevalsrc='0.2*(sin(2*PI*523.25*t)+sin(2*PI*659.25*t)+sin(2*PI*783.99*t)+0.5*sin(2*PI*1046.5*t))*exp(-t*2.2)':d=1.8:s=${SAMPLE_RATE}`,
    filters: "afade=t=in:d=0.01",
  },
];

for (const soundEffect of soundEffects) {
  const outputPath = join(outputDirectory, `${soundEffect.name}.wav`);
  const filterArguments = soundEffect.filters ? ["-af", soundEffect.filters] : [];
  const ffmpegResult = spawnSync(
    "ffmpeg",
    ["-v", "error", "-y", "-f", "lavfi", "-i", soundEffect.source, ...filterArguments, "-ac", "2", outputPath],
    { stdio: "inherit" },
  );
  if (ffmpegResult.status !== 0) {
    throw new Error(`ffmpeg failed for ${soundEffect.name}`);
  }
  console.log(`generated ${outputPath}`);
}
