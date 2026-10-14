import { randomBytes } from 'node:crypto';
import { spawn, spawnSync } from 'node:child_process';
import { chmodSync, cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { basename, join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

export const macOSSecrets = [
  'MACOS_CERTIFICATE', 'MACOS_CERTIFICATE_PASSWORD', 'MACOS_SIGN_IDENTITY',
  'APPLE_TEAM_ID', 'APPLE_ID', 'APPLE_APP_PASSWORD',
];
const entitlements = resolve('build/darwin/entitlements.plist');
const signingDirectoryName = 'mier-macos-signing';

export function withoutSigningSecrets(env) {
  const result = { ...env };
  for (const key of [...macOSSecrets, 'UPDATER_PRIVATE_KEY']) delete result[key];
  return result;
}

export function validateMacOSCredentials(env = process.env) {
  const missing = macOSSecrets.filter(key => !env[key]?.trim());
  if (missing.length) throw new Error('Missing macOS release signing secrets: ' + missing.join(', ') + '.');
  const teamID = env.APPLE_TEAM_ID.trim();
  const identity = env.MACOS_SIGN_IDENTITY.trim();
  if (!/^[A-Z0-9]{10}$/.test(teamID)) throw new Error('APPLE_TEAM_ID must be the 10-character Apple Developer team ID.');
  if (!identity.startsWith('Developer ID Application: ') || !identity.endsWith(' (' + teamID + ')') || /[\r\n]/.test(identity)) {
    throw new Error('MACOS_SIGN_IDENTITY must be a Developer ID Application identity belonging to APPLE_TEAM_ID.');
  }
  if (!/^[^\s@]+@[^\s@]+$/.test(env.APPLE_ID.trim())) throw new Error('APPLE_ID must be your Apple Account email address.');
  const encoded = env.MACOS_CERTIFICATE.replace(/\s/g, '');
  const certificate = Buffer.from(encoded, 'base64');
  if (!encoded || encoded.length > 65536 || certificate.length < 32 || certificate[0] !== 0x30 || certificate.toString('base64') !== encoded) {
    throw new Error('MACOS_CERTIFICATE must contain a Base64-encoded PKCS#12 (.p12) signing identity.');
  }
  return { certificate, certificatePassword: env.MACOS_CERTIFICATE_PASSWORD, identity, teamID, appleID: env.APPLE_ID.trim(), password: env.APPLE_APP_PASSWORD };
}

export function createMacOSTool(env = process.env) {
  const secrets = ['MACOS_CERTIFICATE', 'MACOS_CERTIFICATE_PASSWORD', 'APPLE_APP_PASSWORD', 'UPDATER_PRIVATE_KEY'].map(key => env[key]).filter(Boolean);
  const redact = value => secrets.reduce((text, secret) => text.replaceAll(secret, '[redacted]'), String(value || ''));
  return (command, args, { allowFailure = false, timeout = 120_000, cwd, env: extraEnv = {} } = {}) => {
    const result = spawnSync(command, args, {
      shell: false, encoding: 'utf8', maxBuffer: 4 * 1024 * 1024, timeout,
      env: withoutSigningSecrets({ ...env, ...extraEnv }), cwd,
    });
    const output = { status: result.status, stdout: String(result.stdout || ''), stderr: String(result.stderr || ''),
      diagnostic: redact(result.error?.message || [result.stdout, result.stderr].filter(Boolean).join('\n')) };
    if (!allowFailure && (result.error || result.status !== 0)) {
      throw new Error(command + ' failed: ' + (output.diagnostic || 'exit ' + result.status));
    }
    return output;
  };
}

export function parseKeychains(output) {
  return output.split('\n').map(line => line.trim()).filter(Boolean).map(line => {
    const path = line.replace(/^"(.*)"$/, '$1');
    if (!path.startsWith('/') || /[\r\n\0]/.test(path)) throw new Error('Invalid keychain search list.');
    return path;
  });
}

export function selectSigningIdentity(output, identity) {
  const matches = [...output.matchAll(/^\s*\d+\)\s+([A-Fa-f0-9]{40})\s+"([^"]+)"/gm)].filter(match => match[2] === identity);
  if (matches.length !== 1) throw new Error('The imported .p12 must contain exactly one valid matching Developer ID Application certificate and private key.');
  return matches[0][1];
}

export function cleanupMacOSSigning({ temporaryRoot = process.env.RUNNER_TEMP || tmpdir(), tool = createMacOSTool() } = {}) {
  const directory = join(resolve(temporaryRoot), signingDirectoryName);
  if (!existsSync(directory)) return;
  const statePath = join(directory, 'state.json');
  const errors = [];
  if (existsSync(statePath)) {
    const state = JSON.parse(readFileSync(statePath, 'utf8'));
    if (state.keychain !== join(directory, 'release.keychain-db') || !Array.isArray(state.originalKeychains) ||
        state.originalKeychains.some(path => typeof path !== 'string' || !path.startsWith('/') || /[\r\n\0]/.test(path))) {
      throw new Error('Invalid macOS signing cleanup state.');
    }
    const commands = [['list-keychains', '-d', 'user', '-s', ...state.originalKeychains]];
    if (existsSync(state.keychain)) commands.push(['delete-keychain', state.keychain]);
    for (const args of commands) {
      try { tool('security', args); } catch (error) { errors.push(error); }
    }
  }
  rmSync(directory, { recursive: true, force: true });
  if (errors.length) throw new AggregateError(errors, 'Could not completely clean up the macOS signing keychain.');
}

export function withMacOSSigning(operation, { env = process.env, tool = createMacOSTool(env), temporaryRoot = env.RUNNER_TEMP || tmpdir() } = {}) {
  const credentials = validateMacOSCredentials(env);
  const directory = join(resolve(temporaryRoot), signingDirectoryName);
  mkdirSync(directory, { mode: 0o700 });
  chmodSync(directory, 0o700);
  let failure;
  try {
    const keychain = join(directory, 'release.keychain-db');
    const originalKeychains = parseKeychains(tool('security', ['list-keychains', '-d', 'user']).stdout);
    writeFileSync(join(directory, 'state.json'), JSON.stringify({ keychain, originalKeychains }), { mode: 0o600 });
    const keychainPassword = randomBytes(32).toString('hex');
    const certificatePath = join(directory, 'certificate.p12');
    writeFileSync(certificatePath, credentials.certificate, { mode: 0o600 });
    tool('security', ['create-keychain', '-p', keychainPassword, keychain]);
    tool('security', ['set-keychain-settings', '-lut', '21600', keychain]);
    tool('security', ['unlock-keychain', '-p', keychainPassword, keychain]);
    tool('security', ['import', certificatePath, '-k', keychain, '-P', credentials.certificatePassword, '-T', '/usr/bin/codesign', '-T', '/usr/bin/security']);
    rmSync(certificatePath);
    tool('security', ['set-key-partition-list', '-S', 'apple-tool:,apple:,codesign:', '-s', '-k', keychainPassword, keychain]);
    tool('security', ['list-keychains', '-d', 'user', '-s', keychain, ...originalKeychains]);
    const identity = selectSigningIdentity(tool('security', ['find-identity', '-v', '-p', 'codesigning', keychain]).stdout, credentials.identity);
    const profile = 'mier-release-notarization';
    tool('xcrun', ['notarytool', 'store-credentials', profile, '--apple-id', credentials.appleID,
      '--team-id', credentials.teamID, '--password', credentials.password, '--keychain', keychain]);
    return operation({ identity, teamID: credentials.teamID, keychain, profile, directory, tool });
  } catch (error) {
    failure = error;
    throw error;
  } finally {
    try { cleanupMacOSSigning({ temporaryRoot, tool }); }
    catch (error) {
      if (!failure) throw error;
      console.error(error.message);
    }
  }
}

export function notarizeAndStaple(artifact, stapleTarget, signing) {
  const { tool, keychain, profile } = signing;
  const authentication = ['--keychain-profile', profile, '--keychain', keychain];
  const result = tool('xcrun', ['notarytool', 'submit', artifact, ...authentication,
    '--wait', '--timeout', '20m', '--output-format', 'json'], { allowFailure: true, timeout: 21 * 60_000 });
  let response;
  try { response = JSON.parse(result.stdout); } catch { /* Report the safe tool output below. */ }
  const validID = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i.test(response?.id || '');
  if (result.status !== 0 || response?.status !== 'Accepted' || !validID) {
    if (validID) {
      const log = tool('xcrun', ['notarytool', 'log', response.id, ...authentication], { allowFailure: true });
      console.error(log.diagnostic || log.stdout || log.stderr);
    }
    throw new Error('Apple notarization did not accept ' + basename(artifact) + ': ' +
      (response?.status || result.diagnostic || 'no valid response') + '.');
  }
  console.log('Apple accepted ' + basename(artifact) + ' (' + response.id + ').');
  tool('xcrun', ['stapler', 'staple', stapleTarget]);
  tool('xcrun', ['stapler', 'validate', stapleTarget]);
}

export function signMacOSApp(app, signing) {
  const { tool, identity, keychain } = signing;
  tool('plutil', ['-lint', entitlements]);
  tool('codesign', ['--force', '--sign', identity, ...(keychain ? ['--keychain', keychain] : []),
    identity === '-' ? '--timestamp=none' : '--timestamp', '--options', 'runtime', '--entitlements', entitlements, app]);
  verifyMacOSApp(app, signing);
}

export function verifyMacOSApp(app, signing, notarized = false) {
  const { tool, teamID } = signing;
  tool('codesign', ['--verify', '--deep', '--strict', '--verbose=2', app]);
  const details = tool('codesign', ['--display', '--verbose=4', app]);
  const text = details.stdout + details.stderr;
  if (!/flags=.*\bruntime\b/.test(text)) throw new Error('The macOS app is missing the hardened runtime.');
  if (teamID && (!text.includes('TeamIdentifier=' + teamID + '\n') || !text.includes('Authority=Developer ID Application: '))) {
    throw new Error('The macOS app signature does not match the expected Developer ID team.');
  }
  if (notarized) {
    tool('xcrun', ['stapler', 'validate', app]);
    tool('spctl', ['--assess', '--type', 'execute', '--verbose=2', app]);
  }
}

export function packageMacOSRelease({ appBundle, archive, dmg, signing }) {
  const { tool } = signing;
  const stage = join(signing.directory, 'stage');
  const app = join(stage, 'MIER.app');
  const notarized = Boolean(signing.profile);
  if (signing.identity !== '-' && (!notarized || !signing.teamID || !signing.keychain)) {
    throw new Error('Developer ID release packaging requires a signing keychain, team ID, and notarization profile.');
  }
  rmSync(archive, { force: true });
  rmSync(dmg, { force: true });
  mkdirSync(stage);
  cpSync(appBundle, app, { recursive: true, dereference: false });
  signMacOSApp(app, signing);
  if (notarized) {
    const submission = join(signing.directory, 'app-notarization.zip');
    tool('ditto', ['-c', '-k', '--keepParent', app, submission]);
    notarizeAndStaple(submission, app, signing);
    verifyMacOSApp(app, signing, true);
  }
  // Stapling adds a regular file to the bundle. This ZIP preserves modes and
  // symlinks without __MACOSX sidecars, as required by Wails' app extractor.
  tool('zip', ['-q', '-r', '-y', resolve(archive), 'MIER.app'], { cwd: stage, env: { COPYFILE_DISABLE: '1' } });
  const extracted = join(signing.directory, 'extracted');
  mkdirSync(extracted);
  tool('ditto', ['-x', '-k', resolve(archive), extracted]);
  const entries = readdirSync(extracted);
  if (entries.length !== 1 || entries[0] !== 'MIER.app') throw new Error('The macOS updater ZIP must contain exactly one MIER.app bundle.');
  verifyMacOSApp(join(extracted, 'MIER.app'), signing, notarized);
  symlinkSync('/Applications', join(stage, 'Applications'));
  tool('hdiutil', ['create', '-volname', 'M.I.E.R.', '-srcfolder', stage, '-format', 'UDZO', '-ov', resolve(dmg)]);
  tool('codesign', ['--force', '--sign', signing.identity, ...(signing.keychain ? ['--keychain', signing.keychain] : []),
    signing.identity === '-' ? '--timestamp=none' : '--timestamp', resolve(dmg)]);
  tool('codesign', ['--verify', '--strict', '--verbose=2', resolve(dmg)]);
  if (notarized) {
    notarizeAndStaple(resolve(dmg), resolve(dmg), signing);
    tool('spctl', ['--assess', '--type', 'open', '--context', 'context:primary-signature', '--verbose=2', resolve(dmg)]);
  }
  tool('hdiutil', ['verify', resolve(dmg)]);
  const mount = join(signing.directory, 'mounted');
  mkdirSync(mount);
  tool('hdiutil', ['attach', '-readonly', '-nobrowse', '-mountpoint', mount, resolve(dmg)]);
  try { verifyMacOSApp(join(mount, 'MIER.app'), signing, notarized); }
  finally { tool('hdiutil', ['detach', mount]); }
}

async function smokeTest(appBundle) {
  const directory = mkdtempSync(join(tmpdir(), 'mier-macos-smoke-'));
  try {
    const signing = { identity: '-', directory, tool: createMacOSTool() };
    packageMacOSRelease({ appBundle, archive: join(directory, 'update.zip'), dmg: join(directory, 'installer.dmg'), signing });
    // Exercise the real Wails executable with hardened runtime on each Mac CPU.
    const executable = join(directory, 'extracted/MIER.app/Contents/MacOS/mag-ik-een-review');
    const child = spawn(executable, [], { env: withoutSigningSecrets(process.env), stdio: ['ignore', 'pipe', 'pipe'] });
    let output = '';
    const capture = chunk => { output = (output + chunk).slice(-65536); };
    child.stdout.on('data', capture);
    child.stderr.on('data', capture);
    const closed = new Promise((resolveClose, rejectClose) => {
      child.once('error', rejectClose);
      child.once('close', (code, signal) => resolveClose({ code, signal }));
    });
    let timer;
    try {
      const startup = await Promise.race([closed, new Promise(resolveDelay => { timer = setTimeout(resolveDelay, 5000); })]);
      if (startup) throw new Error('The hardened Wails app exited during startup: ' + output);
      child.kill('SIGTERM');
      clearTimeout(timer);
      await Promise.race([closed, new Promise((_, rejectDelay) => { timer = setTimeout(() => rejectDelay(new Error('The native app did not stop.')), 10_000); })]);
    } finally {
      clearTimeout(timer);
      if (child.exitCode === null && child.signalCode === null) child.kill('SIGKILL');
    }
    console.log('Hardened app, updater ZIP, DMG, and native startup checks passed.');
  } finally { rmSync(directory, { recursive: true, force: true }); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    if (process.platform !== 'darwin') throw new Error('macOS signing tools require a macOS host.');
    if (process.argv[2] === 'cleanup') cleanupMacOSSigning();
    else if (process.argv[2] === 'smoke' && process.argv[3]) await smokeTest(resolve(process.argv[3]));
    else throw new Error('Usage: macos-sign.mjs cleanup | smoke APP_BUNDLE');
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
