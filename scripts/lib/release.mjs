import { createHash } from 'node:crypto';
import { createReadStream, createWriteStream } from 'node:fs';
import { readdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { run } from './process.mjs';

export function releaseFiles(platform) {
  const executableFiles = platform === 'darwin'
    ? ['CVTranslate.app/Contents/MacOS/cvtranslate', 'CVTranslate.app/Contents/Info.plist']
    : ['cvtranslate.exe'];
  return [...executableFiles, 'LICENSE', 'RELEASE.md', 'THIRD-PARTY-NOTICES.txt', 'build-manifest.json', 'SHA256SUMS'];
}

export async function verifyWhitelist(directory, allowedFiles) {
  const allowed = new Set(allowedFiles);
  const observed = new Set();
  async function walk(current, prefix = '') {
    for (const entry of await readdir(current, { withFileTypes: true })) {
      const filename = prefix ? `${prefix}/${entry.name}` : entry.name;
      if (entry.isSymbolicLink()) throw new Error(`Release cannot contain symlinks: ${filename}`);
      if (entry.isDirectory()) {
        if (![...allowed].some(allowedFile => allowedFile.startsWith(`${filename}/`))) throw new Error(`Unapproved release directory: ${filename}`);
        await walk(join(current, entry.name), filename);
      } else if (entry.isFile() && allowed.has(filename)) observed.add(filename);
      else throw new Error(`Unapproved release entry: ${filename}`);
    }
  }
  await walk(directory);
  for (const filename of allowed) if (!observed.has(filename)) throw new Error(`Missing release entry: ${filename}`);
}

export async function sha256(filename) {
  const digest = createHash('sha256');
  for await (const chunk of createReadStream(filename)) digest.update(chunk);
  return digest.digest('hex');
}

export async function writeChecksums(directory, filenames) {
  const lines = [];
  for (const filename of [...filenames].filter(filename => filename !== 'SHA256SUMS').sort()) {
    lines.push(`${await sha256(join(directory, filename))}  ${filename}`);
  }
  await writeFile(join(directory, 'SHA256SUMS'), `${lines.join('\n')}\n`);
}

export async function createArchive(directory, destination, filenames, platform = process.platform) {
  // Validation precedes any archive operation; never archive the workspace itself.
  await verifyWhitelist(directory, filenames);
  if (platform === 'darwin') {
    await run('ditto', ['-c', '-k', '--sequesterRsrc', directory, destination]);
    return;
  }
  const { ZipArchive } = await import('archiver');
  await new Promise((resolveArchive, reject) => {
    const output = createWriteStream(destination, { flags: 'wx' });
    const archive = new ZipArchive({ zlib: { level: 9 } });
    let failed = false;
    const fail = error => {
      if (failed) return;
      failed = true;
      archive.abort();
      output.destroy();
      reject(error);
    };
    output.on('error', fail);
    archive.on('warning', fail);
    archive.on('error', fail);
    output.on('close', () => { if (!failed) resolveArchive(); });
    archive.pipe(output);
    for (const filename of filenames) archive.file(join(directory, filename), { name: filename });
    archive.finalize().catch(fail);
  });
}

export function validateVersion(version) {
  if (typeof version !== 'string' || !/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(version)) {
    throw new Error('package.json must contain a safe semantic version');
  }
  return version;
}

const plainVersionPattern = /^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;

// package.json supplies major.minor; the patch continues from released tags in that line.
export function nextReleaseVersion(baseVersion, existingTags) {
  const base = plainVersionPattern.exec(baseVersion);
  if (!base || baseVersion.startsWith('v')) throw new Error(`package.json version must be plain major.minor.patch: ${baseVersion}`);
  const [baseMajor, baseMinor, basePatch] = base.slice(1).map(Number);
  let highestPatchInLine = -1;
  for (const tag of existingTags) {
    const parsed = plainVersionPattern.exec(tag);
    if (!parsed || !tag.startsWith('v')) continue;
    const [major, minor, patch] = parsed.slice(1).map(Number);
    if (major > baseMajor || (major === baseMajor && minor > baseMinor)) {
      throw new Error(`package.json version ${baseVersion} is behind released tag ${tag}; raise major.minor.`);
    }
    if (major === baseMajor && minor === baseMinor) highestPatchInLine = Math.max(highestPatchInLine, patch);
  }
  return `${baseMajor}.${baseMinor}.${Math.max(basePatch, highestPatchInLine + 1)}`;
}

export function appInfoPlist(version) {
  validateVersion(version);
  return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleIdentifier</key><string>com.cvtranslate.desktop</string>
  <key>CFBundleName</key><string>CVTranslate</string>
  <key>CFBundleDisplayName</key><string>CVTranslate</string>
  <key>CFBundleExecutable</key><string>cvtranslate</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${version.split(/[-+]/)[0]}</string>
  <key>CFBundleVersion</key><string>${version.split(/[-+]/)[0]}</string>
  <key>LSUIElement</key><true/>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>\n`;
}
