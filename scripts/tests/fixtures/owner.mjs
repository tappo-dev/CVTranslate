import { startOwnedProcess } from '../../lib/lifecycle.mjs';

startOwnedProcess(process.execPath, [
  '--input-type=module', '-e',
  'import { writeFileSync } from "node:fs"; writeFileSync(process.argv[1], String(process.pid)); setInterval(() => {}, 1000);',
  process.argv[2],
]);
