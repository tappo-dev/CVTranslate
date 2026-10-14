import { fork } from 'node:child_process';
import { randomBytes, randomUUID, timingSafeEqual } from 'node:crypto';
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { createServer as createSocketServer } from 'node:net';
import { isAbsolute, join, relative } from 'node:path';
import { exists, localDirectory, rootDirectory, run } from './process.mjs';

const defaultLockDirectory = join(localDirectory, 'tooling', 'dev.lock');

export function startOwnedProcess(command, argumentsList, options = {}) {
  const worker = fork(new URL('./worker.mjs', import.meta.url), [], { stdio: ['ignore', 'inherit', 'inherit', 'ipc'], windowsHide: true, execArgv: [] });
  let result;
  const completion = new Promise((resolveResult, reject) => {
    worker.on('message', message => { if (message.type === 'result') result = message; });
    worker.on('error', reject);
    worker.on('exit', () => {
      if (result?.success) resolveResult(result.output);
      else reject(new Error(result?.error ?? 'Owned process supervisor exited unexpectedly'));
    });
  });
  // Callers may attach failure monitoring after startup; never leak an unhandled rejection.
  completion.catch(() => {});
  worker.send({ type: 'start', command, argumentsList, options });
  return {
    completion,
    async stop() {
      if (worker.connected) worker.send({ type: 'stop' }, () => {});
      await completion.catch(() => {});
    },
  };
}

export async function assertPortAvailable(port) {
  const server = createSocketServer();
  await new Promise((resolveReady, reject) => {
    server.once('error', () => reject(new Error(`127.0.0.1:${port} is in use. Stop the existing session; no process will be killed by port.`)));
    server.listen({ host: '127.0.0.1', port, exclusive: true }, resolveReady);
  });
  await new Promise(resolveClosed => server.close(resolveClosed));
}

export async function waitForReady(url, { signal, timeoutMilliseconds = 45000 } = {}) {
  const deadline = Date.now() + timeoutMilliseconds;
  while (Date.now() < deadline) {
    signal?.throwIfAborted();
    try {
      const requestSignal = signal ? AbortSignal.any([signal, AbortSignal.timeout(1000)]) : AbortSignal.timeout(1000);
      const response = await fetch(url, { signal: requestSignal, redirect: 'error' });
      await response.body?.cancel();
      if (response.ok) return;
    } catch { signal?.throwIfAborted(); }
    await new Promise(resolveDelay => setTimeout(resolveDelay, 150));
  }
  throw new Error(`Timed out waiting for ${new URL(url).origin} readiness`);
}

export function authorizedControlRequest(request, token, sessionId) {
  const suppliedToken = request.headers.authorization?.replace(/^Bearer /, '') ?? '';
  const expected = Buffer.from(token);
  const supplied = Buffer.from(suppliedToken);
  return request.method === 'POST' && request.url === `/stop/${sessionId}` &&
    !request.headers.origin && supplied.length === expected.length && timingSafeEqual(supplied, expected);
}

export async function createSession(mode, onStop, directory = defaultLockDirectory) {
  await mkdir(join(directory, '..'), { recursive: true, mode: 0o700 });
  try { await mkdir(directory, { mode: 0o700 }); } catch (error) {
    if (error.code === 'EEXIST') throw new Error(`A development/debug session already owns ${directory}. Use its terminal or npm run dev -- --stop-debug. If the owner crashed, verify all its processes have stopped before manually removing this lock directory.`);
    throw error;
  }
  const token = randomBytes(32).toString('hex');
  const sessionId = randomUUID();
  const server = createServer((request, response) => {
    if (request.headers.host !== `127.0.0.1:${server.address()?.port}` || !authorizedControlRequest(request, token, sessionId)) {
      response.writeHead(403).end(); return;
    }
    response.writeHead(204).end();
    setImmediate(onStop);
  });
  let closed = false;
  async function close() {
    if (closed) return;
    closed = true;
    if (server.listening) await new Promise(resolveClosed => server.close(resolveClosed));
    await rm(directory, { recursive: true, force: true });
  }
  try {
    await new Promise((resolveReady, reject) => {
      server.once('error', reject);
      server.listen(0, '127.0.0.1', resolveReady);
    });
    const metadata = { mode, sessionId, token, port: server.address().port };
    await writeFile(join(directory, 'owner.json'), JSON.stringify(metadata), { mode: 0o600, flag: 'wx' });
    return { close, directory };
  } catch (error) { await close(); throw error; }
}

function hasValidOwnerMetadata(metadata) {
  return Number.isInteger(metadata?.port) && metadata.port >= 1 && metadata.port <= 65535 &&
    /^[a-f0-9]{64}$/.test(metadata.token) && /^[a-f0-9-]{36}$/.test(metadata.sessionId);
}

async function requestOwnerStop(metadata) {
  const response = await fetch(`http://127.0.0.1:${metadata.port}/stop/${metadata.sessionId}`, {
    method: 'POST', headers: { authorization: `Bearer ${metadata.token}` },
    signal: AbortSignal.timeout(3000), redirect: 'error',
  });
  await response.body?.cancel();
  return response.status === 204;
}

async function waitForOwnerRelease(directory, sessionId) {
  const deadline = Date.now() + 10000;
  while (Date.now() < deadline) {
    try {
      const currentOwner = JSON.parse(await readFile(join(directory, 'owner.json'), 'utf8'));
      if (currentOwner.sessionId !== sessionId) return true;
    } catch (error) { if (error.code === 'ENOENT') return true; throw error; }
    await new Promise(resolveDelay => setTimeout(resolveDelay, 100));
  }
  return false;
}

