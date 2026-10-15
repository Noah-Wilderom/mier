import { createHash, createPrivateKey, createPublicKey, timingSafeEqual } from 'node:crypto';
import { appendFileSync, copyFileSync, mkdirSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { basename, join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';
import { validateMacOSCredentials, withMacOSSigning, withoutSigningSecrets } from './macos-sign.mjs';
import { advanceMacOSRelease } from './macos-notarization.mjs';

export const targets = ['darwin-amd64', 'darwin-arm64', 'linux-amd64', 'linux-arm64', 'windows-amd64', 'windows-arm64'];
const appName = 'mag-ik-een-review';
const publicKeyPath = 'build/updater.key.pub';

export function parseTag(tag) {
  const match = /^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/.exec(tag ?? '');
  if (!match || match.slice(1, 4).some(n => Number(n) > 65535) || match[4]?.split('.').some(n => /^0\d+$/.test(n))) {
    throw new Error('Release tag must be SemVer, such as v0.1.0, with version components below 65536.');
  }
  return { version: tag.replace(/^v/, ''), numericVersion: match.slice(1, 4).join('.') };
}

export function archiveName(version, platform, arch) {
  parseTag(version);
  if (!targets.includes(`${platform}-${arch}`)) throw new Error(`Unsupported release target: ${platform}-${arch}`);
  return `mier-${version}-${platform}-${arch}.${platform === 'linux' ? 'tar.gz' : 'zip'}`;
}

export function validateSigningKey(privatePEM, publicPEM) {
  if (!privatePEM) throw new Error('Configure the UPDATER_PRIVATE_KEY repository secret before publishing a release.');
  const privateKey = createPrivateKey(privatePEM);
  const publicKey = createPublicKey(publicPEM);
  if (privateKey.asymmetricKeyType !== 'ed25519' || publicKey.asymmetricKeyType !== 'ed25519') throw new Error('Updater signing keys must be Ed25519.');
  const actual = createPublicKey(privateKey).export({ type: 'spki', format: 'der' });
  const expected = publicKey.export({ type: 'spki', format: 'der' });
  if (actual.length !== expected.length || !timingSafeEqual(actual, expected)) throw new Error('UPDATER_PRIVATE_KEY does not match build/updater.key.pub.');
}

export function validateBinary(bytes, platform, arch) {
  if (bytes.length < 64) throw new Error('Built executable is truncated.');
  let machine;
  if (platform === 'linux' && bytes.subarray(0, 4).equals(Buffer.from([127, 69, 76, 70])) && bytes[4] === 2 && bytes[5] === 1) {
    machine = bytes.readUInt16LE(18);
    if (machine === (arch === 'arm64' ? 183 : 62)) return;
  } else if (platform === 'darwin' && bytes.readUInt32LE(0) === 0xfeedfacf) {
    machine = bytes.readUInt32LE(4);
    if (machine === (arch === 'arm64' ? 0x100000c : 0x1000007)) return;
  } else if (platform === 'windows' && bytes.toString('ascii', 0, 2) === 'MZ') {
    const offset = bytes.readUInt32LE(0x3c);
    if (offset + 6 <= bytes.length && bytes.readUInt32LE(offset) === 0x4550) {
      machine = bytes.readUInt16LE(offset + 4);
      if (machine === (arch === 'arm64' ? 0xaa64 : 0x8664)) return;
    }
  }
  throw new Error(`Built executable is not ${platform}/${arch} (machine=${machine ?? 'unknown'}).`);
}

export function prepareMetadata(root, version) {
  const { numericVersion } = parseTag(version);
  const config = join(root, 'build/config.yml');
  const yaml = readFileSync(config, 'utf8');
  if (!/^  version:.*$/m.test(yaml)) throw new Error('Missing application version in build/config.yml.');
  writeFileSync(config, yaml.replace(/^  version:.*$/m, `  version: "${numericVersion}"`));
  const infoPath = join(root, 'build/windows/info.json');
  const info = JSON.parse(readFileSync(infoPath, 'utf8'));
  info.fixed.file_version = numericVersion;
  info.info['0000'].ProductVersion = numericVersion;
  writeFileSync(infoPath, JSON.stringify(info, null, 2) + '\n');
  const toolsPath = join(root, 'build/windows/nsis/wails_tools.nsh');
  writeFileSync(toolsPath, readFileSync(toolsPath, 'utf8').replace(/(!define\s+INFO_PRODUCTVERSION\s+")[^"]*(")/, `$1${numericVersion}$2`));
  const plistPath = join(root, 'build/darwin/Info.plist');
  let plist = readFileSync(plistPath, 'utf8');
  for (const key of ['CFBundleVersion', 'CFBundleShortVersionString']) {
    const pattern = new RegExp(`(<key>${key}</key>\\s*<string>)[^<]*(</string>)`);
    if (!pattern.test(plist)) throw new Error(`Missing ${key} in macOS bundle metadata.`);
    plist = plist.replace(pattern, `$1${numericVersion}$2`);
  }
  writeFileSync(plistPath, plist);
}

function run(command, args, extraEnv = {}, cwd) {
  const result = spawnSync(command, args, { stdio: 'inherit', env: withoutSigningSecrets({ ...process.env, ...extraEnv }), shell: false, cwd });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${command} exited with status ${result.status}.`);
}

function packageTarget(platform, arch, version) {
  const filename = archiveName(version, platform, arch);
  const host = process.platform === 'win32' ? 'windows' : process.platform;
  if (platform !== host || (platform !== 'windows' && process.arch !== (arch === 'amd64' ? 'x64' : 'arm64'))) {
    throw new Error('Use a native runner for macOS/Linux and a Windows runner for Windows packaging.');
  }
  prepareMetadata('.', version);
  if (platform === 'darwin') validateMacOSCredentials();
  mkdirSync('dist/updates', { recursive: true });
  mkdirSync('dist/installers', { recursive: true });
  const binary = `bin/${appName}${platform === 'windows' ? '.exe' : ''}`;
  // The build/package wrappers translate GOARCH into Task's ARCH variable.
  // Passing ARCH directly is overwritten by the wrapper's host default.
  const args = [`GOOS=${platform}`, `GOARCH=${arch}`, `APP_VERSION=${version}`];
  run('wails3', [platform === 'linux' ? 'build' : 'package', ...args, ...(platform === 'windows' ? ['INSTALL_SCOPE=user'] : [])]);
  validateBinary(readFileSync(binary), platform, arch);
  const archive = resolve('dist/updates', filename);
  if (platform === 'linux') {
    run('tar', ['-czf', archive, '-C', 'bin', appName]);
    packageLinuxInstallers(arch, version);
  } else if (platform === 'windows') {
    run('pwsh', ['-NoLogo', '-NoProfile', '-NonInteractive', '-Command', 'Compress-Archive -LiteralPath $env:MIER_ZIP_INPUT -DestinationPath $env:MIER_ZIP_OUTPUT -Force'], {
      MIER_ZIP_INPUT: resolve(binary), MIER_ZIP_OUTPUT: archive,
    });
    copyFileSync(`bin/${appName}-${arch}-installer.exe`, `dist/installers/mier-${version}-windows-${arch}-setup.exe`);
  } else {
    withMacOSSigning(signing => advanceMacOSRelease({
      ...macOSReleasePaths(arch, version), appBundle: resolve('bin/' + appName + '.app'), signing,
    }));
  }
}

export function macOSReleasePaths(arch, version, commit = process.env.RELEASE_COMMIT, root = 'dist') {
  const archive = archiveName(version, 'darwin', arch);
  if (!/^[a-f0-9]{40}$/.test(commit || '')) throw new Error('Missing captured release commit.');
  return {
    arch, version, commit, checkpoint: resolve(root, 'notarization', 'darwin-' + arch, 'state.json'),
    archive: resolve(root, 'updates', archive), dmg: resolve(root, 'installers', 'mier-' + version + '-darwin-' + arch + '.dmg'),
  };
}

export function linuxPackageName(version, arch, format) {
  archiveName(version, 'linux', arch);
  const extensions = { deb: 'deb', rpm: 'rpm', archlinux: 'pkg.tar.zst', appimage: 'AppImage' };
  if (!extensions[format]) throw new Error(`Unsupported Linux package format: ${format}`);
  return `mier-${version}-linux-${arch}.${extensions[format]}`;
}

function packageNativeLinux(arch, version, format, extension) {
  validateBinary(readFileSync(`bin/${appName}`), 'linux', arch);
  let config = 'build/linux/nfpm/nfpm.yaml';
  if (format === 'archlinux') {
    // This pinned nFPM version loses prereleases in Arch's .PKGINFO. Supply
    // a literal pacman-compatible version so rc and final packages differ.
    mkdirSync('dist', { recursive: true });
    config = 'dist/arch-nfpm.yaml';
    writeFileSync(config, readFileSync('build/linux/nfpm/nfpm.yaml', 'utf8').replace(/^version:.*$/m, `version: "${archPackageVersion(version)}"\nversion_schema: none`));
  }
  run('wails3', ['tool', 'package', '-name', appName, '-format', format, '-config', config, '-out', 'bin'], { GOARCH: arch, APP_VERSION: version });
  mkdirSync('dist/installers', { recursive: true });
  copyFileSync(`bin/${appName}.${extension}`, `dist/installers/${linuxPackageName(version, arch, format)}`);
}

function packageLinuxInstallers(arch, version) {
  const binary = `bin/${appName}`;
  validateBinary(readFileSync(binary), 'linux', arch);
  mkdirSync('dist/installers', { recursive: true });
  for (const [format, extension] of [['deb', 'deb'], ['rpm', 'rpm'], ['archlinux', 'pkg.tar.zst']]) {
    packageNativeLinux(arch, version, format, extension);
  }
  // Give linuxdeploy a matching icon basename and run without requiring FUSE
  // in CI. Wails' generator chooses the architecture of this native runner.
  const stage = resolve('dist/appimage-stage');
  mkdirSync(stage, { recursive: true });
  const icon = join(stage, `${appName}.png`);
  copyFileSync('build/appicon.png', icon);
  run('wails3', ['generate', 'appimage', '-binary', resolve(binary), '-icon', icon, '-desktopfile', resolve(`build/linux/${appName}.desktop`), '-outputdir', resolve('dist/installers'), '-builddir', stage], { APPIMAGE_EXTRACT_AND_RUN: '1' });
  const appImage = join('dist/installers', `${appName}-${arch === 'amd64' ? 'x86_64' : 'aarch64'}.AppImage`);
  validateBinary(readFileSync(appImage), 'linux', arch);
  copyFileSync(appImage, `dist/installers/${linuxPackageName(version, arch, 'appimage')}`);
  rmSync(appImage);
  run('python3', ['scripts/verify-linux-packages.py', 'dist/installers', arch, version]);
}

export function archPackageVersion(version) {
  parseTag(version);
  return version.replace(/^v/, '').replaceAll('-', '_');
}

export function validateUpdateLayout(directory, version) {
  const expected = new Set(targets.map(target => archiveName(version, ...target.split('-'))));
  const actual = readdirSync(directory);
  if (actual.length !== expected.size || actual.some(name => !expected.has(name))) throw new Error('Update archives must contain exactly the six supported platform/architecture targets.');
}

export function createArchRecipe(tag, directory, output, repository = 'Noah-Wilderom/mier') {
  const { version } = parseTag(tag);
  if (!/^[\w.-]+\/[\w.-]+$/.test(repository)) throw new Error('Invalid GitHub repository.');
  const sources = [['x86_64', 'amd64'], ['aarch64', 'arm64']].map(([arch, goarch]) => {
    const filename = linuxPackageName(version, goarch, 'archlinux');
    return { arch, filename, url: `https://github.com/${repository}/releases/download/${encodeURIComponent(tag)}/${filename}`, checksum: createHash('sha256').update(readFileSync(join(directory, filename))).digest('hex') };
  });
  const pkgver = archPackageVersion(version);
  const url = `https://github.com/${repository}`;
  const depends = ['git', 'glibc>=2.39', 'gtk4>=4.14', 'webkitgtk-6.0'];
  mkdirSync(output, { recursive: true });
  writeFileSync(join(output, 'PKGBUILD'), `# Generated from release ${tag}; publish with .SRCINFO to the AUR.
pkgname=mier-bin
pkgver='${pkgver}'
pkgrel=1
pkgdesc='Mag ik een review — a desktop Git and GitHub pull request workspace'
arch=('x86_64' 'aarch64')
url='${url}'
license=('unknown')
depends=(${depends.map(value => `'${value}'`).join(' ')})
optdepends=('gnome-keyring: saved GitHub tokens' 'xdg-desktop-portal: native file dialogs')
provides=('mier')
conflicts=('mier')
options=('!strip' '!debug')
${sources.map(source => `source_${source.arch}=('${source.url}')\nsha256sums_${source.arch}=('${source.checksum}')`).join('\n')}

package() {
  install -Dm755 "$srcdir/usr/bin/mag-ik-een-review" "$pkgdir/usr/bin/mag-ik-een-review"
  ln -s mag-ik-een-review "$pkgdir/usr/bin/mier"
  install -Dm644 "$srcdir/usr/share/applications/mag-ik-een-review.desktop" "$pkgdir/usr/share/applications/mag-ik-een-review.desktop"
  install -Dm644 "$srcdir/usr/share/pixmaps/mag-ik-een-review.png" "$pkgdir/usr/share/pixmaps/mag-ik-een-review.png"
}
`);
  const fields = [
    ['pkgbase', 'mier-bin'], ['pkgdesc', 'Mag ik een review — a desktop Git and GitHub pull request workspace'],
    ['pkgver', pkgver], ['pkgrel', '1'], ['url', url], ['arch', 'x86_64'], ['arch', 'aarch64'], ['license', 'unknown'],
    ...depends.map(value => ['depends', value]), ['optdepends', 'gnome-keyring: saved GitHub tokens'], ['optdepends', 'xdg-desktop-portal: native file dialogs'],
    ['provides', 'mier'], ['conflicts', 'mier'], ['options', '!strip'], ['options', '!debug'],
    ...sources.flatMap(source => [[`source_${source.arch}`, source.url], [`sha256sums_${source.arch}`, source.checksum]]),
    ['pkgname', 'mier-bin'],
  ];
  writeFileSync(join(output, '.SRCINFO'), fields.map(([key, value]) => `${key === 'pkgname' ? '\n' : ''}${key === 'pkgbase' || key === 'pkgname' ? '' : '\t'}${key} = ${value}`).join('\n') + '\n');
}

export function validateInstallerLayout(directory, version) {
  const expected = [
    ...['amd64', 'arm64'].flatMap(arch => ['deb', 'rpm', 'archlinux', 'appimage'].map(format => linuxPackageName(version, arch, format))),
    ...['amd64', 'arm64'].flatMap(arch => [`mier-${version}-darwin-${arch}.dmg`, `mier-${version}-windows-${arch}-setup.exe`]),
  ];
  const allowed = new Set([...expected, `mier-${version}-aur.tar.gz`]);
  for (const name of readdirSync(directory)) {
    if (!allowed.has(name)) throw new Error(`Unexpected release installer: ${name}`);
  }
  for (const name of expected) {
    const file = statSync(join(directory, name));
    if (!file.isFile() || file.size === 0) throw new Error(`Release installer is empty: ${name}`);
  }
}

function publishManifest(tag) {
  const { version } = parseTag(tag);
  const privatePEM = process.env.UPDATER_PRIVATE_KEY;
  validateSigningKey(privatePEM, readFileSync(publicKeyPath));
  validateUpdateLayout('dist/updates', version);
  validateInstallerLayout('dist/installers', version);
  const repository = process.env.GITHUB_REPOSITORY || 'Noah-Wilderom/mier';
  if (!/^[\w.-]+\/[\w.-]+$/.test(repository)) throw new Error('Invalid GitHub repository.');
  createArchRecipe(tag, 'dist/installers', 'dist/aur', repository);
  run('tar', ['-czf', resolve(`dist/installers/mier-${version}-aur.tar.gz`), '-C', 'dist/aur', 'PKGBUILD', '.SRCINFO']);
  const keyDirectory = resolve(process.env.RUNNER_TEMP || 'dist', 'mier-signing');
  mkdirSync(keyDirectory, { recursive: true, mode: 0o700 });
  const privateKeyPath = join(keyDirectory, 'updater.key');
  try {
    writeFileSync(privateKeyPath, privatePEM, { mode: 0o600 });
    const event = process.env.GITHUB_EVENT_PATH ? JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8')) : {};
    const notes = process.env.MIER_RELEASE_NOTES_FILE ? readFileSync(process.env.MIER_RELEASE_NOTES_FILE, 'utf8') : event.release?.body || '';
    run('wails3', ['updater', 'manifest', '-version', version, '-channel', 'stable', '-name', `M.I.E.R. ${version}`, '-notes', notes, '-key', privateKeyPath, '-url-prefix', `https://github.com/${repository}/releases/download/${encodeURIComponent(tag)}/`, '-output', 'dist/manifest.json', 'dist/updates']);
    run('wails3', ['updater', 'verify', '-manifest', 'dist/manifest.json', '-publickey', publicKeyPath, '-dir', 'dist/updates']);
    const files = ['updates', 'installers'].flatMap(dir => readdirSync(`dist/${dir}`).map(name => `dist/${dir}/${name}`)).sort();
    writeFileSync('dist/SHA256SUMS', files.map(file => `${createHash('sha256').update(readFileSync(file)).digest('hex')}  ${basename(file)}`).join('\n') + '\n');
  } finally {
    rmSync(privateKeyPath, { force: true });
  }
}

function main(args) {
  if (args[0] === 'validate') {
    const { version } = parseTag(args[1]);
    validateSigningKey(process.env.UPDATER_PRIVATE_KEY, readFileSync(publicKeyPath));
    validateMacOSCredentials();
    if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `version=${version}\n`);
    console.log(`Release ${version} signing key matches the pinned public key.`);
  } else if (args[0] === 'package') {
    packageTarget(args[1], args[2], args[3]);
  } else if (args[0] === 'resume-macos') {
    if (process.platform !== 'darwin') throw new Error('Resuming macOS notarization requires a macOS runner.');
    withMacOSSigning(signing => advanceMacOSRelease({ ...macOSReleasePaths(args[1], args[2]), signing }, true));
  } else if (args[0] === 'manifest') {
    publishManifest(args[1]);
  } else if (args[0] === 'linux-packages') {
    parseTag(args[2]);
    packageLinuxInstallers(args[1], args[2]);
  } else if (args[0] === 'aur') {
    createArchRecipe(args[1], args[2], args[3]);
  } else if (args[0] === 'arch-package') {
    packageNativeLinux(args[1], args[2], 'archlinux', 'pkg.tar.zst');
  } else {
    throw new Error('Usage: release.mjs validate TAG | package PLATFORM ARCH VERSION | resume-macos ARCH VERSION | linux-packages ARCH VERSION | aur TAG INSTALLERS OUTPUT | manifest TAG');
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try { main(process.argv.slice(2)); }
  catch (error) { console.error(error.message); process.exitCode = 1; }
}
