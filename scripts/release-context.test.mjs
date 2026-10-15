import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { macOSReleasePaths } from './release.mjs';
import { releaseReady, validateReleaseContext, validateResumeContext, validateResumeParent, validateSourceRun, validateUnpublishedRelease } from './release-context.mjs';

const repository = 'example/mier';
const commit = 'a'.repeat(40);
const context = { format: 1, tag: 'v0.1.0', version: '0.1.0', commit, baseRun: '123' };
const base = { id: 123, event: 'release', head_branch: context.tag, head_sha: commit, head_repository: { full_name: repository }, status: 'completed', path: '.github/workflows/release.yml' };

test('saved context preserves the original release revision and root run across resumes', () => {
  const resumed = { ...base, id: 456, event: 'workflow_dispatch', head_branch: 'master', path: '.github/workflows/resume-release.yml' };
  assert.equal(validateSourceRun(base, repository), base);
  assert.equal(validateSourceRun(resumed, repository), resumed);
  assert.equal(validateResumeContext(base, base, context), context);
  assert.equal(validateResumeContext(resumed, base, context), context);
  for (const change of [{ format: 2 }, { tag: 'v0.2.0' }, { commit: 'not-a-sha' }, { baseRun: '--help' }, { baseRun: '0' }]) {
    assert.throws(() => validateReleaseContext({ ...context, ...change }));
  }
  for (const change of [{ id: 789 }, { head_sha: 'b'.repeat(40) }, { head_branch: 'v0.2.0' }, { event: 'workflow_dispatch' }]) {
    assert.throws(() => validateResumeContext(resumed, { ...base, ...change }, context));
  }
  assert.throws(() => validateResumeContext({ ...base, id: 789 }, base, context));
});

test('foreign, PR, unfinished, unexpected-workflow and non-master resume artifacts are rejected', () => {
  for (const change of [
    { head_repository: { full_name: 'attacker/mier' } }, { status: 'in_progress' }, { event: 'pull_request' },
    { path: '.github/workflows/test.yml' }, { event: 'workflow_dispatch', path: '.github/workflows/resume-release.yml', head_branch: 'feature' },
  ]) assert.throws(() => validateSourceRun({ ...base, ...change }, repository));
});

test('resuming an older parent points to the newer checkpoints instead of duplicating uploads', () => {
  const child = { id: 456, event: 'workflow_dispatch', head_branch: 'master', display_title: 'Resume release from run 123', conclusion: 'success' };
  assert.throws(() => validateResumeParent('123', '789', [child]), /Resume run 456/);
  assert.doesNotThrow(() => validateResumeParent('123', '456', [child]));
  assert.doesNotThrow(() => validateResumeParent('999', '789', [child]));
  assert.doesNotThrow(() => validateResumeParent('123', '789', [{ ...child, conclusion: 'failure' }]));
});

test('deleted or already published releases cannot start billed Mac resume jobs', () => {
  assert.doesNotThrow(() => validateUnpublishedRelease({ tag_name: context.tag, assets: [] }, context));
  assert.throws(() => validateUnpublishedRelease({ tag_name: context.tag, assets: [{ name: 'manifest.json' }] }, context), /already complete/);
  assert.throws(() => validateUnpublishedRelease({ tag_name: 'v0.2.0', assets: [] }, context), /no longer exists/);
});

test('both Mac checkpoints and their exact archives are required before publication', t => {
  const root = mkdtempSync(join(tmpdir(), 'mier-release-ready-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const summary = join(root, 'summary.md');
  const sha = file => createHash('sha256').update(readFileSync(file)).digest('hex');
  for (const arch of ['amd64', 'arm64']) {
    const paths = macOSReleasePaths(arch, context.version, commit, root);
    for (const path of [paths.archive, paths.dmg, paths.checkpoint]) mkdirSync(dirname(path), { recursive: true });
    writeFileSync(paths.archive, 'fixture ZIP ' + arch);
    writeFileSync(paths.dmg, 'fixture DMG ' + arch);
    const state = { format: 1, version: context.version, commit, arch, phase: arch === 'amd64' ? 'ready' : 'dmg', appSubmissionID: '01234567-89ab-cdef-0123-456789abcdef', dmgSubmissionID: 'fedcba98-7654-3210-fedc-ba9876543210', archiveHash: sha(paths.archive), dmgHash: sha(paths.dmg) };
    writeFileSync(paths.checkpoint, JSON.stringify(state));
  }
  assert.equal(releaseReady(context.version, commit, root, summary), false);
  assert.match(readFileSync(summary, 'utf8'), /No assets or update feed were published/);
  const arm = macOSReleasePaths('arm64', context.version, commit, root);
  const state = JSON.parse(readFileSync(arm.checkpoint, 'utf8'));
  writeFileSync(arm.checkpoint, JSON.stringify({ ...state, phase: 'ready' }));
  assert.equal(releaseReady(context.version, commit, root), true);
  writeFileSync(arm.archive, 'tampered archive');
  assert.throws(() => releaseReady(context.version, commit, root), /checksum/);
  assert.throws(() => releaseReady(context.version, 'b'.repeat(40), root), /captured release/);
  rmSync(arm.checkpoint);
  assert.throws(() => releaseReady(context.version, commit, root), /ENOENT/);
});
