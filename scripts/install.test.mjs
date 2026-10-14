import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const installer = resolve(dirname(fileURLToPath(import.meta.url)), '../install.sh');
const repositoryURL = 'https://github.com/Noah-Wilderom/mier/releases/download/';
const bash = '/bin/bash';
const command = name => execFileSync('/bin/sh', ['-c', 'command -v "$1"', '_', name], { encoding: 'utf8' }).trim();
const application = '#!/usr/bin/env bash\nprintf "FUSE-free:%s\\n" "$APPIMAGE_EXTRACT_AND_RUN"\nprintf "argument:%s\\n" "$@"\n';

function release(tag, overrides = {}) {
  const version = tag.replace(/^v/, '');
  const names = ['SHA256SUMS', 'manifest.json'];
  for (const arch of ['amd64', 'arm64']) {
    for (const extension of ['deb', 'rpm', 'pkg.tar.zst', 'AppImage']) names.push('mier-' + version + '-linux-' + arch + '.' + extension);
    names.push('mier-' + version + '-darwin-' + arch + '.zip', 'mier-' + version + '-windows-' + arch + '-setup.exe');
  }
  return {
    tag_name: tag, draft: false, prerelease: version.includes('-'), published_at: '2026-10-14T12:00:00Z',
    assets: names.map(name => ({ name, state: 'uploaded', browser_download_url: repositoryURL + encodeURIComponent(tag) + '/' + name })),
    ...overrides,
  };
}

