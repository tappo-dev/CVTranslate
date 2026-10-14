import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { Resvg } from '@resvg/resvg-js';
import { rootDirectory } from './process.mjs';

const windowsIconSizes = [16, 20, 24, 32, 40, 48, 64, 128, 256];

const macOSIconRepresentations = [
  ['icp4', 16],
  ['icp5', 32],
  ['icp6', 64],
  ['ic07', 128],
  ['ic08', 256],
  ['ic09', 512],
  ['ic10', 1024],
  ['ic11', 32],
  ['ic12', 64],
  ['ic13', 256],
  ['ic14', 512],
];

function createWindowsIcon(renderAtSize) {
  const images = windowsIconSizes.map(size => ({ size, image: renderAtSize(size) }));
  const directory = Buffer.alloc(6 + (images.length * 16));
  directory.writeUInt16LE(0, 0);
  directory.writeUInt16LE(1, 2);
  directory.writeUInt16LE(images.length, 4);
  let imageOffset = directory.length;
  for (const [index, { size, image }] of images.entries()) {
    const entryOffset = 6 + (index * 16);
    directory.writeUInt8(size === 256 ? 0 : size, entryOffset);
    directory.writeUInt8(size === 256 ? 0 : size, entryOffset + 1);
    directory.writeUInt8(0, entryOffset + 2);
    directory.writeUInt8(0, entryOffset + 3);
    directory.writeUInt16LE(1, entryOffset + 4);
    directory.writeUInt16LE(32, entryOffset + 6);
    directory.writeUInt32LE(image.length, entryOffset + 8);
    directory.writeUInt32LE(imageOffset, entryOffset + 12);
    imageOffset += image.length;
  }
  return Buffer.concat([directory, ...images.map(({ image }) => image)]);
}

function createMacOSIcon(renderAtSize) {
  const entries = macOSIconRepresentations.map(([type, size]) => {
    const image = renderAtSize(size);
    const header = Buffer.alloc(8);
    header.write(type, 0, 4, 'ascii');
    header.writeUInt32BE(header.length + image.length, 4);
    return Buffer.concat([header, image]);
  });
  const header = Buffer.alloc(8);
  header.write('icns', 0, 4, 'ascii');
  header.writeUInt32BE(header.length + entries.reduce((length, entry) => length + entry.length, 0), 4);
  return Buffer.concat([header, ...entries]);
}

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
  const createRenderer = svg => {
    const renderedImages = new Map();
    return width => {
      if (!renderedImages.has(width)) {
        renderedImages.set(width, Buffer.from(new Resvg(svg, {
          fitTo: { mode: 'width', value: width },
          font: { loadSystemFonts: false },
        }).render().asPng()));
      }
      return renderedImages.get(width);
    };
  };
  const renderColorIcon = createRenderer(source);
  const renderTemplateIcon = createRenderer(template);
  return new Map([
    ['web/public/icon-template.svg', Buffer.from(template)],
    ['internal/platform/icons/tray-windows.png', renderColorIcon(64)],
    ['internal/platform/icons/tray-macos.png', renderTemplateIcon(36)],
    ['internal/platform/icons/application-windows.ico', createWindowsIcon(renderColorIcon)],
    ['internal/platform/icons/application-macos.icns', createMacOSIcon(renderColorIcon)],
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
