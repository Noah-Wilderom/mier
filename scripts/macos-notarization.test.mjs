import { test } from 'node:test';
import assert from 'node:assert/strict';
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { basename, dirname, join } from 'node:path';
import { advanceMacOSRelease, readMacOSCheckpoint } from './macos-notarization.mjs';
import { macOSReleasePaths } from './release.mjs';

const appID = '01234567-89ab-cdef-0123-456789abcdef';
const dmgID = 'fedcba98-7654-3210-fedc-ba9876543210';
const commit = 'a'.repeat(40);
const teamID = 'ABCD123456';

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'mier-deferred-notary-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const calls = [];
  const state = { app: 'In Progress', dmg: 'In Progress', failInfo: false, failDMGSubmit: false, failGatekeeper: false, failStapleValidation: false, entries: 'MIER.app/\nMIER.app/Contents/\n' };
  const appBundle = join(root, 'Source.app');
  mkdirSync(join(appBundle, 'Contents'), { recursive: true });
  writeFileSync(join(appBundle, 'Contents/source'), 'captured application');
  const signing = { identity: 'A'.repeat(40), teamID, keychain: '/test/keychain', profile: 'release', summaryPath: join(root, 'summary.md'), directory: '' };
  function freshRunner() {
    if (signing.directory) rmSync(signing.directory, { recursive: true, force: true });
    signing.directory = mkdtempSync(join(root, 'runner-'));
    writeFileSync(join(signing.directory, 'private-keychain'), 'never upload this');
  }
  freshRunner();
  const createApp = path => {
    mkdirSync(join(path, 'Contents'), { recursive: true });
    writeFileSync(join(path, 'Contents/source'), 'captured application');
  };
  signing.tool = (command, args, options = {}) => {
    calls.push({ command, args, options });
    if (command === 'xcrun' && args[0] === 'notarytool') {
      if (args[1] === 'submit') return state.failDMGSubmit && args[2].endsWith('.dmg') ? { status: 1, diagnostic: 'temporary DMG upload failure' } :
        { status: 0, stdout: JSON.stringify({ id: args[2].endsWith('app.zip') ? appID : dmgID }) };
      if (args[1] === 'info') return state.failInfo ? { status: null, diagnostic: 'status request timed out' } :
        { status: 0, stdout: JSON.stringify({ id: args[2], status: args[2] === appID ? state.app : state.dmg }) };
      if (args[1] === 'log') return { status: 0, stdout: 'Apple rejection details' };
      assert.fail('No notarytool wait or other service commands are allowed');
    }
    if (command === 'codesign' && args[0] === '--display') return { status: 0, stdout: 'flags=0x10000(runtime)\nAuthority=Developer ID Application: Fixture\nTeamIdentifier=' + teamID + '\n', stderr: '' };
    if (command === 'zip') {
      mkdirSync(dirname(args[3]), { recursive: true });
      writeFileSync(args[3], 'fixture ZIP ' + calls.length);
    }
    if (command === 'unzip') return { status: 0, stdout: state.entries };
    if (command === 'ditto') createApp(join(args.at(-1), 'MIER.app'));
    if (command === 'hdiutil' && args[0] === 'create') writeFileSync(args.at(-1), 'fixture DMG');
    if (command === 'hdiutil' && args[0] === 'attach') createApp(join(args[args.indexOf('-mountpoint') + 1], 'MIER.app'));
    if (command === 'xcrun' && args[0] === 'stapler' && args[1] === 'staple' && args[2].endsWith('.dmg')) {
      writeFileSync(args[2], readFileSync(args[2], 'utf8') + ' ticket');
    }
    if (command === 'xcrun' && args[0] === 'stapler' && args[1] === 'validate' && args[2].endsWith('.dmg') && state.failStapleValidation) {
      throw new Error('Ticket validation temporarily unavailable');
    }
    if (command === 'spctl' && state.failGatekeeper) throw new Error('Gatekeeper temporarily unavailable');
    return { status: 0, stdout: '', stderr: '' };
  };
  const options = { ...macOSReleasePaths('arm64', '0.1.0', commit, join(root, 'dist')), signing, appBundle };
  const submissions = () => calls.filter(call => call.command === 'xcrun' && call.args[1] === 'submit');
  return { root, options, calls, state, signing, freshRunner, submissions };
}