function fixture(t, releases = [release('v1.2.3')]) {
  const root = mkdtempSync(join(tmpdir(), 'mier-installer-test-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const bin = join(root, 'bin'), temporary = join(root, 'temporary');
  mkdirSync(bin); mkdirSync(temporary);
  for (const name of ['bash', 'jq', 'awk', 'cat', 'mktemp', 'rm', 'mv', 'chmod', 'install', 'mkdir', 'cp', 'unzip']) {
    symlinkSync(command(name), join(bin, name));
  }
  symlinkSync(process.execPath, join(bin, 'node'));
  // Exercise both supported host hashing utilities.
  const hasher = process.platform === 'darwin' ? 'shasum' : 'sha256sum';
  symlinkSync(command(hasher), join(bin, hasher));
  const stateFile = join(root, 'state.json'), callsFile = join(root, 'calls.jsonl');
  const state = { pages: [releases], payload: join(root, 'payload'), checksum: '', callsFile };
  writeFileSync(state.payload, application);
  state.checksum = createHash('sha256').update(application).digest('hex');
  const save = () => writeFileSync(stateFile, JSON.stringify(state));
  const writeCommand = (name, code) => {
    const file = join(bin, name);
    rmSync(file, { force: true });
    writeFileSync(file, '#!/usr/bin/env node\n' + code, { mode: 0o755 });
  };
  writeCommand('uname', 'console.log(process.argv[2] === "-s" ? process.env.MIER_TEST_OS : process.env.MIER_TEST_ARCH);');
  writeCommand('sysctl', 'console.log(process.env.MIER_TEST_ROSETTA || "0");');
  writeCommand('id', 'console.log("0");');
  writeCommand('curl', [
    'const fs = require("node:fs");',
    'const state = JSON.parse(fs.readFileSync(process.env.MIER_INSTALL_TEST_STATE));',
    'const args = process.argv.slice(2), url = args[args.length - 1], output = args[args.indexOf("--output") + 1];',
    'fs.appendFileSync(state.callsFile, JSON.stringify({command:"curl",url}) + "\\n");',
    'if (state.failDownload && url.endsWith(state.failDownload)) process.exit(22);',
    'if (url.startsWith("https://api.github.com/")) {',
    '  const page = Number(new URL(url).searchParams.get("page"));',
    '  fs.writeFileSync(output, JSON.stringify(state.pages[page - 1] || []));',
    '} else if (url.endsWith("/SHA256SUMS")) {',
    '  const names = state.pages.flat().flatMap(r => r.assets.map(a => a.name)).filter(n => n !== "SHA256SUMS" && n !== "manifest.json");',
    '  fs.writeFileSync(output, names.map(n => (state.badChecksum || state.checksum) + "  " + n).join("\\n") + "\\n");',
    '} else { fs.copyFileSync(state.payload, output); }',
  ].join('\n'));
  const env = {
    ...process.env, PATH: bin, TMPDIR: temporary, MIER_INSTALL_TEST_STATE: stateFile,
    MIER_TEST_OS: 'Linux', MIER_TEST_ARCH: 'x86_64',
    PROCESSOR_ARCHITECTURE: '', PROCESSOR_ARCHITEW6432: '',
  };
  const run = (args = [], extra = {}) => {
    save();
    return spawnSync(bash, [installer, ...args], { env: { ...env, ...extra }, encoding: 'utf8' });
  };
  const calls = () => existsSync(callsFile) ? readFileSync(callsFile, 'utf8').trim().split('\n').filter(Boolean).map(JSON.parse) : [];
  const manager = name => writeCommand(name, [
    'const fs = require("node:fs");',
    'const state = JSON.parse(fs.readFileSync(process.env.MIER_INSTALL_TEST_STATE));',
    'const args = process.argv.slice(2);',
    'fs.appendFileSync(state.callsFile, JSON.stringify({command:' + JSON.stringify(name) + ', args, payload:fs.readFileSync(args[args.length-1],"utf8")}) + "\\n");',
  ].join('\n'));
  return { root, bin, temporary, state, env, run, calls, manager, writeCommand };
}

function passed(result) { assert.equal(result.status, 0, result.stdout + result.stderr); }

test('stable is default, compares SemVer numerically, and excludes drafts, prereleases, and malformed tags', t => {
  const f = fixture(t, [
    release('v2.0.0-beta.1'), release('v1.9.9'), release('v1.10.0+build.2'),
    release('v3.0.0', { draft: true }), release('v01.99.0'), release('v65536.0.0'),
    release('v1.2.3;echo bad'), release('v2.1.0', { prerelease: true }),
  ]);
  const result = f.run(['--portable', '--dry-run']);
  passed(result);
  assert.match(result.stdout, /Release: v1\.10\.0\+build\.2/);
  assert.match(result.stdout, /linux-amd64\.AppImage/);
  assert.equal(f.calls().length, 1);
  assert.deepEqual(readdirSync(f.temporary), []);
});

test('alpha, beta and rc stay isolated and numeric identifiers use SemVer precedence', t => {
  const f = fixture(t, [
    release('v1.2.0-alpha.2'), release('v1.2.0-alpha.10'),
    release('v1.2.0-alpha.01'), release('v1.2.0-alpha-extra.99'),
    release('v2.0.0-beta.3'), release('v3.0.0-rc.2'), release('v4.0.0'),
  ]);
  for (const [channel, version] of [['alpha', '1.2.0-alpha.10'], ['beta', '2.0.0-beta.3'], ['rc', '3.0.0-rc.2']]) {
    const result = f.run(['--' + channel, '--portable', '--dry-run']);
    passed(result);
    assert.ok(result.stdout.includes('Release: v' + version));
  }
});

test('release selection follows pagination and never silently substitutes another channel', t => {
  const f = fixture(t);
  f.state.pages = [Array.from({ length: 100 }, () => release('v9.0.0-beta.1')), [release('v1.0.0-alpha.1')]];
  const alpha = f.run(['--alpha', '--portable', '--dry-run']);
  passed(alpha);
  assert.match(alpha.stdout, /Release: v1\.0\.0-alpha\.1/);
  assert.equal(f.calls().filter(c => c.url.includes('/releases?')).length, 2);
  const stable = f.run(['--portable', '--dry-run']);
  assert.notEqual(stable.status, 0);
  assert.match(stable.stderr, /No stable release is available/);
});

test('detects all six targets, Rosetta, and native ARM64 Windows under an AMD64 shell', t => {
  const f = fixture(t);
  for (const [os, arch, extra, target] of [
    ['Linux', 'x86_64', {}, 'linux-amd64.AppImage'],
    ['Linux', 'aarch64', {}, 'linux-arm64.AppImage'],
    ['Darwin', 'x86_64', {}, 'darwin-amd64.zip'],
    ['Darwin', 'arm64', {}, 'darwin-arm64.zip'],
    ['Darwin', 'x86_64', { MIER_TEST_ROSETTA: '1' }, 'darwin-arm64.zip'],
    ['MINGW64_NT-10.0', 'x86_64', { PROCESSOR_ARCHITECTURE: 'AMD64' }, 'windows-amd64-setup.exe'],
    ['MSYS_NT-10.0', 'x86_64', { PROCESSOR_ARCHITECTURE: 'AMD64', PROCESSOR_ARCHITEW6432: 'ARM64' }, 'windows-arm64-setup.exe'],
    ['CYGWIN_NT-10.0', 'aarch64', { PROCESSOR_ARCHITECTURE: 'ARM64' }, 'windows-arm64-setup.exe'],
  ]) {
    const result = f.run(os === 'Linux' ? ['--portable', '--dry-run'] : ['--dry-run'], { MIER_TEST_OS: os, MIER_TEST_ARCH: arch, ...extra });
    passed(result);
    assert.ok(result.stdout.includes(target), result.stdout);
  }
});

test('native Linux managers receive only the checksum-verified matching package', t => {
  for (const [manager, extension, arguments_] of [['apt-get', 'deb', ['install', '-y']], ['pacman', 'pkg.tar.zst', ['-U', '--noconfirm']], ['dnf', 'rpm', ['install', '-y']]]) {
    const f = fixture(t); f.manager(manager);
    passed(f.run());
    const call = f.calls().find(c => c.command === manager);
    assert.deepEqual(call.args.slice(0, -1), arguments_);
    assert.ok(call.args.at(-1).endsWith('.' + extension));
    assert.equal(call.payload, application);
    assert.deepEqual(readdirSync(f.temporary), []);
  }
});

test('portable installation works in a quoted path and launches without FUSE while preserving arguments', t => {
  const f = fixture(t), prefix = join(f.root, "my app's directory");
  passed(f.run(['--prefix', prefix]));
  const result = spawnSync(join(prefix, 'mier'), ['two words', 'plain'], { env: f.env, encoding: 'utf8' });
  passed(result);
  assert.match(result.stdout, /FUSE-free:1/);
  assert.match(result.stdout, /argument:two words\nargument:plain/);
  assert.deepEqual(readdirSync(prefix).sort(), ['mier', 'mier.AppImage']);
});

test('corrupt downloads and failed downloads preserve a working installation', t => {
  const f = fixture(t), prefix = join(f.root, 'portable');
  passed(f.run(['--prefix', prefix]));
  const previous = readFileSync(join(prefix, 'mier.AppImage'));
  f.state.badChecksum = '0'.repeat(64);
  let result = f.run(['--prefix', prefix]);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /SHA-256 verification failed/);
  assert.deepEqual(readFileSync(join(prefix, 'mier.AppImage')), previous);
  delete f.state.badChecksum; f.state.failDownload = '.AppImage';
  result = f.run(['--prefix', prefix]);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /Could not download the selected installer/);
  assert.deepEqual(readFileSync(join(prefix, 'mier.AppImage')), previous);
  assert.deepEqual(readdirSync(f.temporary), []);
});

