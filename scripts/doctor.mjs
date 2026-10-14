import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { exists, main, npm, rootDirectory, run } from './lib/process.mjs';

main(async () => {
  const argumentsList = process.argv.slice(2);
  if (argumentsList.some(argument => argument !== '--debug')) throw new Error('Usage: node scripts/doctor.mjs [--debug]');
  let failures = 0;
  async function inspect(label, check) {
    try { console.log(`[OK] ${label}: ${await check()}`); } catch (error) {
      failures += 1;
      console.error(`[FAIL] ${label}: ${error.message}`);
    }
  }
  await inspect('Desktop host', async () => {
    if (!['win32', 'darwin'].includes(process.platform)) throw new Error('Only Windows and macOS desktop hosts are supported.');
    return `${process.platform}/${process.arch}`;
  });
  await inspect('Node', async () => {
    const [major, minor] = process.versions.node.split('.').map(Number);
    if (major < 22 || (major === 22 && minor < 12)) throw new Error('Node >=22.12 is required.');
    return process.version;
  });
  await inspect('npm', () => npm(['--version'], { capture: true }));
  await inspect('Go', async () => {
    const output = await run('go', ['version'], { capture: true });
    const installed = /go(\d+)\.(\d+)(?:\.(\d+))?/.exec(output);
    const moduleText = await readFile(join(rootDirectory, 'go.mod'), 'utf8');
    const required = /^go\s+(\d+)\.(\d+)(?:\.(\d+))?/m.exec(moduleText);
    if (!installed || !required) throw new Error('Cannot determine installed/required Go versions.');
    const numericVersion = match => Number(match[1]) * 1000000 + Number(match[2]) * 1000 + Number(match[3] ?? 0);
    if (numericVersion(installed) < numericVersion(required)) throw new Error(`Go ${required[0]} or newer is required.`);
    return output;
  });
  await inspect('Installed workspace dependencies', async () => {
    for (const relative of ['package-lock.json', 'node_modules/vite/bin/vite.js', 'node_modules/archiver/package.json']) {
      if (!(await exists(join(rootDirectory, relative)))) throw new Error(`${relative} missing; run npm ci (doctor never installs).`);
    }
    await npm(['ls', '--all', '--workspaces', '--include-workspace-root'], { capture: true });
    return 'lockfile, Vite, archiver and npm dependency tree present';
  });
  if (argumentsList.includes('--debug')) await inspect('Delve', () => run('dlv', ['version'], { capture: true }));
  if (process.platform === 'darwin') {
    // ditto has no help/version flag that exits 0, so only resolve it on PATH.
    await inspect('macOS archive tool', () => run('which', ['ditto'], { capture: true }));
    await inspect('macOS native compiler', async () => {
      const cgo = await run('go', ['env', 'CGO_ENABLED'], { capture: true });
      if (cgo !== '1') throw new Error('CGO_ENABLED=1 is required for macOS Accessibility and Keychain integration.');
      await run('xcrun', ['--find', 'clang'], { capture: true });
      return run('clang', ['--version'], { capture: true });
    });
  }
  if (failures) throw new Error(`Doctor found ${failures} prerequisite problem(s). Nothing was installed.`);
  console.log('Prerequisites passed. This does not validate desktop permissions or UI behavior.');
});
