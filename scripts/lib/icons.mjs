import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { Resvg } from '@resvg/resvg-js';
import { rootDirectory } from './process.mjs';

export async function renderIconAssets() {
  const source = (await readFile(join(rootDirectory, 'web/public/icon.svg'), 'utf8')).replaceAll('\r\n', '\n');
  const definitionsEnd = source.indexOf('</defs>');
  if (definitionsEnd === -1 || !source.includes('viewBox="0 0 32 32"')) {
    throw new Error('Icon source must keep its <defs> block and 32-unit viewBox.');
  }
  // AppKit tints the alpha mask. Paint every visible shape black and keep the
  // mask cutouts in <defs>, rather than hand-drawing a second version.
  const template = source.slice(0, definitionsEnd)
    + source.slice(definitionsEnd).replace(/fill="#[0-9a-f]{3,6}"/g, 'fill="#000"');
  const render = (svg, width) => Buffer.from(new Resvg(svg, {
    fitTo: { mode: 'width', value: width },
    font: { loadSystemFonts: false },
  }).render().asPng());
  return new Map([
    ['web/public/icon-template.svg', Buffer.from(template)],
    ['internal/platform/icons/tray-windows.png', render(source, 64)],
    ['internal/platform/icons/tray-macos.png', render(template, 36)],
  ]);
}

export async function verifyIconAssets() {
  for (const [filename, expected] of await renderIconAssets()) {
    let actual;
    try { actual = await readFile(join(rootDirectory, filename)); } catch (error) {
      if (error.code !== 'ENOENT') throw error;
    }
    // SVG line endings may be normalized by Git on Windows.
    if (filename.endsWith('.svg') && actual) actual = Buffer.from(actual.toString('utf8').replaceAll('\r\n', '\n'));
    if (!actual?.equals(expected)) throw new Error(`Stale or missing icon asset: ${filename}. Run npm run icons.`);
  }
}