test('incomplete publication and unexpected URLs fail before download or package installation', t => {
  for (const missing of ['manifest.json', 'SHA256SUMS', 'mier-1.2.3-linux-amd64.AppImage']) {
    const incomplete = release('v1.2.3');
    incomplete.assets = incomplete.assets.filter(a => a.name !== missing);
    const f = fixture(t, [incomplete]), result = f.run(['--portable']);
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /packaging|published|checksums/);
    assert.equal(f.calls().length, 1);
  }
  const unexpected = release('v1.2.3');
  unexpected.assets.find(a => a.name.endsWith('linux-amd64.AppImage')).browser_download_url = 'http://example.com/installer';
  const f = fixture(t, [unexpected]), result = f.run(['--portable']);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /Unexpected release download URL/);
  assert.equal(f.calls().length, 1);
});

test('invalid options and unsupported machines fail early; help needs no network', t => {
  const f = fixture(t);
  passed(f.run(['--help']));
  for (const args of [['--alpha', '--beta'], ['--unknown'], ['--prefix'], ['--prefix', '--dry-run']]) {
    assert.notEqual(f.run(args).status, 0);
  }
  for (const extra of [{ MIER_TEST_OS: 'FreeBSD' }, { MIER_TEST_ARCH: 'i686' }, { MIER_TEST_OS: 'MINGW64_NT-10.0' }]) {
    const result = f.run(['--portable'], extra);
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /Unsupported|Windows uses its per-user setup/);
  }
  assert.equal(f.calls().length, 0);
});