export async function stopDebugSession(directory = defaultLockDirectory) {
  let metadata;
  try { metadata = JSON.parse(await readFile(join(directory, 'owner.json'), 'utf8')); } catch (error) {
    if (error.code === 'ENOENT') return;
    throw error;
  }
  if (metadata.mode !== 'debug') throw new Error('The active session is not an F5 helper; stop it from its own terminal.');
  if (!hasValidOwnerMetadata(metadata)) {
    throw new Error('Invalid debug ownership metadata; refusing to contact or kill any process.');
  }
  if (!(await requestOwnerStop(metadata))) throw new Error('Debug ownership verification failed; no process was killed.');
  if (!(await waitForOwnerRelease(directory, metadata.sessionId))) {
    throw new Error('Debug helper acknowledged shutdown but cleanup has not completed.');
  }
}

// Returns 'none', 'stopped' (live owner shut down via its control endpoint) or 'removed' (stale lock deleted).
export async function takeOverSession(directory = defaultLockDirectory) {
  let metadata;
  try { metadata = JSON.parse(await readFile(join(directory, 'owner.json'), 'utf8')); } catch (error) {
    const missingOwnerFile = error.code === 'ENOENT';
    if (missingOwnerFile && !(await exists(directory))) return 'none';
    if (!missingOwnerFile && !(error instanceof SyntaxError)) throw error;
  }
  if (hasValidOwnerMetadata(metadata)) {
    let stopAcknowledged = false;
    try { stopAcknowledged = await requestOwnerStop(metadata); } catch { /* An unreachable owner means the lock is stale. */ }
    if (stopAcknowledged) {
      if (await waitForOwnerRelease(directory, metadata.sessionId)) return 'stopped';
      throw new Error('The previous session acknowledged shutdown but did not release its lock.');
    }
  }
  await rm(directory, { recursive: true, force: true });
  return 'removed';
}

export function isProjectProcess({ executablePath, commandLine }, projectRoot = rootDirectory) {
  if (executablePath) {
    const pathFromRoot = relative(projectRoot, executablePath);
    if (pathFromRoot && !pathFromRoot.startsWith('..') && !isAbsolute(pathFromRoot)) return true;
  }
  if (!commandLine) return false;
  const viteEntry = join(projectRoot, 'node_modules', 'vite', 'bin', 'vite.js');
  const caseInsensitivePaths = process.platform === 'win32';
  const normalizedCommandLine = caseInsensitivePaths ? commandLine.toLowerCase() : commandLine;
  return normalizedCommandLine.includes(caseInsensitivePaths ? viteEntry.toLowerCase() : viteEntry);
}

async function listPortListeners(port) {
  if (!Number.isInteger(port)) throw new Error('Port must be an integer.');
  if (process.platform === 'win32') {
    const script = `$owners = @(Get-NetTCPConnection -State Listen -LocalPort ${port} -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess -Unique);
      ConvertTo-Json -Compress -InputObject @($owners | ForEach-Object {
        $processInfo = Get-CimInstance Win32_Process -Filter "ProcessId=$_";
        [pscustomobject]@{ pid = [int]$_; executablePath = $processInfo.ExecutablePath; commandLine = $processInfo.CommandLine } })`;
    const output = await run('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', script], { capture: true });
    return output ? JSON.parse(output) : [];
  }
  let pidOutput;
  try { pidOutput = await run('lsof', ['-nP', `-iTCP:${port}`, '-sTCP:LISTEN', '-t'], { capture: true }); } catch { return []; }
  const listenerPids = [...new Set(pidOutput.split(/\s+/).filter(Boolean).map(Number))];
  return Promise.all(listenerPids.map(async pid => ({
    pid,
    executablePath: await run('ps', ['-p', String(pid), '-o', 'comm='], { capture: true }).catch(() => ''),
    commandLine: await run('ps', ['-p', String(pid), '-o', 'command='], { capture: true }).catch(() => ''),
  })));
}

async function isPortAvailable(port) {
  try { await assertPortAvailable(port); return true; } catch { return false; }
}

async function terminateProcess(pid, forceful) {
  try {
    if (process.platform === 'win32') await run('taskkill.exe', ['/PID', String(pid), '/T', '/F'], { capture: true });
    else process.kill(pid, forceful ? 'SIGKILL' : 'SIGTERM');
  } catch { /* Already exited; port availability is checked below. */ }
}

// Terminates a previous listener only when it is verifiably this checkout's dev binary or Vite.
export async function releaseProjectPort(port, { projectRoot = rootDirectory, log = console.log } = {}) {
  if (await isPortAvailable(port)) return;
  const listeners = await listPortListeners(port);
  const foreignListener = listeners.find(listener => !isProjectProcess(listener, projectRoot));
  if (listeners.length === 0 || foreignListener) {
    const occupant = foreignListener ? `${foreignListener.executablePath || 'unknown executable'} (PID ${foreignListener.pid})` : 'an unidentified process';
    throw new Error(`127.0.0.1:${port} is in use by ${occupant}, which is not a CVTranslate development process; it was not terminated.`);
  }
  for (const listener of listeners) {
    log(`Terminating previous CVTranslate process ${listener.pid} on 127.0.0.1:${port}`);
    await terminateProcess(listener.pid, false);
  }
  const startTime = Date.now();
  let escalated = false;
  while (Date.now() - startTime < 10000) {
    if (await isPortAvailable(port)) return;
    if (!escalated && Date.now() - startTime > 3000) {
      escalated = true;
      for (const listener of listeners) await terminateProcess(listener.pid, true);
    }
    await new Promise(resolveDelay => setTimeout(resolveDelay, 150));
  }
  throw new Error(`127.0.0.1:${port} is still in use after terminating the previous CVTranslate process.`);
}
