import { mkdir } from 'node:fs/promises';
import { join } from 'node:path';
import { assertPortAvailable, createSession, releaseProjectPort, startOwnedProcess, stopDebugSession, takeOverSession, waitForReady } from './lib/lifecycle.mjs';
import { exists, localDirectory, main, rootDirectory } from './lib/process.mjs';

main(async () => {
  const argumentsList = process.argv.slice(2);
  if (argumentsList.some(argument => !['--prepare-debug', '--stop-debug', '--headless'].includes(argument)) || argumentsList.length > 1) {
    throw new Error('Usage: node scripts/dev.mjs [--prepare-debug | --stop-debug | --headless]');
  }
  if (argumentsList.includes('--stop-debug')) { await stopDebugSession(); return; }
  const debug = argumentsList.includes('--prepare-debug');
  const headless = argumentsList.includes('--headless');
  const cancellation = new AbortController();
  const children = new Set();
  let stopping = false;
  let failure;
  let resolveShutdown;
  const shutdownRequested = new Promise(resolveStop => { resolveShutdown = resolveStop; });
  function requestStop(error) {
    if (stopping) return;
    stopping = true;
    failure = error;
    cancellation.abort();
    resolveShutdown();
  }
  const onSignal = () => requestStop();
  process.on('SIGINT', onSignal);
  process.on('SIGTERM', onSignal);
  const parentPid = process.ppid;
  const parentWatcher = setInterval(() => {
    try { process.kill(parentPid, 0); } catch (error) { if (error.code === 'ESRCH') requestStop(); }
  }, 1000);
  let session;
  function start(command, commandArguments, options = {}, persistent = true) {
    cancellation.signal.throwIfAborted();
    const child = startOwnedProcess(command, commandArguments, options);
    children.add(child);
    child.completion.then(() => {
      if (persistent && !stopping) requestStop(new Error('A development service exited unexpectedly.'));
    }, error => { if (!stopping) requestStop(error); });
    return child;
  }
  try {
    console.log('CVTRANSLATE_DEV_STARTING');
    await start(process.execPath, [join(rootDirectory, 'scripts', 'doctor.mjs'), ...(debug ? ['--debug'] : [])], {}, false).completion;
    if (debug) {
      const takeoverResult = await takeOverSession();
      if (takeoverResult === 'stopped') console.log('Stopped the previous development/debug session.');
      if (takeoverResult === 'removed') console.log('Removed a stale development/debug lock.');
      await releaseProjectPort(19329);
      await releaseProjectPort(19328);
    }
    session = await createSession(debug ? 'debug' : 'dev', onSignal);
    await assertPortAvailable(19329);
    await assertPortAvailable(19328);
    const viteEntry = join(rootDirectory, 'node_modules', 'vite', 'bin', 'vite.js');
    if (!(await exists(viteEntry))) throw new Error('Vite is missing. Run npm ci first.');
    await mkdir(join(localDirectory, 'dev'), { recursive: true, mode: 0o700 });
    start(process.execPath, [viteEntry, '--host', '127.0.0.1', '--port', '19329', '--strictPort'], { cwd: join(rootDirectory, 'web') });
    await waitForReady('http://127.0.0.1:19329/', { signal: cancellation.signal });
    if (!debug) {
      const executable = join(localDirectory, 'dev', process.platform === 'win32' ? 'cvtranslate-dev.exe' : 'cvtranslate-dev');
      await start('go', ['build', '-tags', 'dev', '-o', executable, './cmd/cvtranslate'], {}, false).completion;
      start(executable, ['--port=19328', `--data-dir=${join(localDirectory, 'dev')}`, '--vite-url=http://127.0.0.1:19329', ...(headless ? ['--no-open', '--no-native'] : [])]);
      await waitForReady('http://127.0.0.1:19328/healthz', { signal: cancellation.signal });
    }
    console.log(debug ? 'CVTRANSLATE_DEBUG_READY' : 'CVTRANSLATE_DEV_READY');
    await shutdownRequested;
  } catch (error) {
    if (!stopping) failure = error;
  } finally {
    stopping = true;
    cancellation.abort();
    clearInterval(parentWatcher);
    await Promise.all([...children].map(child => child.stop()));
    await session?.close();
    process.off('SIGINT', onSignal);
    process.off('SIGTERM', onSignal);
  }
  if (failure) throw failure;
});