test('pending uploads survive fresh runners and complete app and DMG without rebuilding or waiting', t => {
  const f = fixture(t);
  assert.equal(advanceMacOSRelease(f.options), false);
  const initial = readMacOSCheckpoint(f.options);
  assert.equal(initial.phase, 'app');
  assert.equal(initial.archiveHash, initial.appHash);
  assert.equal(readFileSync(f.options.archive, 'utf8'), readFileSync(join(dirname(f.options.checkpoint), 'app.zip'), 'utf8'));
  assert.ok(existsSync(f.options.dmg));
  assert.match(readFileSync(f.signing.summaryPath, 'utf8'), /downloads can publish now/);
  assert.equal(f.submissions().length, 1);
  f.freshRunner();
  assert.equal(advanceMacOSRelease(f.options, true), false);
  assert.equal(f.submissions().length, 1);
  f.state.app = 'Accepted';
  f.freshRunner();
  assert.equal(advanceMacOSRelease(f.options, true), false);
  assert.equal(readMacOSCheckpoint(f.options).phase, 'dmg');
  assert.equal(f.submissions().length, 2);
  assert.ok(!existsSync(join(dirname(f.options.checkpoint), 'app.zip')));
  f.state.dmg = 'Accepted';
  f.freshRunner();
  assert.equal(advanceMacOSRelease(f.options, true), true);
  assert.equal(readMacOSCheckpoint(f.options).phase, 'ready');
  assert.equal(f.submissions().length, 2);
  const signingCalls = f.calls.filter(call => call.command === 'codesign' && call.args[0] === '--force').length;
  f.freshRunner();
  assert.equal(advanceMacOSRelease(f.options, true), true);
  assert.equal(f.submissions().length, 2);
  assert.equal(f.calls.filter(call => call.command === 'codesign' && call.args[0] === '--force').length, signingCalls);
  assert.ok(!f.calls.some(call => call.args.includes('--wait') || call.args[1] === 'wait' || ['go', 'npm', 'wails3'].includes(call.command)));
  assert.deepEqual(readdirSync(dirname(f.options.checkpoint)), ['state.json']);
  assert.ok(!readFileSync(f.options.checkpoint, 'utf8').includes('never upload this'));
});

test('immediately accepted app and DMG can finish in the initial run', t => {
  const f = fixture(t);
  f.state.app = f.state.dmg = 'Accepted';
  assert.equal(advanceMacOSRelease(f.options), true);
  assert.equal(readMacOSCheckpoint(f.options).phase, 'ready');
  assert.equal(f.submissions().length, 2);
});

test('transient status failures retain the existing submission and never reupload', t => {
  const f = fixture(t);
  f.state.failInfo = true;
  assert.throws(() => advanceMacOSRelease(f.options), /status request timed out/);
  assert.equal(readMacOSCheckpoint(f.options).appSubmissionID, appID);
  f.state.failInfo = false;
  f.freshRunner();
  assert.equal(advanceMacOSRelease(f.options, true), false);
  assert.equal(f.submissions().length, 1);
});

test('a rejected app fails packaging before publication can run', t => {
  const f = fixture(t);
  f.state.app = 'Invalid';
  assert.throws(() => advanceMacOSRelease(f.options), /Apple rejected/);
  assert.equal(readMacOSCheckpoint(f.options).phase, 'app');
  assert.equal(f.submissions().length, 1);
});

test('pending downloads are verified by their saved checksums before publication or resume', t => {
  const f = fixture(t);
  advanceMacOSRelease(f.options);
  for (const file of [f.options.archive, f.options.dmg]) {
    const original = readFileSync(file);
    writeFileSync(file, 'modified pending download');
    f.calls.length = 0;
    assert.throws(() => readMacOSCheckpoint(f.options), /checksum/);
    assert.throws(() => advanceMacOSRelease(f.options, true), /checksum/);
    assert.equal(f.calls.length, 0);
    writeFileSync(file, original);
  }
});

test('a failed DMG submission retains the published ZIP and DMG for a fresh-runner retry', t => {
  const f = fixture(t);
  advanceMacOSRelease(f.options);
  const archive = readFileSync(f.options.archive);
  const dmg = readFileSync(f.options.dmg);
  f.state.app = 'Accepted';
  f.state.failDMGSubmit = true;
  assert.throws(() => advanceMacOSRelease(f.options, true), /temporary DMG upload failure/);
  assert.equal(readMacOSCheckpoint(f.options).phase, 'app');
  assert.deepEqual(readFileSync(f.options.archive), archive);
  assert.deepEqual(readFileSync(f.options.dmg), dmg);
  f.state.failDMGSubmit = false;
  f.freshRunner();
  assert.equal(advanceMacOSRelease(f.options, true), false);
  assert.equal(readMacOSCheckpoint(f.options).phase, 'dmg');
  assert.equal(f.submissions().filter(call => call.args[2].endsWith('app.zip')).length, 1);
});

