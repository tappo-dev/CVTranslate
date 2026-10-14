import { mkdir, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { renderIconAssets, verifyIconAssets } from './lib/icons.mjs';
import { main, rootDirectory } from './lib/process.mjs';

main(async () => {
  const argumentsList = process.argv.slice(2);
  if (argumentsList.length > 1 || (argumentsList.length === 1 && argumentsList[0] !== '--check')) {
    throw new Error('Usage: node scripts/icons.mjs [--check]');
  }
  if (argumentsList[0] === '--check') {
    await verifyIconAssets();
    console.log('SVG and native tray assets are in sync.');
    return;
  }
  for (const [filename, contents] of await renderIconAssets()) {
    const destination = join(rootDirectory, filename);
    await mkdir(dirname(destination), { recursive: true });
    await writeFile(destination, contents);
    console.log(`Generated ${filename}`);
  }
});
