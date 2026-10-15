import { test } from 'node:test';
import assert from 'node:assert/strict';
import { generateKeyPairSync } from 'node:crypto';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { archiveName, archPackageVersion, createArchRecipe, linuxPackageName, parseTag, prepareMetadata, releaseChannel, targets, validateBinary, validateInstallerLayout, validateReleaseChannel, validateSigningKey, validateUpdateLayout } from './release.mjs';

test('release tags reject invalid versions and command injection', () => {
  assert.deepEqual(parseTag('v1.2.3'), { version: '1.2.3', numericVersion: '1.2.3' });
  assert.deepEqual(parseTag('v1.2.3-beta.1'), { version: '1.2.3-beta.1', numericVersion: '1.2.3' });
  for (const tag of ['main', 'v1.2', 'v01.2.3', 'v1.2.3-beta.01', 'v65536.2.3', 'v1.2.3;echo bad', 'v1.2.3\nkey=value']) assert.throws(() => parseTag(tag));
});

test('update manifests follow the release tag channel, including prerelease counters', () => {
  for (const [tag, channel] of [
    ['v1.2.3', 'stable'], ['1.2.3+build.alpha', 'stable'],
    ['v1.2.3-alpha', 'alpha'], ['v1.2.3-alpha.10+build.4', 'alpha'],
    ['v1.2.3-beta.2', 'beta'], ['v1.2.3-rc.1', 'rc'],
  ]) {
    assert.equal(releaseChannel(tag), channel);
    assert.equal(validateReleaseChannel(tag, channel !== 'stable'), channel);
    assert.throws(() => validateReleaseChannel(tag, channel === 'stable'), /prerelease status/);
  }
  for (const tag of ['dev', 'v1.2', 'v1.2.3-alpha.01', 'v1.2.3-preview.1', 'v1.2.3-alpha1', 'v1.2.3-RC.1']) {
    assert.throws(() => releaseChannel(tag));
  }
  assert.throws(() => validateReleaseChannel('v1.2.3', undefined), /prerelease status/);
});

test('all six update targets have distinct supported archive formats', () => {
  const names = targets.map(target => archiveName('1.2.3', ...target.split('-')));
  assert.equal(new Set(names).size, 6);
  assert.equal(names.filter(name => name.endsWith('.tar.gz')).length, 2);
  assert.equal(names.filter(name => name.endsWith('.zip')).length, 4);
  assert.throws(() => archiveName('1.2.3', 'linux', '386'));
});

test('Linux installers stay separate from the updater archives', () => {
  for (const arch of ['amd64', 'arm64']) {
    const names = ['deb', 'rpm', 'archlinux', 'appimage'].map(format => linuxPackageName('1.2.3', arch, format));
    assert.equal(new Set(names).size, 4);
    assert.ok(names.every(name => name !== archiveName('1.2.3', 'linux', arch)));
    assert.ok(names.some(name => name.endsWith('.pkg.tar.zst')));
  }
});

test('Arch prereleases remain distinct from the stable package version', () => {
  assert.equal(archPackageVersion('v1.2.3-beta.1'), '1.2.3_beta.1');
  assert.equal(archPackageVersion('1.2.3'), '1.2.3');
  assert.notEqual(archPackageVersion('1.2.3-beta.1'), archPackageVersion('1.2.3'));
});

