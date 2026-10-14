import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { authorizedControlRequest, createSession, isProjectProcess, startOwnedProcess, stopDebugSession, takeOverSession, waitForReady } from '../lib/lifecycle.mjs';
import { run } from '../lib/process.mjs';

async function temporaryDirectory(context) {
  const directory = await mkdtemp(join(tmpdir(), 'cvtranslate tooling tests '));
  context.after(() => rm(directory, { recursive: true, force: true }));
  return directory;
}

async function eventually(predicate, timeoutMilliseconds = 10000) {
  const deadline = Date.now() + timeoutMilliseconds;
  while (Date.now() < deadline) {
    if (await predicate()) return;
    await new Promise(resolveDelay => setTimeout(resolveDelay, 50));
  }
  throw new Error('Expected lifecycle transition did not occur');
}

test('process arguments preserve spaces and shell metacharacters', async context => {
  const directory = await temporaryDirectory(context);
  const value = 'spaces & %PATH% $(never-run) "quoted"';
  const output = await run(process.execPath, ['-e', 'process.stdout.write(process.argv[1])', value], { cwd: directory, capture: true });
  assert.equal(output, value);
});

test('owned supervisor does not inherit eval-only or debugger arguments', async () => {
  const lifecycleURL = new URL('../lib/lifecycle.mjs', import.meta.url).href;
  const source = `import { startOwnedProcess } from ${JSON.stringify(lifecycleURL)};
    const child = startOwnedProcess(process.execPath, ['-e', 'process.stdout.write("ready")'], { capture: true });
    console.log(await child.completion);`;
  const output = await run(process.execPath, ['--input-type=module', '-e', source], { capture: true });
  assert.equal(output, 'ready');
});

test('control authentication rejects wrong token, browser origin and session', () => {
  const request = { method: 'POST', url: '/stop/session', headers: { authorization: 'Bearer secret' } };
  assert.equal(authorizedControlRequest(request, 'secret', 'session'), true);
  assert.equal(authorizedControlRequest(request, 'different', 'session'), false);
  assert.equal(authorizedControlRequest(request, 'secret', 'another-session'), false);
  assert.equal(authorizedControlRequest({ ...request, headers: { ...request.headers, origin: 'http://evil.invalid' } }, 'secret', 'session'), false);
  assert.equal(authorizedControlRequest({ ...request, method: 'GET' }, 'secret', 'session'), false);
});

test('exclusive session rejects duplicates and authenticated debug stop releases ownership', async context => {
  const directory = join(await temporaryDirectory(context), 'session.lock');
  let closeRequested = false;
  const session = await createSession('debug', () => { closeRequested = true; void session.close(); }, directory);
  context.after(() => session.close());
  await assert.rejects(createSession('dev', () => {}, directory), /already owns/);
  const metadata = JSON.parse(await readFile(join(directory, 'owner.json'), 'utf8'));
  const rejected = await fetch(`http://127.0.0.1:${metadata.port}/stop/${metadata.sessionId}`, { method: 'POST', headers: { authorization: 'Bearer wrong' } });
  assert.equal(rejected.status, 403);
  assert.equal(closeRequested, false);
  await stopDebugSession(directory);
  assert.equal(closeRequested, true);
});

test('debug cleanup refuses an ordinary dev session', async context => {
  const directory = join(await temporaryDirectory(context), 'session.lock');
  const session = await createSession('dev', () => assert.fail('must not stop'), directory);
  context.after(() => session.close());
  await assert.rejects(stopDebugSession(directory), /not an F5 helper/);
});

test('F5 takeover stops a live session of any mode through its control endpoint', async context => {
  const directory = join(await temporaryDirectory(context), 'session.lock');
  let stopRequested = false;
  const session = await createSession('dev', () => { stopRequested = true; void session.close(); }, directory);
  context.after(() => session.close());
  assert.equal(await takeOverSession(directory), 'stopped');
  assert.equal(stopRequested, true);
  await assert.rejects(stat(directory), { code: 'ENOENT' });
});

