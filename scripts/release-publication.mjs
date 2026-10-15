import { macOSReleasePaths } from './release.mjs';
import { readMacOSCheckpoint } from './macos-notarization.mjs';

const architectures = ['amd64', 'arm64'];
const phases = ['app', 'dmg', 'ready'];
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
const digest = /^[a-f0-9]{64}$/;
const start = '<!-- mier:macos-notarization:start -->';
const end = '<!-- mier:macos-notarization:end -->';

export function validatePublicationStatus(publication, context) {
  if (publication?.format !== 1 || ['tag', 'version', 'commit', 'baseRun'].some(key => publication[key] !== context[key]) ||
      !/^[1-9][0-9]*$/.test(publication.sourceRun || '') || typeof publication.complete !== 'boolean' ||
      !publication.macos || Object.keys(publication.macos).sort().join(',') !== architectures.join(',')) {
    throw new Error('Published notarization status does not match the captured release.');
  }
  for (const arch of architectures) {
    const state = publication.macos[arch];
    if (!state || !phases.includes(state.phase) || state.status !== (state.phase === 'ready' ? 'notarized' : 'pending') ||
        !uuid.test(state.appSubmissionID || '') || (state.phase !== 'app' && !uuid.test(state.dmgSubmissionID || '')) ||
        !digest.test(state.archiveSHA256 || '') || !digest.test(state.dmgSHA256 || '')) {
      throw new Error('Invalid published macOS notarization status for ' + arch + '.');
    }
  }
  if (publication.complete !== architectures.every(arch => publication.macos[arch].status === 'notarized')) {
    throw new Error('Published notarization completion does not match both Mac architectures.');
  }
  return publication;
}

export function releasePublication(context, root = 'dist', sourceRun = process.env.GITHUB_RUN_ID || context.baseRun) {
  const macos = Object.fromEntries(architectures.map(arch => {
    const state = readMacOSCheckpoint(macOSReleasePaths(arch, context.version, context.commit, root));
    return [arch, {
      status: state.phase === 'ready' ? 'notarized' : 'pending', phase: state.phase,
      appSubmissionID: state.appSubmissionID,
      ...(state.dmgSubmissionID ? { dmgSubmissionID: state.dmgSubmissionID } : {}),
      archiveSHA256: state.archiveHash, dmgSHA256: state.dmgHash,
    }];
  }));
  return validatePublicationStatus({
    ...context, sourceRun, complete: architectures.every(arch => macos[arch].status === 'notarized'), macos,
  }, context);
}

export function validatePublicationTransition(previous, next, context) {
  validatePublicationStatus(previous, context);
  validatePublicationStatus(next, context);
  for (const arch of architectures) {
    const before = previous.macos[arch];
    const after = next.macos[arch];
    if (phases.indexOf(after.phase) < phases.indexOf(before.phase) ||
        (before.phase === 'ready' && (after.archiveSHA256 !== before.archiveSHA256 || after.dmgSHA256 !== before.dmgSHA256))) {
      throw new Error('Publication would replace newer notarization files for ' + arch + '. Resume the latest saved run.');
    }
  }
}

export function renderReleaseNotes(notes, publication) {
  const opening = notes.indexOf(start);
  const closing = notes.indexOf(end);
  if ((opening < 0) !== (closing < 0) || (opening >= 0 && (closing < opening ||
      notes.indexOf(start, opening + start.length) >= 0 || notes.indexOf(end, closing + end.length) >= 0))) {
    throw new Error('Release notes contain an invalid managed notarization notice.');
  }
  const body = (opening < 0 ? notes : notes.slice(0, opening) + notes.slice(closing + end.length)).trimEnd();
  const pending = architectures.filter(arch => publication.macos[arch].status === 'pending')
    .map(arch => arch === 'amd64' ? 'Intel' : 'Apple silicon');
  const notice = pending.length
    ? '**macOS notarization pending (' + pending.join(', ') + ').** The macOS downloads are Developer ID signed, but Apple is still processing them. Gatekeeper may prevent them from opening until approval. Notarized and stapled downloads will replace these files after processing completes.'
    : '**macOS notarization complete.** Both macOS downloads are Developer ID signed, notarized, and stapled.';
  return body + (body ? '\n\n' : '') + start + '\n' + notice + '\n' + end + '\n';
}
