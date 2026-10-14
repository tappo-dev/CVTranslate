import { lstat, readFile, readdir, realpath } from 'node:fs/promises';
import { join, relative } from 'node:path';
import { npm, rootDirectory, run } from './process.mjs';
import { parseJsonStream } from './pipeline.mjs';

// Only reviewed package versions may use an upstream license omitted from npm.
const supplementalNotices = new Map([
  ['cm6-theme-basic-light@0.2.0', {
    license: 'MIT',
    filename: 'cm6-theme-basic-light-0.2.0.LICENSE',
    sources: ['https://github.com/craftzdog/cm6-themes/blob/fc765963a365ec4dc3b6771cea7a881350359d9a/LICENSE'],
  }],
  ['format@0.2.2', {
    license: 'MIT',
    filename: 'format-0.2.2.LICENSE',
    sources: [
      'https://github.com/samsonjs/format/blob/4f898096759776b7c84fa7a25b13c923dadfe46e/Readme.md',
      'https://sjs.mit-license.org/2010-2014',
    ],
  }],
  ['mdast-util-highlight-mark@1.2.2', {
    license: 'MIT',
    filename: 'remark-highlight-mark.LICENSE',
    sources: ['https://github.com/shlroland/remark-highlight-mark/blob/63a6d1cb6e34ff00d875693d5a235bdcf0f8d44d/License'],
  }],
  ['micromark-extension-highlight-mark@1.2.0', {
    license: 'MIT',
    filename: 'remark-highlight-mark.LICENSE',
    sources: ['https://github.com/shlroland/remark-highlight-mark/blob/63a6d1cb6e34ff00d875693d5a235bdcf0f8d44d/License'],
  }],
  ['react-remove-scroll-bar@2.3.8', {
    license: 'MIT',
    filename: 'react-remove-scroll-bar-2.3.8.LICENSE',
    sources: ['https://github.com/theKashey/react-remove-scroll-bar/blob/7301c160fda44cb8cf2b9fdfde61efad35736196/LICENSE'],
  }],
]);

export function getDeclaredLicense(metadata) {
  if (typeof metadata.license === 'string') return metadata.license;
  if (metadata.license?.type) return metadata.license.type;
  // Older npm packages, including format, use an array of license objects.
  return metadata.licenses?.map(license => typeof license === 'string' ? license : license.type).filter(Boolean).join(', ') || undefined;
}

export async function licenseFiles(directory) {
  const files = [];
  const entries = await readdir(directory, { withFileTypes: true });
  for (const entry of entries) {
    if (entry.isSymbolicLink()) continue;
    if (entry.isFile() && /^(licen[sc]e|copying|notice)([.\-_ ]|$)/i.test(entry.name)) files.push(join(directory, entry.name));
    if (entry.isDirectory() && /^(licen[sc]es|notices)$/i.test(entry.name)) {
      async function collectNested(current, depth) {
        if (depth > 8) throw new Error(`License directory nesting too deep: ${entry.name}`);
        for (const nested of await readdir(current, { withFileTypes: true })) {
          const filename = join(current, nested.name);
          if (nested.isDirectory()) await collectNested(filename, depth + 1);
          else if (nested.isFile()) files.push(filename);
        }
      }
      await collectNested(join(directory, entry.name), 0);
    }
  }
  return files.sort();
}

export async function formatNotice({ name, version, license, directory, replacement }) {
  const files = await licenseFiles(directory);
  let noticeDirectory = directory;
  const header = [`## ${name}@${version}`, `Declared license: ${license ?? 'not declared in package metadata'}`];
  if (replacement) header.push(`Resolved replacement: ${replacement}`);
  if (files.length === 0) {
    const supplement = supplementalNotices.get(`${name}@${version}`);
    if (!supplement || license !== supplement.license || replacement) {
      throw new Error(`No license/notice text found for ${name}@${version}. Resolve its redistribution notices before releasing.`);
    }
    noticeDirectory = join(rootDirectory, 'docs', 'third-party');
    files.push(join(noticeDirectory, supplement.filename));
    header.push(...supplement.sources.map(source => `Supplemental license source: ${source}`));
  }
  for (const filename of files) {
    if (!(await lstat(filename)).isFile()) throw new Error('License source must be a regular file');
    const licenseText = await readFile(filename, 'utf8');
    if (!licenseText.trim()) throw new Error(`Empty license/notice text for ${name}@${version}. Resolve its redistribution notices before releasing.`);
    header.push(`\n--- ${relative(noticeDirectory, filename).replaceAll('\\', '/')} ---\n${licenseText}`);
  }
  return header.join('\n');
}

export async function collectNotices() {
  const sections = [
    '# Third-party notices',
    'Generated from resolved production dependencies for this host. Dependency licenses do not grant a license to CVTranslate itself.',
  ];
  const goroot = await run('go', ['env', 'GOROOT'], { capture: true });
  sections.push(await formatNotice({ name: 'Go runtime and standard library', version: await run('go', ['env', 'GOVERSION'], { capture: true }), directory: goroot }));
  const packages = parseJsonStream(await run('go', ['list', '-deps', '-json', './cmd/cvtranslate'], { capture: true }));
  const modules = new Map();
  for (const definition of packages) {
    if (!definition.Module || definition.Module.Main) continue;
    const module = definition.Module;
    const resolved = module.Replace ?? module;
    if (!resolved.Dir) throw new Error(`Cannot locate resolved Go module ${module.Path}`);
    modules.set(`${module.Path}@${module.Version}`, {
      name: module.Path, version: module.Version ?? 'local', directory: resolved.Dir,
      replacement: module.Replace ? `${resolved.Path}@${resolved.Version ?? 'local'}` : undefined,
    });
  }
  for (const [, module] of [...modules].sort(([left], [right]) => left.localeCompare(right))) sections.push(await formatNotice(module));
  const paths = await npm(['ls', '--omit=dev', '--all', '--parseable', '--workspaces', '--include-workspace-root'], { capture: true });
  const uniquePaths = [...new Set(paths.split(/\r?\n/).filter(Boolean))].sort();
  const ownDirectories = new Set(await Promise.all([realpath(rootDirectory), realpath(join(rootDirectory, 'web'))]));
  const visited = new Set();
  for (const dependencyPath of uniquePaths) {
    const directory = await realpath(dependencyPath);
    if (ownDirectories.has(directory) || visited.has(directory)) continue;
    visited.add(directory);
    const metadata = JSON.parse(await readFile(join(directory, 'package.json'), 'utf8'));
    const declaredLicense = getDeclaredLicense(metadata);
    sections.push(await formatNotice({ name: metadata.name, version: metadata.version, license: declaredLicense, directory }));
  }
  return `${sections.join('\n\n')}\n`;
}
