import assert from 'node:assert/strict';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import test from 'node:test';
import { inflateRawSync } from 'node:zlib';
import { NtExecutable, NtExecutableResource } from '@shockpkg/resedit';
import { formatNotice, getDeclaredLicense } from '../lib/notices.mjs';
import { parseJsonStream, verifyArchitecture } from '../lib/pipeline.mjs';
import { appInfoPlist, createArchive, embedWindowsExecutableIcon, nextReleaseVersion, releaseFiles, validateVersion, verifyWhitelist, writeChecksums } from '../lib/release.mjs';

async function fixture(context) {
  const directory = await mkdtemp(join(tmpdir(), 'cvtranslate release tests '));
  context.after(() => rm(directory, { recursive: true, force: true }));
  return directory;
}

test('release version continues the package major.minor line from existing tags', () => {
  assert.equal(nextReleaseVersion('0.1.0', []), '0.1.0');
  assert.equal(nextReleaseVersion('0.1.0', ['v0.1.0']), '0.1.1');
  assert.equal(nextReleaseVersion('0.1.0', ['v0.1.9', 'v0.1.10', 'v0.1.2']), '0.1.11');
  assert.equal(nextReleaseVersion('0.2.0', ['v0.1.7']), '0.2.0');
  assert.equal(nextReleaseVersion('0.1.5', ['v0.1.2']), '0.1.5');
  assert.equal(nextReleaseVersion('0.1.0', ['v0.1.3-beta', 'experiment', '0.1.8', 'v0.01.4']), '0.1.0');
  assert.throws(() => nextReleaseVersion('0.1.0', ['v0.2.0']), /behind released tag v0\.2\.0/);
  assert.throws(() => nextReleaseVersion('0.1.0', ['v1.0.0']), /behind released tag/);
  assert.throws(() => nextReleaseVersion('0.1.0-beta', []), /plain major\.minor\.patch/);
});

test('release whitelist rejects secrets, databases and unexpected directories', async context => {
  const directory = await fixture(context);
  const filenames = releaseFiles('win32');
  for (const filename of filenames) await writeFile(join(directory, filename), filename);
  await verifyWhitelist(directory, filenames);
  await rm(join(directory, 'LICENSE'));
  await assert.rejects(verifyWhitelist(directory, filenames), /Missing release entry: LICENSE/);
  await writeFile(join(directory, 'LICENSE'), 'LICENSE');
  for (const forbidden of ['.env', 'history.sqlite', 'owner.json']) {
    await writeFile(join(directory, forbidden), 'must not ship');
    await assert.rejects(verifyWhitelist(directory, filenames), /Unapproved release entry/);
    await rm(join(directory, forbidden));
  }
  await mkdir(join(directory, '.local'));
  await assert.rejects(verifyWhitelist(directory, filenames), /Unapproved release directory/);
});

test('macOS whitelist permits only the declared bundle files', async context => {
  const directory = await fixture(context);
  const filenames = releaseFiles('darwin');
  for (const filename of filenames) {
    await mkdir(dirname(join(directory, filename)), { recursive: true });
    await writeFile(join(directory, filename), filename);
  }
  await verifyWhitelist(directory, filenames);
  await rm(join(directory, 'LICENSE'));
  await assert.rejects(verifyWhitelist(directory, filenames), /Missing release entry: LICENSE/);
  await writeFile(join(directory, 'LICENSE'), 'LICENSE');
  await rm(join(directory, 'CVTranslate.app/Contents/Info.plist'));
  await assert.rejects(verifyWhitelist(directory, filenames), /Missing release entry/);
  assert.ok(filenames.includes('CVTranslate.app/Contents/Resources/CVTranslate.icns'));
  assert.match(appInfoPlist('0.1.0'), /com\.cvtranslate\.desktop/);
  assert.match(appInfoPlist('0.1.0'), /<key>CFBundleIconFile<\/key><string>CVTranslate\.icns<\/string>/);
  assert.match(appInfoPlist('0.1.0'), /<key>LSUIElement<\/key><true\/>/);
});

test('Windows executable embedding adds every generated icon representation', async context => {
  const directory = await fixture(context);
  const executable = join(directory, 'cvtranslate.exe');
  await writeFile(executable, Buffer.from(NtExecutable.createEmpty(false, false).generate()));
  await embedWindowsExecutableIcon(
    executable,
    new URL('../../internal/platform/icons/application-windows.ico', import.meta.url),
  );
  const resources = NtExecutableResource.from(NtExecutable.from(await readFile(executable)));
  const iconImages = resources.entries.filter(entry => entry.type === 3 && entry.lang === 1033);
  const iconGroups = resources.entries.filter(entry => entry.type === 14 && entry.lang === 1033);
  assert.equal(iconImages.length, 9);
  assert.equal(iconGroups.length, 1);
});

