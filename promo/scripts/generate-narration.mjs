// Synthesizes narration through a locally running CVTranslate instance's Edge read-aloud endpoint.
// Only the narration script is sent (to Microsoft, via the app); the voice is the app's saved zh-CN Edge voice.
import { spawnSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const promoDirectory = join(dirname(fileURLToPath(import.meta.url)), "..");
const applicationOrigin = process.env.CVTRANSLATE_ORIGIN ?? "http://127.0.0.1:19328";
const requiredVoiceFragment = "Xiaoxiao";

const narrationLines = JSON.parse(readFileSync(join(promoDirectory, "src", "narration.json"), "utf8"));
const outputDirectory = join(promoDirectory, "public", "narration");
mkdirSync(outputDirectory, { recursive: true });

const stateResponse = await fetch(`${applicationOrigin}/api/state`);
if (!stateResponse.ok) {
  throw new Error(`Could not read ${applicationOrigin}/api/state (HTTP ${stateResponse.status}). Is CVTranslate running?`);
}
const speechSettings = (await stateResponse.json()).settings.speech;
const savedChineseVoice = speechSettings.edgeVoices["zh-CN"];
if (speechSettings.engine !== "edge" || !savedChineseVoice.includes(requiredVoiceFragment)) {
  throw new Error(`Saved speech must be Edge with a ${requiredVoiceFragment} zh-CN voice; found ${speechSettings.engine} / ${savedChineseVoice}.`);
}
console.log(`Using ${applicationOrigin} with voice ${savedChineseVoice}`);

const narrationDurations = {};
for (const [sceneId, narrationText] of Object.entries(narrationLines)) {
  const speechResponse = await fetch(`${applicationOrigin}/api/speech`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Origin: applicationOrigin },
    body: JSON.stringify({ text: narrationText, language: "zh-CN" }),
  });
  if (!speechResponse.ok || speechResponse.headers.get("content-type") !== "audio/mpeg") {
    throw new Error(`Speech failed for ${sceneId}: HTTP ${speechResponse.status} ${await speechResponse.text()}`);
  }
  const audioPath = join(outputDirectory, `${sceneId}.mp3`);
  writeFileSync(audioPath, Buffer.from(await speechResponse.arrayBuffer()));

  const probeResult = spawnSync(
    "ffprobe",
    ["-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", audioPath],
    { encoding: "utf8" },
  );
  if (probeResult.status !== 0) {
    throw new Error(`ffprobe failed for ${audioPath}`);
  }
  narrationDurations[sceneId] = Number(probeResult.stdout.trim());
  console.log(`${sceneId}: ${narrationDurations[sceneId].toFixed(2)}s`);
}

writeFileSync(join(promoDirectory, "src", "narration-durations.json"), `${JSON.stringify(narrationDurations, null, 2)}\n`);
