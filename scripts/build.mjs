import { chmod, copyFile, mkdir, mkdtemp, rename, rm, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { checkPipeline } from './lib/pipeline.mjs';
import { collectNotices } from './lib/notices.mjs';
import { appInfoPlist, createArchive, embedWindowsExecutableIcon, releaseFiles, validateVersion, verifyWhitelist, writeChecksums } from './lib/release.mjs';
import { exists, main, readPackage, rootDirectory, run } from './lib/process.mjs';

main(async () => {
  const buildArguments = process.argv.slice(2);
  const releaseVersionPrefix = '--release-version=';
  const releaseVersionArgument = buildArguments.find(argument => argument.startsWith(releaseVersionPrefix));
  if (buildArguments.length > (releaseVersionArgument ? 1 : 0)) {
    throw new Error('Usage: node scripts/build.mjs [--release-version=X.Y.Z] (current host only)');
  }
  if (!['win32', 'darwin'].includes(process.platform)) throw new Error('Release builds support only Windows and macOS hosts.');
  const architecture = { x64: 'amd64', arm64: 'arm64' }[process.arch];
  if (!architecture) throw new Error(`Unsupported current host architecture: ${process.arch}`);
  const platform = process.platform === 'win32' ? 'windows' : 'darwin';
  const goEnvironment = JSON.parse(await run('go', ['env', '-json', 'GOOS', 'GOARCH', 'GOVERSION', 'CGO_ENABLED'], { capture: true }));
  if (goEnvironment.GOOS !== platform || goEnvironment.GOARCH !== architecture) {
    throw new Error('GOOS/GOARCH must match this Node host. Cross-compilation is not a release validation substitute.');
  }
  if (platform === 'darwin' && goEnvironment.CGO_ENABLED !== '1') {
    throw new Error('macOS releases require CGO_ENABLED=1 and the native compiler toolchain.');
  }
  const metadata = await readPackage();
  const version = validateVersion(releaseVersionArgument?.slice(releaseVersionPrefix.length) ?? metadata.version);
  const label = `cvtranslate-${version}-${platform}-${architecture}`;
  const releaseDirectory = join(rootDirectory, 'release');
  const stagingDirectory = join(releaseDirectory, '.staging');
  const finalDirectory = join(releaseDirectory, label);
  await mkdir(stagingDirectory, { recursive: true });
  // A separate exclusive reservation prevents two builds publishing the same version.
  const reservation = join(stagingDirectory, `${label}.lock`);
  try { await mkdir(reservation); } catch (error) {
    if (error.code === 'EEXIST') throw new Error(`A build owns ${label}; inspect its staging lock before retrying.`);
    throw error;
  }
  let stage;
  try {
    if (await exists(finalDirectory)) throw new Error(`Release already exists: ${finalDirectory}. It will not be overwritten.`);
    stage = await mkdtemp(join(stagingDirectory, `${label}-`));
    const payload = join(stage, 'payload');
    await mkdir(payload);
    await checkPipeline({ compile: false });
    let commit = 'unavailable';
    let dirty = 'unavailable';
    try {
      const gitRoot = await run('git', ['rev-parse', '--show-toplevel'], { capture: true });
      if (resolve(gitRoot).toLowerCase() === resolve(rootDirectory).toLowerCase()) {
        const candidate = await run('git', ['rev-parse', 'HEAD'], { capture: true });
        if (/^[a-f0-9]{40,64}$/.test(candidate)) commit = candidate;
        dirty = Boolean(await run('git', ['status', '--porcelain'], { capture: true }));
      }
    } catch { /* Git is optional; absence is recorded explicitly. */ }
    const buildTime = new Date().toISOString();
    const executableRelative = process.platform === 'darwin' ? 'CVTranslate.app/Contents/MacOS/cvtranslate' : 'cvtranslate.exe';
    const executable = join(payload, executableRelative);
    if (process.platform === 'darwin') {
      await mkdir(join(payload, 'CVTranslate.app', 'Contents', 'MacOS'), { recursive: true });
      await mkdir(join(payload, 'CVTranslate.app', 'Contents', 'Resources'), { recursive: true });
      await writeFile(join(payload, 'CVTranslate.app', 'Contents', 'Info.plist'), appInfoPlist(version));
      await copyFile(
        join(rootDirectory, 'internal', 'platform', 'icons', 'application-macos.icns'),
        join(payload, 'CVTranslate.app', 'Contents', 'Resources', 'CVTranslate.icns'),
      );
    }
    const linkerFlags = `${platform === 'windows' ? '-s -w -H=windowsgui ' : ''}-X main.version=${version} -X main.commit=${commit} -X main.buildTime=${buildTime}`;
    await run('go', ['build', '-trimpath', '-buildvcs=false', '-ldflags', linkerFlags, '-o', executable, './cmd/cvtranslate']);
    if (process.platform === 'darwin') {
      await chmod(executable, 0o755);
    } else {
      await embedWindowsExecutableIcon(
        executable,
        join(rootDirectory, 'internal', 'platform', 'icons', 'application-windows.ico'),
      );
    }
    const versionOutput = await run(executable, ['--version', '--no-native', '--no-open'], { capture: true });
    if (!versionOutput.includes(version)) throw new Error('Version smoke check did not report the package version.');
    await copyFile(join(rootDirectory, 'LICENSE'), join(payload, 'LICENSE'));
    await copyFile(join(rootDirectory, 'docs', 'release.md'), join(payload, 'RELEASE.md'));
    await writeFile(join(payload, 'THIRD-PARTY-NOTICES.txt'), await collectNotices());
    await writeFile(join(payload, 'build-manifest.json'), `${JSON.stringify({
      schemaVersion: 1, application: metadata.name, version, commit, dirty, buildTime,
      platform, architecture, nodeVersion: process.version, goVersion: goEnvironment.GOVERSION,
      cgoEnabled: goEnvironment.CGO_ENABLED, applicationLicense: metadata.license ?? 'unspecified',
      signing: 'not performed', notarization: 'not performed',
      verification: 'automated checks and side-effect-free version smoke; not desktop acceptance',
    }, null, 2)}\n`);
    const whitelist = releaseFiles(process.platform);
    await writeChecksums(payload, whitelist);
    const archiveName = `${label}.zip`;
    const archive = join(stage, archiveName);
    await createArchive(payload, archive, whitelist);
    await rename(archive, join(payload, archiveName));
    await writeChecksums(payload, [...whitelist, archiveName]);
    await verifyWhitelist(payload, [...whitelist, archiveName]);
    if (await exists(finalDirectory)) throw new Error('Release destination appeared during build; refusing to overwrite it.');
    await rename(payload, finalDirectory);
    console.log(`Release created: ${finalDirectory}`);
    console.log('Unsigned local artifact. No uploads, signing, notarization or desktop acceptance were performed.');
  } finally {
    if (stage) await rm(stage, { recursive: true, force: true });
    await rm(reservation, { recursive: true, force: true });
  }
});
