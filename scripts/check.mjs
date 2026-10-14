import { checkPipeline } from './lib/pipeline.mjs';
import { main } from './lib/process.mjs';

main(async () => {
  if (process.argv.length > 2) throw new Error('Usage: node scripts/check.mjs');
  await checkPipeline();
});
