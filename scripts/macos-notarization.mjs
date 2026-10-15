import { createHash } from 'node:crypto';
import { appendFileSync, copyFileSync, existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, renameSync, rmSync, writeFileSync } from 'node:fs';
import { basename, dirname, join } from 'node:path';
import { completeNotarization, createMacOSDMG, stageMacOSApp, submitNotarization, verifyMacOSApp, verifyMacOSDMG, writeMacOSArchive } from './macos-sign.mjs';

const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
const digest = /^[a-f0-9]{64}$/;

function hash(file) {
  const info = lstatSync(file);
  if (!info.isFile() || info.size === 0) throw new Error('Missing or invalid notarization artifact.');
  return createHash('sha256').update(readFileSync(file)).digest('hex');
}

function matches(file, expected) {
  try { return hash(file) === expected; }
  catch { return false; }
}

export function readMacOSCheckpoint(options) {
  const { checkpoint, version, arch, commit, archive, dmg } = options;
  if (lstatSync(checkpoint).size > 8192) throw new Error('Invalid notarization checkpoint size.');
  const state = JSON.parse(readFileSync(checkpoint, 'utf8'));
  if (state.format !== 1 || state.version !== version || state.arch !== arch || state.commit !== commit ||
      !['app', 'dmg', 'ready'].includes(state.phase) || !uuid.test(state.appSubmissionID || '')) {
    throw new Error('Notarization checkpoint does not match the captured release.');
  }
  const files = [[archive, state.archiveHash], [dmg, state.dmgHash]];
  if (state.phase === 'app') {
    if (state.appHash !== state.archiveHash) throw new Error('Pending updater ZIP does not match the submitted app.');
    files.push([join(dirname(checkpoint), 'app.zip'), state.appHash]);
  }
  if (state.phase !== 'app' && !uuid.test(state.dmgSubmissionID || '')) throw new Error('Missing DMG notarization submission.');
  for (const [file, expected] of files) {
    if (!digest.test(expected || '')) throw new Error('Invalid notarization artifact checksum.');
    if (!matches(file, expected)) {
      // The new submission is saved before replacing published files. Recover
      // an interrupted replacement only from its checksum-bound staged copy.
      const staged = join(dirname(checkpoint), 'next', basename(file));
      if (state.phase === 'app' || !matches(staged, expected)) throw new Error('Notarization artifact checksum does not match its checkpoint.');
      mkdirSync(dirname(file), { recursive: true });
      copyFileSync(staged, file);
    }
  }
  if (state.phase !== 'app') {
    rmSync(join(dirname(checkpoint), 'next'), { recursive: true, force: true });
    rmSync(join(dirname(checkpoint), 'app.zip'), { force: true });
  }
  return state;
}

function save(checkpoint, state) {
  const temporary = checkpoint + '.tmp';
  writeFileSync(temporary, JSON.stringify(state, null, 2) + '\n', { mode: 0o600 });
  renameSync(temporary, checkpoint);
}

function extractApp(archive, signing) {
  // These archives are produced by the captured release workflow and checked
  // against its saved hash before extraction. Refuse paths outside the bundle.
  const entries = signing.tool('unzip', ['-Z1', archive]).stdout.trim().split('\n');
  if (!entries.length || entries.some(name => !name.startsWith('MIER.app/') || name.includes('\\') || name.split('/').includes('..'))) {
    throw new Error('Invalid macOS checkpoint archive layout.');
  }
  const stage = mkdtempSync(join(signing.directory, 'resume-'));
  signing.tool('ditto', ['-x', '-k', archive, stage]);
  const app = join(stage, 'MIER.app');
  verifyMacOSApp(app, signing);
  return app;
}

function pending(state, signing) {
  const message = 'macOS/' + state.arch + ' is waiting for Apple (' + state.phase + '). Files and submission IDs are saved. ' +
    'Signed ZIP and DMG downloads can publish now with notarization marked as pending. ' +
    'Run Resume release with source run ' + (process.env.GITHUB_RUN_ID || 'this run') + ' later; no runner is polling.';
  console.log(message);
  if (signing.summaryPath) appendFileSync(signing.summaryPath, '\n' + message + '\n');
  return false;
}

export function advanceMacOSRelease(options, resume = false) {
  const { checkpoint, archive, dmg, version, arch, commit, signing, appBundle } = options;
  if (!['amd64', 'arm64'].includes(arch) || typeof version !== 'string' || !/^[a-f0-9]{40}$/.test(commit)) throw new Error('Invalid captured macOS release.');
  if (!signing.profile || !signing.keychain || !signing.teamID || signing.identity === '-') throw new Error('Release checkpoints require Developer ID signing and notarization.');
  mkdirSync(dirname(checkpoint), { recursive: true });
  const payload = join(dirname(checkpoint), 'app.zip');
  let state;
  if (resume) {
    // Missing or corrupt checkpoints never cause a rebuild or a fresh upload.
    state = readMacOSCheckpoint(options);
  } else {
    if (existsSync(checkpoint)) throw new Error('A notarization checkpoint already exists; resume it instead of rebuilding.');
    const app = stageMacOSApp(appBundle, signing);
    writeMacOSArchive(app, payload, signing);
    mkdirSync(dirname(archive), { recursive: true });
    copyFileSync(payload, archive);
    createMacOSDMG(app, dmg, signing);
    verifyMacOSDMG(dmg, signing);
    state = { format: 1, version, arch, commit, phase: 'app', appHash: hash(payload), archiveHash: hash(archive), dmgHash: hash(dmg),
      appSubmissionID: submitNotarization(payload, signing) };
    save(checkpoint, state);
  }
  if (state.phase === 'app') {
    const app = extractApp(payload, signing);
    if (!completeNotarization(state.appSubmissionID, app, signing)) return pending(state, signing);
    verifyMacOSApp(app, signing, true);
    const next = join(dirname(checkpoint), 'next');
    const nextArchive = join(next, basename(archive));
    const nextDMG = join(next, basename(dmg));
    writeMacOSArchive(app, nextArchive, signing, true);
    createMacOSDMG(app, nextDMG, signing);
    state = { ...state, phase: 'dmg', archiveHash: hash(nextArchive), dmgHash: hash(nextDMG), dmgSubmissionID: submitNotarization(nextDMG, signing) };
    save(checkpoint, state);
    copyFileSync(nextArchive, archive);
    copyFileSync(nextDMG, dmg);
    rmSync(next, { recursive: true, force: true });
    rmSync(payload, { force: true });
  }
  if (state.phase === 'dmg') {
    let accepted;
    try { accepted = completeNotarization(state.dmgSubmissionID, dmg, signing); }
    finally {
      // Stapling can change bytes before its own validation or Gatekeeper
      // fails. Preserve the checksum so a later runner can retry safely.
      const updated = hash(dmg);
      if (updated !== state.dmgHash) {
        state = { ...state, dmgHash: updated };
        save(checkpoint, state);
      }
    }
    if (!accepted) return pending(state, signing);
    verifyMacOSDMG(dmg, signing, true);
    state = { ...state, phase: 'ready' };
    save(checkpoint, state);
  } else {
    verifyMacOSApp(extractApp(archive, signing), signing, true);
    verifyMacOSDMG(dmg, signing, true);
  }
  console.log('macOS/' + arch + ' app, updater ZIP, and DMG are notarized and verified.');
  return true;
}
