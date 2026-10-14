import { spawn } from 'node:child_process';
import { access, readFile } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

export const rootDirectory = fileURLToPath(new URL('../../', import.meta.url));
export const localDirectory = join(rootDirectory, '.local');

export async function exists(filename) {
  try { await access(filename); return true; } catch { return false; }
}

export function terminateTree(child) {
  if (!child.pid || child.exitCode !== null || child.signalCode !== null) return;
  if (process.platform === 'win32') {
    // Only a still-owned child is targeted, never a discovered PID or a port.
    const terminator = spawn('taskkill.exe', ['/PID', String(child.pid), '/T', '/F'], { stdio: 'ignore', windowsHide: true });
    terminator.on('error', () => child.kill());
  } else {
    try { process.kill(-child.pid, 'SIGTERM'); } catch { child.kill('SIGTERM'); }
    const escalation = setTimeout(() => {
      if (child.exitCode === null && child.signalCode === null) {
        try { process.kill(-child.pid, 'SIGKILL'); } catch { /* Already exited. */ }
      }
    }, 3000);
    escalation.unref();
  }
}

export function run(command, argumentsList = [], options = {}) {
  const { capture = false, cwd = rootDirectory, env = process.env, signal } = options;
  return new Promise((resolveResult, reject) => {
    if (signal?.aborted) { reject(new Error('Operation cancelled')); return; }
    const child = spawn(command, argumentsList, {
      cwd, env, windowsHide: true, detached: process.platform !== 'win32',
      stdio: capture ? ['ignore', 'pipe', 'pipe'] : 'inherit',
    });
    let standardOutput = '';
    let standardError = '';
    child.stdout?.on('data', chunk => { standardOutput += chunk; });
    child.stderr?.on('data', chunk => { standardError += chunk; });
    const cancel = () => terminateTree(child);
    signal?.addEventListener('abort', cancel, { once: true });
    child.on('error', error => {
      signal?.removeEventListener('abort', cancel);
      reject(new Error(`Cannot start ${command}: ${error.message}`, { cause: error }));
    });
    child.on('close', (code, exitSignal) => {
      signal?.removeEventListener('abort', cancel);
      if (code === 0 && !signal?.aborted) resolveResult(standardOutput.trim());
      else reject(new Error(`${command} exited ${exitSignal ?? code}${capture && standardError ? `: ${standardError.trim()}` : ''}`));
    });
  });
}

export async function npmInvocation(argumentsList) {
  const candidates = [process.env.npm_execpath, join(dirname(process.execPath), 'node_modules/npm/bin/npm-cli.js')].filter(Boolean);
  if (process.platform === 'win32') {
    try {
      const locations = await run('where.exe', ['npm.cmd'], { capture: true });
      for (const location of locations.split(/\r?\n/)) candidates.push(join(dirname(location), 'node_modules/npm/bin/npm-cli.js'));
    } catch { /* Report an actionable error below. */ }
  } else {
    for (const directory of (process.env.PATH ?? '').split(':')) {
      candidates.push(resolve(directory, '../lib/node_modules/npm/bin/npm-cli.js'));
    }
  }
  for (const candidate of candidates) {
    if (await exists(candidate)) return { command: process.execPath, argumentsList: [candidate, ...argumentsList] };
  }
  throw new Error('npm CLI not found. Use a Node installation that includes npm, or invoke this script through npm run.');
}

export async function npm(argumentsList, options = {}) {
  const invocation = await npmInvocation(argumentsList);
  return run(invocation.command, invocation.argumentsList, options);
}

export async function readPackage() {
  return JSON.parse(await readFile(join(rootDirectory, 'package.json'), 'utf8'));
}

export function main(action) {
  action().catch(error => { console.error(error.message); process.exitCode = 1; });
}
