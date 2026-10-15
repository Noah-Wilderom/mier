import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  checkNotarizationStatus, cleanupMacOSSigning, createMacOSTool, macOSSecrets, notarizeAndStaple, packageMacOSRelease,
  parseKeychains, selectSigningIdentity, validateMacOSCredentials, verifyMacOSApp, withMacOSSigning, withoutSigningSecrets,
} from './macos-sign.mjs';

const teamID = 'ABCD123456';
const identity = 'Developer ID Application: MIER Test (' + teamID + ')';
const fingerprint = 'A'.repeat(40);
const submissionID = '01234567-89ab-cdef-0123-456789abcdef';

function credentials() {
  const bytes = Buffer.alloc(64, 1);
  bytes[0] = 0x30;
  return {
    MACOS_CERTIFICATE: bytes.toString('base64'), MACOS_CERTIFICATE_PASSWORD: 'test-p12-password',
    MACOS_SIGN_IDENTITY: identity, APPLE_TEAM_ID: teamID, APPLE_ID: 'test@example.com',
    APPLE_APP_PASSWORD: 'test-app-password',
  };
}

function temporaryDirectory(t) {
  const root = mkdtempSync(join(tmpdir(), 'mier-signing-test-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  return root;
}

function fakeKeychainTool(root, calls, failAt) {
  return (command, args) => {
    calls.push({ command, args });
    if (args[0] === failAt) throw new Error('simulated ' + failAt + ' failure');
    if (command === 'security' && args[0] === 'create-keychain') {
      const certificate = join(root, 'mier-macos-signing/certificate.p12');
      if (process.platform !== 'win32') {
        assert.equal(statSync(certificate).mode & 0o777, 0o600);
        assert.equal(statSync(join(root, 'mier-macos-signing')).mode & 0o777, 0o700);
      }
      writeFileSync(args.at(-1), 'temporary keychain');
    }
    if (args[0] === 'find-identity') return { status: 0, stdout: '  1) ' + fingerprint + ' "' + identity + '"\n  1 valid identities found\n', stderr: '' };
    if (args[0] === 'list-keychains' && !args.includes('-s')) return { status: 0, stdout: '    "/Users/runner/Library/Keychains/login.keychain-db"\n    "/Library/Keychains/System.keychain"\n', stderr: '' };
    return { status: 0, stdout: '', stderr: '' };
  };
}

test('release signing requires every credential and a matching Developer ID Application team', () => {
  const env = credentials();
  assert.equal(validateMacOSCredentials(env).teamID, teamID);
  assert.equal(validateMacOSCredentials({ ...env, MACOS_CERTIFICATE: '\n' + env.MACOS_CERTIFICATE + '\n' }).certificate.length, 64);
  for (const key of macOSSecrets) {
    const missing = { ...env };
    delete missing[key];
    assert.throws(() => validateMacOSCredentials(missing), new RegExp(key));
  }
  for (const invalid of [
    { APPLE_TEAM_ID: 'short' }, { MACOS_SIGN_IDENTITY: 'Apple Development: MIER Test (' + teamID + ')' },
    { MACOS_SIGN_IDENTITY: 'Developer ID Installer: MIER Test (' + teamID + ')' },
    { MACOS_SIGN_IDENTITY: identity.replace(teamID, 'ZZZZ999999') }, { APPLE_ID: 'not an email' },
    { MACOS_CERTIFICATE: 'not base64!' }, { MACOS_CERTIFICATE: Buffer.from('not a p12').toString('base64') },
  ]) assert.throws(() => validateMacOSCredentials({ ...env, ...invalid }));
});

test('certificate lookup rejects missing, expired, or ambiguous identities', () => {
  const valid = '  1) ' + fingerprint + ' "' + identity + '"\n';
  assert.equal(selectSigningIdentity(valid, identity), fingerprint);
  for (const output of ['', '  0 valid identities found\n', valid + valid, valid.replace(identity, 'Apple Development: Test')]) {
    assert.throws(() => selectSigningIdentity(output, identity));
  }
});

test('keychain paths with spaces survive search-list parsing', () => {
  assert.deepEqual(parseKeychains(' "/Users/A User/login.keychain-db"\n "/Library/Keychains/System.keychain"\n'), [
    '/Users/A User/login.keychain-db', '/Library/Keychains/System.keychain',
  ]);
  assert.throws(() => parseKeychains('--delete-keychain'));
});

test('a signing session removes private files and restores the existing search list', t => {
  const root = temporaryDirectory(t);
  const calls = [];
  const result = withMacOSSigning(signing => {
    assert.equal(signing.identity, fingerprint);
    assert.equal(signing.teamID, teamID);
    assert.ok(!existsSync(join(signing.directory, 'certificate.p12')));
    assert.ok(existsSync(signing.keychain));
    return 'signed';
  }, { env: credentials(), temporaryRoot: root, tool: fakeKeychainTool(root, calls) });
  assert.equal(result, 'signed');
  assert.ok(!existsSync(join(root, 'mier-macos-signing')));
  const restore = calls.at(-2);
  assert.deepEqual(restore.args, ['list-keychains', '-d', 'user', '-s', '/Users/runner/Library/Keychains/login.keychain-db', '/Library/Keychains/System.keychain']);
  assert.equal(calls.at(-1).args[0], 'delete-keychain');
  const imported = calls.find(call => call.args[0] === 'import').args;
  assert.ok(!imported.includes('-A'));
  assert.ok(imported.includes('/usr/bin/codesign'));
});

test('native certificate import and keychain cleanup work without Apple account credentials', { skip: process.platform !== 'darwin' }, t => {
  const root = temporaryDirectory(t);
  const tool = createMacOSTool();
  const certificatePath = join(root, 'certificate.pem');
  const keyPath = join(root, 'key.pem');
  const p12 = join(root, 'certificate.p12');
  const env = credentials();
  tool('openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
    '-subj', '/CN=' + identity + '/OU=' + teamID, '-keyout', keyPath, '-out', certificatePath]);
  tool('openssl', ['pkcs12', '-export', '-inkey', keyPath, '-in', certificatePath,
    '-keypbe', 'PBE-SHA1-3DES', '-certpbe', 'PBE-SHA1-3DES', '-macalg', 'sha1',
    '-out', p12, '-passout', 'pass:' + env.MACOS_CERTIFICATE_PASSWORD]);
  env.MACOS_CERTIFICATE = readFileSync(p12).toString('base64');
  const original = parseKeychains(tool('security', ['list-keychains', '-d', 'user']).stdout);
  const completed = [];
  const nativeTool = (command, args, options) => {
    // The fixture is self-signed: do not alter trust or contact Apple's service.
    if (args[0] === 'find-identity') return { stdout: '  1) ' + fingerprint + ' "' + identity + '"\n' };
    if (command === 'xcrun') return { status: 0, stdout: '', stderr: '' };
    const result = tool(command, args, options);
    completed.push(args[0]);
    return result;
  };
  withMacOSSigning(signing => {
    assert.ok(existsSync(signing.keychain));
  }, { env, temporaryRoot: root, tool: nativeTool });
  assert.ok(completed.includes('import'));
  assert.ok(completed.includes('set-key-partition-list'));
  assert.ok(completed.includes('delete-keychain'));
  assert.deepEqual(parseKeychains(tool('security', ['list-keychains', '-d', 'user']).stdout), original);
  assert.ok(!existsSync(join(root, 'mier-macos-signing')));
});

test('setup and signing failures clean up the temporary keychain', t => {
  for (const failAt of ['import', 'set-key-partition-list', 'find-identity', 'notarytool', 'operation']) {
    const root = temporaryDirectory(t);
    const calls = [];
    assert.throws(() => withMacOSSigning(() => { throw new Error('operation failed'); }, {
      env: credentials(), temporaryRoot: root, tool: fakeKeychainTool(root, calls, failAt === 'operation' ? undefined : failAt),
    }));
    assert.ok(!existsSync(join(root, 'mier-macos-signing')));
    assert.ok(calls.some(call => call.args[0] === 'delete-keychain'));
  }
});

test('the cancellation cleanup restores state without needing any credential secret', t => {
  const root = temporaryDirectory(t);
  const directory = join(root, 'mier-macos-signing');
  mkdirSync(directory);
  const keychain = join(directory, 'release.keychain-db');
  writeFileSync(keychain, 'keychain');
  writeFileSync(join(directory, 'state.json'), JSON.stringify({ keychain, originalKeychains: ['/Users/runner/login.keychain-db'] }));
  const calls = [];
  cleanupMacOSSigning({ temporaryRoot: root, tool: (command, args) => { calls.push({ command, args }); return {}; } });
  assert.equal(calls.at(-1).args[0], 'delete-keychain');
  assert.ok(!existsSync(directory));
  assert.doesNotThrow(() => cleanupMacOSSigning({ temporaryRoot: root, tool: () => { throw new Error('unexpected tool call'); } }));
});

test('malformed cleanup state cannot point deletion at another keychain', t => {
  const root = temporaryDirectory(t);
  const directory = join(root, 'mier-macos-signing');
  mkdirSync(directory);
  const other = join(root, 'other.keychain-db');
  writeFileSync(other, 'keep this file');
  writeFileSync(join(directory, 'state.json'), JSON.stringify({ keychain: other, originalKeychains: [] }));
  assert.throws(() => cleanupMacOSSigning({ temporaryRoot: root, tool: () => assert.fail('should not execute security') }), /Invalid/);
  assert.equal(readFileSync(other, 'utf8'), 'keep this file');
});

test('only an accepted notarization submission may staple the application', () => {
  const calls = [];
  const signing = {
    keychain: '/tmp/signing.keychain-db', profile: 'release',
    tool: (command, args, options) => {
      calls.push({ command, args, options });
      return { status: 0, stdout: JSON.stringify({ status: args[1] === 'submit' ? 'In Progress' : 'Accepted', id: submissionID }), stderr: '' };
    },
  };
  notarizeAndStaple('/tmp/app.zip', '/tmp/MIER.app', signing);
  assert.deepEqual(calls.slice(2).map(call => call.args), [
    ['stapler', 'staple', '/tmp/MIER.app'], ['stapler', 'validate', '/tmp/MIER.app'],
  ]);
  assert.equal(calls[0].args[1], 'submit');
  assert.ok(!calls[0].args.includes('--wait'));
  assert.deepEqual(calls[1].args.slice(0, 3), ['notarytool', 'wait', submissionID]);
  assert.ok(calls[1].args.includes('15m'));
  assert.ok(calls[1].options.timeout > 15 * 60_000);
});

test('an Apple queue timeout keeps waiting on the original submission without uploading again', () => {
  const calls = [];
  let waits = 0;
  const signing = { keychain: '/tmp/signing.keychain-db', profile: 'release', tool: (command, args) => {
    calls.push({ command, args });
    if (args[1] === 'submit' || args[1] === 'info') {
      return { status: 0, stdout: JSON.stringify({ id: submissionID, status: 'In Progress' }) };
    }
    if (args[1] === 'wait' && ++waits === 1) {
      return { status: 69, stdout: JSON.stringify({ id: submissionID, message: 'Timeout of 900 second(s) was reached before processing completed.' }) };
    }
    return { status: 0, stdout: JSON.stringify({ id: submissionID, status: 'Accepted' }) };
  } };
  assert.doesNotThrow(() => notarizeAndStaple('/tmp/app.zip', '/tmp/MIER.app', signing));
  assert.equal(calls.filter(call => call.args[1] === 'submit').length, 1);
  assert.equal(waits, 2);
  assert.ok(calls.filter(call => ['wait', 'info'].includes(call.args[1])).every(call => call.args[2] === submissionID));
  assert.ok(!calls.some(call => call.args[1] === 'log'));
  assert.deepEqual(calls.slice(-2).map(call => call.args), [
    ['stapler', 'staple', '/tmp/MIER.app'], ['stapler', 'validate', '/tmp/MIER.app'],
  ]);
});

test('Apple acceptance between a wait timeout and status check still permits stapling', () => {
  const calls = [];
  notarizeAndStaple('/tmp/app.zip', '/tmp/MIER.app', { keychain: '/tmp/keychain', profile: 'release', tool: (command, args) => {
    calls.push(args);
    if (args[1] === 'wait') return { status: 69, stdout: JSON.stringify({ id: submissionID, message: 'Timeout reached.' }) };
    return { status: 0, stdout: JSON.stringify({ id: submissionID, status: args[1] === 'submit' ? 'In Progress' : 'Accepted' }) };
  } });
  assert.equal(calls.filter(args => args[1] === 'wait').length, 1);
  assert.equal(calls.filter(args => args[1] === 'info').length, 1);
  assert.ok(calls.some(args => args[0] === 'stapler' && args[1] === 'staple'));
});

test('a bounded pending submission records its ID and never fetches a rejection log or staples', t => {
  const calls = [];
  const summaryPath = join(temporaryDirectory(t), 'summary.md');
  assert.throws(() => notarizeAndStaple('/tmp/app.zip', '/tmp/MIER.app', {
    keychain: '/tmp/keychain', profile: 'release', summaryPath, tool: (command, args) => {
      calls.push(args);
      if (args[1] === 'wait') return { status: 69, stdout: JSON.stringify({ id: submissionID, message: 'Timeout reached.' }) };
      return { status: 0, stdout: JSON.stringify({ id: submissionID, status: 'In Progress' }) };
    },
  }), error => {
    assert.match(error.message, /still In Progress/);
    assert.match(error.message, /not been rejected/);
    assert.ok(error.message.includes(submissionID));
    return true;
  });
  assert.equal(calls.filter(args => args[1] === 'submit').length, 1);
  assert.equal(calls.filter(args => args[1] === 'wait').length, 4);
  assert.equal(calls.filter(args => args[1] === 'info').length, 4);
  assert.ok(!calls.some(args => args[1] === 'log' || args[0] === 'stapler'));
  assert.match(readFileSync(summaryPath, 'utf8'), new RegExp(submissionID + ': In Progress'));
});

test('only a terminal rejection fetches the Apple rejection log', () => {
  for (const status of ['Invalid', 'Rejected']) {
    const calls = [];
    assert.throws(() => notarizeAndStaple('/tmp/app.zip', '/tmp/MIER.app', {
      keychain: '/tmp/keychain', profile: 'release', tool: (command, args) => {
        calls.push(args);
        return { status: 0, stdout: JSON.stringify({ id: submissionID, status: args[1] === 'submit' ? 'In Progress' : status }) };
      },
    }), /Apple rejected/);
    assert.equal(calls.filter(args => args[1] === 'log').length, 1);
    assert.equal(calls.filter(args => args[1] === 'wait').length, 1);
    assert.ok(!calls.some(args => args[0] === 'stapler'));
  }
});

test('status checks are read-only and work without a signing certificate', t => {
  const calls = [];
  const summary = join(temporaryDirectory(t), 'summary.md');
  const env = { APPLE_ID: 'test@example.com', APPLE_TEAM_ID: teamID, APPLE_APP_PASSWORD: 'test-app-password', GITHUB_STEP_SUMMARY: summary };
  const secondID = 'fedcba98-7654-3210-fedc-ba9876543210';
  const results = checkNotarizationStatus([submissionID, secondID], { env, tool: (command, args, options) => {
    calls.push({ command, args, options });
    return { status: 0, stdout: JSON.stringify({ id: args[2], status: args[2] === submissionID ? 'Accepted' : 'In Progress' }) };
  } });
  assert.deepEqual(results.map(result => result.status), ['Accepted', 'In Progress']);
  assert.ok(calls.every(call => call.command === 'xcrun' && call.args[1] === 'info'));
  assert.ok(calls.every(call => call.args.includes(env.APPLE_APP_PASSWORD) && call.options.timeout === 30_000));
  const contents = readFileSync(summary, 'utf8');
  assert.ok(contents.includes(submissionID) && contents.includes(secondID));
  assert.ok(!contents.includes(env.APPLE_APP_PASSWORD));
});

test('status checks reject invalid IDs or missing credentials before contacting Apple', () => {
  const tool = () => assert.fail('must not contact Apple');
  for (const ids of [[], ['--help'], ['not-a-uuid'], [submissionID + '\n'], Array(11).fill(submissionID)]) {
    assert.throws(() => checkNotarizationStatus(ids, { env: credentials(), tool }), /UUIDs/);
  }
  for (const key of ['APPLE_ID', 'APPLE_TEAM_ID', 'APPLE_APP_PASSWORD']) {
    const env = credentials();
    delete env[key];
    assert.throws(() => checkNotarizationStatus([submissionID], { env, tool }), new RegExp(key));
  }
});

test('status lookup rejects another submission, unknown statuses, and command failures', () => {
  for (const response of [
    { status: 0, stdout: JSON.stringify({ id: 'fedcba98-7654-3210-fedc-ba9876543210', status: 'Accepted' }) },
    { status: 0, stdout: JSON.stringify({ id: submissionID, status: 'Unknown' }) },
    { status: 1, stdout: JSON.stringify({ id: submissionID, status: 'Accepted' }) },
    { status: null, stdout: '' }, { status: 0, stdout: 'not JSON' },
  ]) assert.throws(() => checkNotarizationStatus([submissionID], { env: credentials(), tool: () => response }), /Could not check/);
});

test('rejected, incomplete, timed-out, and malformed notarization responses cannot publish', () => {
  for (const result of [
    { status: 0, stdout: JSON.stringify({ status: 'Invalid', id: submissionID }) },
    { status: 0, stdout: JSON.stringify({ status: 'In Progress', id: submissionID }) },
    { status: 1, stdout: JSON.stringify({ status: 'Accepted', id: submissionID }) },
    { status: 0, stdout: JSON.stringify({ status: 'Accepted' }) },
    { status: null, stdout: '' }, { status: 0, stdout: 'not JSON' },
  ]) {
    const calls = [];
    const signing = { keychain: '/tmp/keychain', profile: 'release', tool: (command, args) => {
      calls.push(args);
      if (args[1] === 'log') return { stdout: 'simulated notary rejection details' };
      return result;
    } };
    assert.throws(() => notarizeAndStaple('/tmp/app.zip', '/tmp/MIER.app', signing), /notarization|Apple rejected/);
    assert.ok(!calls.some(args => args[0] === 'stapler'));
  }
});

test('verification rejects missing runtime protection and an unexpected signing team', () => {
  const good = 'flags=0x10000(runtime)\nAuthority=' + identity + '\nTeamIdentifier=' + teamID + '\n';
  for (const details of [good, good.replace('runtime', 'adhoc'), good.replaceAll(teamID, 'OTHERTEAM1'), good.replace('Developer ID Application', 'Apple Development')]) {
    const signing = { teamID, tool: (_command, args) => ({ stdout: args[0] === '--display' ? details : '', stderr: '' }) };
    if (details === good) assert.doesNotThrow(() => verifyMacOSApp('/tmp/MIER.app', signing));
    else assert.throws(() => verifyMacOSApp('/tmp/MIER.app', signing));
  }
});

test('release packaging cannot silently skip notarization', t => {
  const root = temporaryDirectory(t);
  assert.throws(() => packageMacOSRelease({
    appBundle: '/tmp/MIER.app', archive: join(root, 'update.zip'), dmg: join(root, 'installer.dmg'),
    signing: { identity, directory: root, tool: () => assert.fail('no native commands should run') },
  }), /requires/);
});

test('subprocesses receive no signing secrets and failures redact passwords', t => {
  const root = temporaryDirectory(t);
  const env = { ...process.env, ...credentials(), UPDATER_PRIVATE_KEY: 'test-updater-key' };
  const script = join(root, 'tool.mjs');
  writeFileSync(script, 'console.log(process.env.APPLE_APP_PASSWORD || "no-password"); console.error(process.argv[2] || ""); process.exit(Number(process.argv[3] || 0));');
  const tool = createMacOSTool(env);
  assert.equal(tool(process.execPath, [script]).stdout.trim(), 'no-password');
  assert.throws(() => tool(process.execPath, [script, env.APPLE_APP_PASSWORD, '1']), error => {
    assert.ok(!error.message.includes(env.APPLE_APP_PASSWORD));
    assert.match(error.message, /\[redacted\]/);
    return true;
  });
  assert.ok(macOSSecrets.every(key => !(key in withoutSigningSecrets(env))));
  assert.ok(!('UPDATER_PRIVATE_KEY' in withoutSigningSecrets(env)));
  assert.equal(tool(process.execPath, ['-e', 'console.log(process.cwd())'], { cwd: root }).stdout.trim(), realpathSync(root));
});

test('production entitlements allow networking without development privileges or an app sandbox', () => {
  const plist = readFileSync('build/darwin/entitlements.plist', 'utf8');
  assert.match(plist, /com\.apple\.security\.network\.client/);
  assert.match(plist, /com\.apple\.security\.network\.server/);
  assert.doesNotMatch(plist, /allow-jit|allow-unsigned-executable-memory|disable-library-validation|get-task-allow|app-sandbox/);
  if (process.platform === 'darwin') assert.equal(spawnSync('plutil', ['-lint', 'build/darwin/entitlements.plist']).status, 0);
});
