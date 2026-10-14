import { nextReleaseVersion } from './lib/release.mjs';
import { main, readPackage, run } from './lib/process.mjs';

main(async () => {
  if (process.argv.length > 2) throw new Error('Usage: node scripts/release-version.mjs');
  const metadata = await readPackage();
  const tagListing = await run('git', ['tag', '--list', 'v*'], { capture: true });
  const existingTags = tagListing.split(/\r?\n/).filter(Boolean);
  console.log(nextReleaseVersion(metadata.version, existingTags));
});