test('macOS replaces a complete bundle and rolls back if the final rename fails', t => {
  const f = fixture(t), source = join(f.root, 'source'), prefix = join(f.root, 'Applications');
  const executable = 'MIER.app/Contents/MacOS/mag-ik-een-review';
  mkdirSync(join(source, 'MIER.app/Contents/MacOS'), { recursive: true });
  writeFileSync(join(source, executable), '#!/bin/sh\nexit 0\n', { mode: 0o755 });
  const archive = join(f.root, 'bundle.zip');
  execFileSync(command('zip'), ['-q', '-r', archive, 'MIER.app'], { cwd: source });
  f.state.payload = archive;
  f.state.checksum = createHash('sha256').update(readFileSync(archive)).digest('hex');
  // Use actual ditto on macOS; emulate only its copy/extract operations on Linux.
  if (process.platform === 'darwin') symlinkSync(command('ditto'), join(f.bin, 'ditto'));
  else f.writeCommand('ditto', 'const {execFileSync}=require("node:child_process"); const a=process.argv.slice(2); if(a[0]==="-x")execFileSync("unzip",["-q",a[2],"-d",a[3]]);else execFileSync("cp",["-R",a[0],a[1]]);');
  mkdirSync(join(prefix, 'MIER.app'), { recursive: true });
  writeFileSync(join(prefix, 'MIER.app/obsolete'), 'old file');
  const environment = { MIER_TEST_OS: 'Darwin', MIER_TEST_ARCH: 'arm64' };
  passed(f.run(['--prefix', prefix], environment));
  assert.equal(existsSync(join(prefix, 'MIER.app/obsolete')), false);
  assert.equal(existsSync(join(prefix, executable)), true);
  writeFileSync(join(prefix, 'MIER.app/keep'), 'working bundle');
  const realMv = command('mv');
  f.writeCommand('mv', 'const a=process.argv.slice(2); if(a[0].includes(".mier-install.")&&a[0].endsWith("/MIER.app"))process.exit(1);const r=require("node:child_process").spawnSync(' + JSON.stringify(realMv) + ',a,{stdio:"inherit"});process.exit(r.status);');
  const failed = f.run(['--prefix', prefix], environment);
  assert.notEqual(failed.status, 0);
  assert.equal(readFileSync(join(prefix, 'MIER.app/keep'), 'utf8'), 'working bundle');
  assert.deepEqual(readdirSync(prefix), ['MIER.app']);
  assert.deepEqual(readdirSync(f.temporary), []);
});

test('Windows setup receives its silent flag intact with MSYS path conversion disabled', t => {
  const f = fixture(t, [release('v1.0.0-rc.1')]);
  const payload = '#!/usr/bin/env node\nconst fs=require("node:fs");const s=JSON.parse(fs.readFileSync(process.env.MIER_INSTALL_TEST_STATE));fs.appendFileSync(s.callsFile,JSON.stringify({command:"setup",args:process.argv.slice(2),conversion:process.env.MSYS2_ARG_CONV_EXCL})+"\\n");\n';
  writeFileSync(f.state.payload, payload);
  f.state.checksum = createHash('sha256').update(payload).digest('hex');
  passed(f.run(['--rc'], { MIER_TEST_OS: 'MINGW64_NT-10.0', PROCESSOR_ARCHITECTURE: 'ARM64' }));
  assert.deepEqual(f.calls().find(c => c.command === 'setup'), { command: 'setup', args: ['/S'], conversion: '*' });
  assert.deepEqual(readdirSync(f.temporary), []);
});
