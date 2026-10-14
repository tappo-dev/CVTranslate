import { run } from './process.mjs';

const cancellation = new AbortController();
let started = false;
process.on('disconnect', () => cancellation.abort());
process.on('SIGINT', () => cancellation.abort());
process.on('SIGTERM', () => cancellation.abort());
process.on('message', async message => {
  if (message.type === 'stop') { cancellation.abort(); return; }
  if (message.type !== 'start' || started) return;
  started = true;
  try {
    const output = await run(message.command, message.argumentsList, { ...message.options, signal: cancellation.signal });
    if (process.connected) process.send({ type: 'result', success: true, output });
  } catch (error) {
    if (process.connected) process.send({ type: 'result', success: false, error: error.message });
    process.exitCode = cancellation.signal.aborted ? 0 : 1;
  } finally {
    if (process.connected) process.disconnect();
  }
});