test('F5 takeover removes a stale or corrupt lock and ignores a missing one', async context => {
  const temporaryRoot = await temporaryDirectory(context);
  const staleDirectory = join(temporaryRoot, 'stale.lock');
  const unusedPort = await new Promise(resolvePort => {
    const server = createServer().listen(0, '127.0.0.1', () => { const { port } = server.address(); server.close(() => resolvePort(port)); });
  });
  await mkdir(staleDirectory);
  await writeFile(join(staleDirectory, 'owner.json'), JSON.stringify({ mode: 'dev', sessionId: '00000000-0000-0000-0000-000000000000', token: 'a'.repeat(64), port: unusedPort }));
  assert.equal(await takeOverSession(staleDirectory), 'removed');
  await assert.rejects(stat(staleDirectory), { code: 'ENOENT' });
  const corruptDirectory = join(temporaryRoot, 'corrupt.lock');
  await mkdir(corruptDirectory);
  await writeFile(join(corruptDirectory, 'owner.json'), '{');
  assert.equal(await takeOverSession(corruptDirectory), 'removed');
  assert.equal(await takeOverSession(join(temporaryRoot, 'missing.lock')), 'none');
});

test('only this checkout\'s binaries and Vite are eligible for forced termination', () => {
  const projectRoot = join(tmpdir(), 'cvtranslate checkout');
  const viteCommandLine = `"${process.execPath}" "${join(projectRoot, 'node_modules', 'vite', 'bin', 'vite.js')}" --port 19329`;
  assert.equal(isProjectProcess({ executablePath: join(projectRoot, 'cmd', 'cvtranslate', '__debug_bin.exe') }, projectRoot), true);
  assert.equal(isProjectProcess({ executablePath: join(projectRoot, '.local', 'dev', 'cvtranslate-dev.exe') }, projectRoot), true);
  assert.equal(isProjectProcess({ executablePath: process.execPath, commandLine: viteCommandLine }, projectRoot), true);
  assert.equal(isProjectProcess({ executablePath: process.execPath, commandLine: `"${process.execPath}" other-server.js` }, projectRoot), false);
  assert.equal(isProjectProcess({ executablePath: join(`${projectRoot} copy`, 'cvtranslate.exe') }, projectRoot), false);
  assert.equal(isProjectProcess({}, projectRoot), false);
});

test('owned child is reaped on requested stop and spawn failure is reported', async context => {
  const directory = await temporaryDirectory(context);
  const marker = join(directory, 'owned.pid');
  const child = startOwnedProcess(process.execPath, ['-e', 'require("node:fs").writeFileSync(process.argv[1], String(process.pid)); setInterval(() => {}, 1000)', marker]);
  context.after(() => child.stop());
  await eventually(async () => { try { await readFile(marker); return true; } catch { return false; } });
  const childPid = Number(await readFile(marker, 'utf8'));
  await child.stop();
  assert.throws(() => process.kill(childPid, 0), { code: 'ESRCH' });
  const missing = startOwnedProcess(join(directory, 'nonexistent-command'), []);
  await assert.rejects(missing.completion, /Cannot start/);
});

test('abrupt owner exit disconnects worker and reaps its child', { timeout: 20000 }, async context => {
  const directory = await temporaryDirectory(context);
  const marker = join(directory, 'orphan.pid');
  const owner = spawn(process.execPath, [fileURLToPath(new URL('./fixtures/owner.mjs', import.meta.url)), marker], { stdio: 'ignore', windowsHide: true });
  context.after(() => { if (owner.exitCode === null && owner.signalCode === null) owner.kill(); });
  await eventually(async () => { try { await readFile(marker); return true; } catch { return false; } });
  const childPid = Number(await readFile(marker, 'utf8'));
  const ownerExited = once(owner, 'exit');
  owner.kill('SIGKILL');
  await ownerExited;
  await eventually(() => {
    try { process.kill(childPid, 0); return false; } catch (error) { return error.code === 'ESRCH'; }
  });
});

test('readiness uses only a health request and honors cancellation', async context => {
  const requestedPaths = [];
  const server = createServer((request, response) => { requestedPaths.push(request.url); response.writeHead(200).end('ok'); });
  await new Promise(resolveReady => server.listen(0, '127.0.0.1', resolveReady));
  context.after(() => new Promise(resolveClosed => server.close(resolveClosed)));
  const endpoint = `http://127.0.0.1:${server.address().port}/healthz`;
  await waitForReady(endpoint);
  assert.deepEqual(requestedPaths, ['/healthz']);
  await assert.rejects(waitForReady(endpoint, { signal: AbortSignal.abort() }), /abort/i);
});