test('AUR recipes pin both architecture downloads to their actual checksums', t => {
  const root = mkdtempSync(join(tmpdir(), 'mier-aur-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  writeFileSync(join(root, linuxPackageName('1.2.3', 'amd64', 'archlinux')), 'amd64 package');
  writeFileSync(join(root, linuxPackageName('1.2.3', 'arm64', 'archlinux')), 'arm64 package');
  createArchRecipe('v1.2.3', root, join(root, 'aur'));
  const pkgbuild = readFileSync(join(root, 'aur/PKGBUILD'), 'utf8');
  const srcinfo = readFileSync(join(root, 'aur/.SRCINFO'), 'utf8');
  assert.match(pkgbuild, /sha256sums_x86_64=\('[a-f0-9]{64}'\)/);
  assert.match(pkgbuild, /sha256sums_aarch64=\('[a-f0-9]{64}'\)/);
  assert.ok(!pkgbuild.includes('SKIP'));
  for (const arch of ['x86_64', 'aarch64']) {
    const checksum = new RegExp(`sha256sums_${arch}=\\('([a-f0-9]+)'\\)`).exec(pkgbuild)[1];
    assert.ok(srcinfo.includes(`sha256sums_${arch} = ${checksum}`));
  }
  assert.match(pkgbuild, /usr\/share\/applications\/mag-ik-een-review\.desktop/);
  assert.throws(() => createArchRecipe('v1.2.4', root, join(root, 'other')), /ENOENT/);
  assert.throws(() => validateInstallerLayout(root, '1.2.3'), /Unexpected/);
});

test('installer publication requires all targets and rejects another release version', t => {
  const directory = mkdtempSync(join(tmpdir(), 'mier-installers-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  for (const arch of ['amd64', 'arm64']) {
    for (const format of ['deb', 'rpm', 'archlinux', 'appimage']) writeFileSync(join(directory, linuxPackageName('1.2.3', arch, format)), 'package');
    writeFileSync(join(directory, `mier-1.2.3-darwin-${arch}.dmg`), 'dmg');
    writeFileSync(join(directory, `mier-1.2.3-windows-${arch}-setup.exe`), 'nsis');
  }
  assert.doesNotThrow(() => validateInstallerLayout(directory, '1.2.3'));
  writeFileSync(join(directory, 'mier-1.2.2-linux-amd64.deb'), 'old package');
  assert.throws(() => validateInstallerLayout(directory, '1.2.3'), /Unexpected/);
});

test('private signing key must match the embedded public key', () => {
  const pair = generateKeyPairSync('ed25519');
  const other = generateKeyPairSync('ed25519');
  const privatePEM = pair.privateKey.export({ type: 'pkcs8', format: 'pem' });
  const publicPEM = pair.publicKey.export({ type: 'spki', format: 'pem' });
  assert.doesNotThrow(() => validateSigningKey(privatePEM, publicPEM));
  assert.throws(() => validateSigningKey('', publicPEM));
  assert.throws(() => validateSigningKey(other.privateKey.export({ type: 'pkcs8', format: 'pem' }), publicPEM), /does not match/);
});

test('a build labeled with the wrong CPU architecture is rejected', () => {
  for (const platform of ['linux', 'darwin', 'windows']) {
    for (const arch of ['amd64', 'arm64']) {
      const bytes = Buffer.alloc(256);
      if (platform === 'linux') {
        bytes.set([127, 69, 76, 70, 2, 1]);
        bytes.writeUInt16LE(arch === 'arm64' ? 183 : 62, 18);
      } else if (platform === 'darwin') {
        bytes.writeUInt32LE(0xfeedfacf);
        bytes.writeUInt32LE(arch === 'arm64' ? 0x100000c : 0x1000007, 4);
      } else {
        bytes.write('MZ');
        bytes.writeUInt32LE(128, 0x3c);
        bytes.writeUInt32LE(0x4550, 128);
        bytes.writeUInt16LE(arch === 'arm64' ? 0xaa64 : 0x8664, 132);
      }
      assert.doesNotThrow(() => validateBinary(bytes, platform, arch));
      assert.throws(() => validateBinary(bytes, platform, arch === 'amd64' ? 'arm64' : 'amd64'), /not/);
    }
  }
  assert.throws(() => validateBinary(Buffer.alloc(4), 'windows', 'amd64'), /truncated/);
});

test('an incomplete release or installer mixed into the update feed is rejected', t => {
  const directory = mkdtempSync(join(tmpdir(), 'mier-release-layout-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  assert.throws(() => validateUpdateLayout(directory, '1.2.3'));
  for (const target of targets) writeFileSync(join(directory, archiveName('1.2.3', ...target.split('-'))), 'archive');
  assert.doesNotThrow(() => validateUpdateLayout(directory, '1.2.3'));
  writeFileSync(join(directory, 'mier-1.2.3-windows-amd64-setup.exe'), 'installer');
  assert.throws(() => validateUpdateLayout(directory, '1.2.3'));
});

test('prereleases keep valid numeric Windows/macOS metadata', t => {
  const root = mkdtempSync(join(tmpdir(), 'mier-release-metadata-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const dir of ['build/windows/nsis', 'build/darwin']) mkdirSync(join(root, dir), { recursive: true });
  writeFileSync(join(root, 'build/config.yml'), 'info:\n  version: "0.1.0"\n');
  writeFileSync(join(root, 'build/windows/info.json'), JSON.stringify({ fixed: { file_version: '0.1.0' }, info: { '0000': { ProductVersion: '0.1.0' } } }));
  writeFileSync(join(root, 'build/windows/nsis/wails_tools.nsh'), '!define INFO_PRODUCTVERSION "0.1.0"\n');
  writeFileSync(join(root, 'build/darwin/Info.plist'), '<key>CFBundleVersion</key><string>0.1.0</string><key>CFBundleShortVersionString</key><string>0.1.0</string>');
  prepareMetadata(root, '1.2.3-beta.1');
  assert.equal(JSON.parse(readFileSync(join(root, 'build/windows/info.json'))).fixed.file_version, '1.2.3');
  assert.match(readFileSync(join(root, 'build/windows/nsis/wails_tools.nsh'), 'utf8'), /INFO_PRODUCTVERSION "1\.2\.3"/);
  assert.match(readFileSync(join(root, 'build/darwin/Info.plist'), 'utf8'), /<string>1\.2\.3<\/string>/);
});
