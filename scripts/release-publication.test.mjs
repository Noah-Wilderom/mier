import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { delimiter, dirname, join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { macOSReleasePaths } from './release.mjs';
import { releasePublication, renderReleaseNotes, validatePublicationStatus, validatePublicationTransition } from './release-publication.mjs';

const context = { format: 1, tag: 'v0.1.0', version: '0.1.0', commit: 'a'.repeat(40), baseRun: '123' };
const appID = '01234567-89ab-cdef-0123-456789abcdef';
const dmgID = 'fedcba98-7654-3210-fedc-ba9876543210';

function fixture(t, phases = ['app', 'app']) {
  const directory = mkdtempSync(join(tmpdir(), 'mier-release-publication-'));
  const root = join(directory, 'dist');
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const sha = file => createHash('sha256').update(readFileSync(file)).digest('hex');
  for (const [index, arch] of ['amd64', 'arm64'].entries()) {
    const paths = macOSReleasePaths(arch, context.version, context.commit, root);
    for (const file of [paths.archive, paths.dmg, paths.checkpoint]) mkdirSync(dirname(file), { recursive: true });
    writeFileSync(paths.archive, 'fixture ZIP ' + arch);
    writeFileSync(paths.dmg, 'fixture DMG ' + arch);
    writeFileSync(join(dirname(paths.checkpoint), 'app.zip'), readFileSync(paths.archive));
    writeFileSync(paths.checkpoint, JSON.stringify({
      ...context, arch, phase: phases[index], appSubmissionID: appID, dmgSubmissionID: dmgID,
      appHash: sha(paths.archive), archiveHash: sha(paths.archive), dmgHash: sha(paths.dmg),
    }));
  }
  return { directory, root, publication: releasePublication(context, root, '456') };
}

test('signed ZIPs and DMGs are publishable while both Mac submissions are pending', t => {
  const { publication } = fixture(t);
  assert.equal(publication.complete, false);
  assert.equal(publication.sourceRun, '456');
  for (const arch of ['amd64', 'arm64']) {
    assert.equal(publication.macos[arch].status, 'pending');
    assert.equal(publication.macos[arch].phase, 'app');
    assert.match(publication.macos[arch].archiveSHA256, /^[a-f0-9]{64}$/);
  }
  assert.equal(validatePublicationStatus(publication, context), publication);
});

test('publication checks saved files and captured revision even when Mac notarization is pending', t => {
  const { root, publication } = fixture(t, ['ready', 'dmg']);
  assert.equal(publication.complete, false);
  assert.equal(publication.macos.amd64.status, 'notarized');
  assert.equal(publication.macos.arm64.status, 'pending');
  const arm = macOSReleasePaths('arm64', context.version, context.commit, root);
  assert.throws(() => releasePublication({ ...context, commit: 'b'.repeat(40) }, root), /captured release/);
  writeFileSync(arm.archive, 'tampered pending download');
  assert.throws(() => releasePublication(context, root), /checksum/);
  rmSync(arm.checkpoint);
  assert.throws(() => releasePublication(context, root), /ENOENT/);
});

test('release notes preserve user content and replace the pending notice after completion', t => {
  const { publication } = fixture(t);
  const original = '# Changes\n\n**Full Changelog**: https://example.com/compare\n\nLiteral `$(do-not-run)` text.\n';
  const pending = renderReleaseNotes(original, publication);
  assert.match(pending, /macOS notarization pending \(Intel, Apple silicon\)/);
  assert.ok(pending.startsWith(original.trimEnd()));
  assert.equal(renderReleaseNotes(pending, publication), pending);
  const completed = fixture(t, ['ready', 'ready']).publication;
  assert.equal(completed.complete, true);
  const ready = renderReleaseNotes(pending, completed);
  assert.ok(ready.startsWith(original.trimEnd()));
  assert.ok(!ready.includes('notarization pending'));
  assert.match(ready, /macOS notarization complete/);
  assert.equal(renderReleaseNotes(ready, completed), ready);
  const mixed = fixture(t, ['ready', 'dmg']).publication;
  assert.match(renderReleaseNotes(original, mixed), /notarization pending \(Apple silicon\)/);
});

test('malformed managed notices fail instead of deleting release notes', t => {
  const { publication } = fixture(t);
  const start = '<!-- mier:macos-notarization:start -->';
  const end = '<!-- mier:macos-notarization:end -->';
  for (const notes of [start, end, end + start, start + start + end, start + end + end]) {
    assert.throws(() => renderReleaseNotes(notes, publication), /invalid managed/);
  }
});

test('foreign, malformed, and incorrectly completed publication metadata is rejected', t => {
  const { publication } = fixture(t);
  for (const change of [
    { format: 2 }, { tag: 'v0.2.0' }, { version: '0.2.0' }, { commit: 'b'.repeat(40) }, { baseRun: '999' },
    { sourceRun: '--help' }, { complete: true }, { complete: 'false' }, { macos: {} },
    { macos: { ...publication.macos, arm64: { ...publication.macos.arm64, phase: 'ready' } } },
    { macos: { ...publication.macos, arm64: { ...publication.macos.arm64, archiveSHA256: 'bad' } } },
  ]) assert.throws(() => validatePublicationStatus({ ...publication, ...change }, context));
});

test('later publication advances notarization and cannot overwrite newer or completed files', t => {
  const pending = fixture(t).publication;
  const processing = fixture(t, ['dmg', 'dmg']).publication;
  const completed = fixture(t, ['ready', 'ready']).publication;
  assert.doesNotThrow(() => validatePublicationTransition(pending, processing, context));
  assert.doesNotThrow(() => validatePublicationTransition(processing, completed, context));
  assert.doesNotThrow(() => validatePublicationTransition(completed, completed, context));
  assert.throws(() => validatePublicationTransition(processing, pending, context), /newer notarization files/);
  assert.throws(() => validatePublicationTransition(completed, processing, context), /newer notarization files/);
  const changed = { ...completed, macos: { ...completed.macos, arm64: { ...completed.macos.arm64, dmgSHA256: 'b'.repeat(64) } } };
  assert.throws(() => validatePublicationTransition(completed, changed, context), /newer notarization files/);
  assert.throws(() => validatePublicationTransition(pending, { ...completed, baseRun: '999' }, context), /captured release/);
});

test('publication and resume commands support an existing manifest with pending Mac files', { skip: process.platform === 'win32' }, t => {
  const { directory, root } = fixture(t);
  const bin = join(directory, 'bin');
  mkdirSync(bin);
  const metadata = join(directory, 'github.json');
  const remote = {
    release: { tag_name: context.tag, body: 'Original release notes.\n', assets: [] },
    base: { id: 123, event: 'release', head_branch: context.tag, head_sha: context.commit,
      head_repository: { full_name: 'example/mier' }, status: 'completed', path: '.github/workflows/release.yml' },
  };
  const gh = join(bin, 'gh');
  writeFileSync(gh, `#!/usr/bin/env node
import { readFileSync } from 'node:fs';
const remote = JSON.parse(readFileSync(process.env.MIER_PUBLICATION_TEST_GITHUB, 'utf8'));
const args = process.argv.slice(2);
let value;
if (args[0] === 'api' && args[1].includes('/git/ref/tags/')) value = {object:{type:'commit',sha:remote.base.head_sha}};
else if (args[0] === 'api' && args[1].includes('/releases/tags/')) value = remote.release;
else if (args[0] === 'api' && args[1].endsWith('/actions/runs/123')) value = remote.base;
else if (args[0] === 'release' && args[1] === 'download' && args.includes('notarization.json') && args.at(-1) === '-') value = remote.publication;
else throw new Error('Unexpected GitHub request: ' + JSON.stringify(args));
process.stdout.write(JSON.stringify(value));
`);
  chmodSync(gh, 0o755);
  const env = {
    ...process.env, PATH: bin + delimiter + process.env.PATH, MIER_PUBLICATION_TEST_GITHUB: metadata,
    GITHUB_REPOSITORY: 'example/mier', GITHUB_RUN_ID: '789', GITHUB_OUTPUT: join(directory, 'outputs'),
    GITHUB_STEP_SUMMARY: join(directory, 'summary.md'), MIER_RELEASE_NOTES_FILE: join(root, 'release-notes.md'),
  };
  const cli = (...args) => spawnSync(process.execPath, [fileURLToPath(new URL('./release-context.mjs', import.meta.url)), ...args],
    { cwd: directory, env, encoding: 'utf8' });
  writeFileSync(metadata, JSON.stringify(remote));
  const first = cli('prepare-publication', context.tag, context.commit, context.baseRun);
  assert.equal(first.status, 0, first.stderr);
  assert.match(first.stdout, /Publishing signed downloads and updates for all six targets/);
  const published = JSON.parse(readFileSync(join(root, 'notarization.json'), 'utf8'));
  assert.equal(published.complete, false);
  assert.equal(published.sourceRun, '789');
  assert.match(readFileSync(join(root, 'release-notes.md'), 'utf8'), /Original release notes\.[\s\S]*notarization pending/);
  remote.publication = published;
  remote.release.assets = [{ name: 'manifest.json' }, { name: 'notarization.json' }];
  remote.release.body = readFileSync(join(root, 'release-notes.md'), 'utf8');
  writeFileSync(metadata, JSON.stringify(remote));
  const saved = join(root, 'release-context.json');
  writeFileSync(saved, JSON.stringify(context));
  const resumed = cli('resume', '123', saved);
  assert.equal(resumed.status, 0, resumed.stderr);
  assert.match(readFileSync(env.GITHUB_OUTPUT, 'utf8'), /baseRun=123/);
  assert.equal(cli('prepare-publication', context.tag, context.commit, context.baseRun).status, 0);
  assert.equal(readFileSync(join(root, 'release-notes.md'), 'utf8'), remote.release.body);
  remote.publication.commit = 'b'.repeat(40);
  writeFileSync(metadata, JSON.stringify(remote));
  const foreign = cli('resume', '123', saved);
  assert.equal(foreign.status, 1);
  assert.match(foreign.stderr, /does not match the captured release/);
});
