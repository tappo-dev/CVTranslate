import { mkdtemp, readdir, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { npm, rootDirectory, run } from './process.mjs';

export function parseJsonStream(text) {
  const values = [];
  let start = 0;
  let depth = 0;
  let quoted = false;
  let escaped = false;
  for (let index = 0; index < text.length; index += 1) {
    const character = text[index];
    if (quoted) {
      if (escaped) escaped = false;
      else if (character === '\\') escaped = true;
      else if (character === '"') quoted = false;
    } else if (character === '"') quoted = true;
    else if (character === '{') { if (depth === 0) start = index; depth += 1; }
    else if (character === '}') {
      depth -= 1;
      if (depth === 0) values.push(JSON.parse(text.slice(start, index + 1)));
    }
  }
  if (depth !== 0 || quoted) throw new Error('Incomplete JSON stream from Go tooling');
  return values;
}

export function verifyArchitecture(packages) {
  for (const definition of packages) {
    if (!/^cvtranslate\/internal\/(domain|provider)(\/|$)/.test(definition.ImportPath)) continue;
    const dependencies = [...(definition.Imports ?? []), ...(definition.Deps ?? [])];
    for (const dependency of dependencies) {
      if (/^cvtranslate\/internal\/(platform|server|storage)(\/|$)/.test(dependency)) {
        throw new Error(`Architecture boundary violated: ${definition.ImportPath} depends on ${dependency}`);
      }
    }
  }
}

async function collectGoFiles(directory) {
  const files = [];
  let entries;
  try { entries = await readdir(directory, { withFileTypes: true }); } catch (error) {
    if (error.code === 'ENOENT') return files;
    throw error;
  }
  for (const entry of entries) {
    if (entry.isSymbolicLink() || ['node_modules', 'dist', '.git', '.local', 'release'].includes(entry.name)) continue;
    const filename = join(directory, entry.name);
    if (entry.isDirectory()) files.push(...await collectGoFiles(filename));
    else if (entry.isFile() && entry.name.endsWith('.go')) files.push(filename);
  }
  return files;
}

export async function checkPipeline({ compile = true } = {}) {
  await run(process.execPath, [join(rootDirectory, 'scripts', 'doctor.mjs')]);
  await run(process.execPath, [join(rootDirectory, 'scripts', 'icons.mjs'), '--check']);
  const testFiles = (await readdir(join(rootDirectory, 'scripts', 'tests'))).filter(name => name.endsWith('.test.mjs')).sort();
  await run(process.execPath, ['--test', ...testFiles.map(name => join(rootDirectory, 'scripts', 'tests', name))]);
  for (const task of ['typecheck', 'lint', 'test', 'build']) {
    console.log(`Checking web: ${task}`);
    await npm(['run', task, '--workspace', '@cvtranslate/web']);
  }
  const goFiles = await collectGoFiles(rootDirectory);
  for (let offset = 0; offset < goFiles.length; offset += 40) {
    const unformatted = await run('gofmt', ['-l', ...goFiles.slice(offset, offset + 40)], { capture: true });
    if (unformatted) throw new Error(`Run gofmt on these files:\n${unformatted}`);
  }
  console.log('Checking Go architecture, vet and tests');
  // Some npm dependencies ship unrelated Go examples without their own module.
  // Scope checks to our packages instead of walking node_modules with ./....
  const projectPackages = ['./cmd/...', './internal/...', './web'];
  for (const buildArguments of [[], ['-tags', 'dev']]) {
    const packages = parseJsonStream(await run('go', ['list', ...buildArguments, '-json', ...projectPackages], { capture: true }));
    verifyArchitecture(packages);
    await run('go', ['vet', ...buildArguments, ...projectPackages]);
    await run('go', ['test', ...buildArguments, ...projectPackages]);
  }
  if (compile) {
    const directory = await mkdtemp(join(tmpdir(), 'cvtranslate-check-'));
    try {
      for (const buildArguments of [[], ['-tags', 'dev']]) {
        const executable = join(directory, process.platform === 'win32' ? 'cvtranslate.exe' : 'cvtranslate');
        await run('go', ['build', ...buildArguments, '-o', executable, './cmd/cvtranslate']);
        await run(executable, ['--version', '--no-native', '--no-open']);
      }
    } finally { await rm(directory, { recursive: true, force: true }); }
  }
}