test('macOS application icon contains standard and Retina PNG representations', async () => {
  const icon = await readFile(new URL('../../internal/platform/icons/application-macos.icns', import.meta.url));
  assert.equal(icon.subarray(0, 4).toString('ascii'), 'icns');
  assert.equal(icon.readUInt32BE(4), icon.length);
  const representationTypes = new Set();
  let entryOffset = 8;
  while (entryOffset < icon.length) {
    const representationType = icon.subarray(entryOffset, entryOffset + 4).toString('ascii');
    const entryLength = icon.readUInt32BE(entryOffset + 4);
    assert.ok(entryLength > 8, `Invalid ${representationType} entry length`);
    assert.deepEqual([...icon.subarray(entryOffset + 8, entryOffset + 16)], [137, 80, 78, 71, 13, 10, 26, 10]);
    representationTypes.add(representationType);
    entryOffset += entryLength;
  }
  assert.equal(entryOffset, icon.length);
  assert.deepEqual(
    representationTypes,
    new Set(['icp4', 'icp5', 'icp6', 'ic07', 'ic08', 'ic09', 'ic10', 'ic11', 'ic12', 'ic13', 'ic14']),
  );
});

test('checksums use exact staged bytes and exclude the checksum file itself', async context => {
  const directory = await fixture(context);
  await writeFile(join(directory, 'payload'), 'abc');
  await writeChecksums(directory, ['payload', 'SHA256SUMS']);
  assert.equal(await readFile(join(directory, 'SHA256SUMS'), 'utf8'), 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad  payload\n');
});

test('notices include actual license text, metadata and supplementary notices', async context => {
  const directory = await fixture(context);
  await writeFile(join(directory, 'LICENSE'), 'Actual upstream license text');
  await writeFile(join(directory, 'NOTICE.txt'), 'Upstream attribution');
  await writeFile(join(directory, '.env'), 'never include this secret');
  const notice = await formatNotice({ name: 'example-dependency', version: '1.2.3', license: 'BSD-3-Clause', directory });
  assert.match(notice, /example-dependency@1\.2\.3/);
  assert.match(notice, /BSD-3-Clause/);
  assert.match(notice, /Actual upstream license text/);
  assert.match(notice, /Upstream attribution/);
  assert.doesNotMatch(notice, /secret/);
  await rm(join(directory, 'LICENSE'));
  await rm(join(directory, 'NOTICE.txt'));
  await assert.rejects(formatNotice({ name: 'example', version: '1', license: 'MIT', directory }), /No license\/notice text/);
});

test('reviewed supplements include upstream attribution, full terms and provenance', async context => {
  const directory = await fixture(context);
  const reviewedDependencies = [
    { name: 'cm6-theme-basic-light', version: '0.2.0', attribution: 'Takuya Matsuyama' },
    { name: 'format', version: '0.2.2', attribution: 'Copyright 2010 - 2014 Sami Samhuri' },
    { name: 'mdast-util-highlight-mark', version: '1.2.2', attribution: 'Copyright (c) 2024 remark-highlight-mark' },
    { name: 'micromark-extension-highlight-mark', version: '1.2.0', attribution: 'Copyright (c) 2024 remark-highlight-mark' },
    { name: 'react-remove-scroll-bar', version: '2.3.8', attribution: 'Anton Korzunov' },
  ];
  for (const dependency of reviewedDependencies) {
    const notice = await formatNotice({ ...dependency, license: 'MIT', directory });
    assert.ok(notice.includes(dependency.attribution), dependency.name);
    assert.match(notice, /Permission is hereby granted, free of charge/);
    assert.match(notice, /THE SOFTWARE IS PROVIDED "AS IS"/);
    assert.match(notice, /Supplemental license source: https:\/\//);
    assert.ok(!notice.includes(directory));
  }
});

test('supplements reject unreviewed names, versions, licenses and replacements', async context => {
  const directory = await fixture(context);
  const dependency = { name: 'cm6-theme-basic-light', version: '0.2.0', license: 'MIT', directory };
  for (const mismatch of [
    { name: 'unreviewed-package' },
    { version: '0.2.1' },
    { license: 'Apache-2.0' },
    { license: undefined },
    { replacement: 'local-fork@local' },
  ]) {
    await assert.rejects(formatNotice({ ...dependency, ...mismatch }), /No license\/notice text/);
  }
});

test('packaged license text takes precedence and empty text cannot use a supplement', async context => {
  const directory = await fixture(context);
  const dependency = { name: 'cm6-theme-basic-light', version: '0.2.0', license: 'MIT', directory };
  await writeFile(join(directory, 'LICENSE'), 'License text from the installed package');
  const notice = await formatNotice(dependency);
  assert.match(notice, /License text from the installed package/);
  assert.doesNotMatch(notice, /Supplemental license source/);
  await writeFile(join(directory, 'LICENSE'), ' \n\t');
  await assert.rejects(formatNotice(dependency), /Empty license\/notice text/);
});

test('license metadata supports legacy npm arrays without inventing missing declarations', () => {
  assert.equal(getDeclaredLicense({ license: 'MIT' }), 'MIT');
  assert.equal(getDeclaredLicense({ license: { type: 'BSD-3-Clause' } }), 'BSD-3-Clause');
  assert.equal(getDeclaredLicense({ licenses: [{ type: 'MIT', url: 'http://sjs.mit-license.org' }] }), 'MIT');
  assert.equal(getDeclaredLicense({ license: 'ISC', licenses: [{ type: 'MIT' }] }), 'ISC');
  assert.equal(getDeclaredLicense({}), undefined);
  assert.equal(getDeclaredLicense({ licenses: [] }), undefined);
});

test('Go JSON stream handles braces inside strings and architecture transitive violations', () => {
  const packages = parseJsonStream('{"ImportPath":"cvtranslate/internal/provider/example","Doc":"brace } and \\\"quoted\\\"","Deps":["cvtranslate/internal/storage"]}\n{"ImportPath":"fmt"}');
  assert.equal(packages.length, 2);
  assert.throws(() => verifyArchitecture(packages), /Architecture boundary/);
  verifyArchitecture([{ ImportPath: 'cvtranslate/internal/provider', Imports: ['net/http', 'cvtranslate/internal/domain'] }]);
  assert.throws(() => parseJsonStream('{"unfinished":'), /Incomplete/);
});

test('release version cannot inject paths, linker options or XML', () => {
  assert.equal(validateVersion('0.1.0-beta.1+local'), '0.1.0-beta.1+local');
  for (const value of ['../escape', '0.1.0 -X malicious', '0.1.0<script>', undefined]) assert.throws(() => validateVersion(value));
});

test('real ZIP central directory contains only whitelisted payload bytes', async context => {
  const directory = await fixture(context);
  const payload = join(directory, 'payload');
  await mkdir(payload);
  const filenames = releaseFiles('win32');
  const applicationLicense = await readFile(new URL('../../LICENSE', import.meta.url), 'utf8');
  for (const filename of filenames) {
    const contents = filename === 'LICENSE' ? applicationLicense : `fixture:${filename}`;
    await writeFile(join(payload, filename), contents);
  }
  const destination = join(directory, 'artifact.zip');
  await createArchive(payload, destination, filenames, 'win32');
  const archive = await readFile(destination);
  const trailerOffset = archive.length - 22;
  assert.equal(archive.readUInt32LE(trailerOffset), 0x06054b50);
  const entryCount = archive.readUInt16LE(trailerOffset + 10);
  assert.equal(entryCount, filenames.length);
  let entryOffset = archive.readUInt32LE(trailerOffset + 16);
  const archivedNames = [];
  for (let index = 0; index < entryCount; index += 1) {
    assert.equal(archive.readUInt32LE(entryOffset), 0x02014b50);
    const nameLength = archive.readUInt16LE(entryOffset + 28);
    const extraLength = archive.readUInt16LE(entryOffset + 30);
    const commentLength = archive.readUInt16LE(entryOffset + 32);
    const filename = archive.subarray(entryOffset + 46, entryOffset + 46 + nameLength).toString();
    archivedNames.push(filename);
    const compressedLength = archive.readUInt32LE(entryOffset + 20);
    const localOffset = archive.readUInt32LE(entryOffset + 42);
    const dataOffset = localOffset + 30 + archive.readUInt16LE(localOffset + 26) + archive.readUInt16LE(localOffset + 28);
    const compressed = archive.subarray(dataOffset, dataOffset + compressedLength);
    const content = archive.readUInt16LE(entryOffset + 10) === 8 ? inflateRawSync(compressed) : compressed;
    assert.equal(content.toString(), filename === 'LICENSE' ? applicationLicense : `fixture:${filename}`);
    entryOffset += 46 + nameLength + extraLength + commentLength;
  }
  assert.ok(archivedNames.includes('LICENSE'));
  assert.deepEqual(archivedNames.sort(), [...filenames].sort());
  await writeFile(join(payload, '.env'), 'private');
  await assert.rejects(createArchive(payload, join(directory, 'must-not-exist.zip'), filenames, 'win32'), /Unapproved/);
});