test('an interrupted file replacement restores the exact files already submitted to Apple', t => {
  const f = fixture(t);
  f.state.app = 'Accepted';
  advanceMacOSRelease(f.options);
  const next = join(dirname(f.options.checkpoint), 'next');
  mkdirSync(next);
  const expected = [f.options.archive, f.options.dmg].map(file => {
    const bytes = readFileSync(file);
    copyFileSync(file, join(next, basename(file)));
    writeFileSync(file, 'old published file');
    return bytes;
  });
  assert.equal(readMacOSCheckpoint(f.options).phase, 'dmg');
  for (const [index, file] of [f.options.archive, f.options.dmg].entries()) {
    assert.deepEqual(readFileSync(file), expected[index]);
  }
  f.freshRunner();
  assert.equal(advanceMacOSRelease(f.options, true), false);
  assert.equal(f.submissions().length, 2);
});

test('a rejected DMG never marks the checkpoint ready', t => {
  const f = fixture(t);
  f.state.app = 'Accepted';
  f.state.dmg = 'Rejected';
  assert.throws(() => advanceMacOSRelease(f.options), /Apple rejected/);
  assert.equal(readMacOSCheckpoint(f.options).phase, 'dmg');
});

test('verification failure after stapling retains a valid checkpoint for retry', t => {
  const f = fixture(t);
  f.state.app = 'Accepted';
  assert.equal(advanceMacOSRelease(f.options), false);
  f.state.dmg = 'Accepted';
  f.state.failGatekeeper = true;
  assert.throws(() => advanceMacOSRelease(f.options, true), /Gatekeeper/);
  assert.equal(readMacOSCheckpoint(f.options).phase, 'dmg');
  f.state.failGatekeeper = false;
  f.freshRunner();
  assert.equal(advanceMacOSRelease(f.options, true), true);
  assert.equal(f.submissions().length, 2);
});

test('failed ticket validation after DMG mutation can resume without a checksum failure', t => {
  const f = fixture(t);
  f.state.app = 'Accepted';
  advanceMacOSRelease(f.options);
  f.state.dmg = 'Accepted';
  f.state.failStapleValidation = true;
  assert.throws(() => advanceMacOSRelease(f.options, true), /Ticket validation/);
  assert.equal(readMacOSCheckpoint(f.options).phase, 'dmg');
  f.state.failStapleValidation = false;
  f.freshRunner();
  assert.equal(advanceMacOSRelease(f.options, true), true);
  assert.equal(f.submissions().length, 2);
});

test('missing, mixed-version, mixed-commit, corrupt and modified checkpoints fail before native commands', t => {
  const f = fixture(t);
  assert.throws(() => advanceMacOSRelease(f.options, true), /ENOENT/);
  assert.equal(f.calls.length, 0);
  advanceMacOSRelease(f.options);
  const saved = readFileSync(f.options.checkpoint);
  const original = JSON.parse(saved);
  for (const change of [{ version: '0.2.0' }, { arch: 'amd64' }, { commit: 'b'.repeat(40) }, { phase: 'ready' }, { appSubmissionID: '--help' }, { appHash: '0'.repeat(64) }, { format: 2 }]) {
    writeFileSync(f.options.checkpoint, JSON.stringify({ ...original, ...change }));
    f.calls.length = 0;
    assert.throws(() => advanceMacOSRelease(f.options, true));
    assert.equal(f.calls.length, 0);
  }
  for (const bytes of ['not JSON', ' '.repeat(8193)]) {
    writeFileSync(f.options.checkpoint, bytes);
    assert.throws(() => advanceMacOSRelease(f.options, true));
    assert.equal(f.calls.length, 0);
  }
  writeFileSync(f.options.checkpoint, saved);
  writeFileSync(join(dirname(f.options.checkpoint), 'app.zip'), 'modified application');
  assert.throws(() => advanceMacOSRelease(f.options, true), /checksum/);
  assert.equal(f.calls.length, 0);
});

test('restarting initial packaging cannot overwrite an existing submission', t => {
  const f = fixture(t);
  advanceMacOSRelease(f.options);
  assert.throws(() => advanceMacOSRelease(f.options), /resume it/);
  assert.equal(f.submissions().length, 1);
});

test('checkpoint extraction rejects paths outside the application bundle', t => {
  const f = fixture(t);
  advanceMacOSRelease(f.options);
  for (const entries of ['../outside\n', 'MIER.app/../../outside\n', '/MIER.app/file\n', 'MIER.app\\outside\n']) {
    f.state.entries = entries;
    f.calls.length = 0;
    assert.throws(() => advanceMacOSRelease(f.options, true), /archive layout/);
    assert.ok(!f.calls.some(call => call.command === 'ditto' || call.command === 'xcrun'));
  }
});
