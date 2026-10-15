import { test } from 'node:test';
import assert from 'node:assert/strict';
import { validateReleaseContext, validateResumableRelease, validateResumeContext, validateResumeParent, validateSourceRun } from './release-context.mjs';

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
  assert.doesNotThrow(() => validateResumableRelease({ tag_name: context.tag, assets: [] }, context));
  assert.throws(() => validateResumableRelease({ tag_name: context.tag, assets: [{ name: 'manifest.json' }] }, context), /already complete/);
  assert.throws(() => validateResumableRelease({ tag_name: 'v0.2.0', assets: [] }, context), /no longer exists/);
});

test('an already published release can resume only with matching pending notarization metadata', () => {
  const release = { tag_name: context.tag, assets: [{ name: 'manifest.json' }, { name: 'notarization.json' }] };
  const mac = { status: 'pending', phase: 'app', appSubmissionID: '01234567-89ab-cdef-0123-456789abcdef', archiveSHA256: 'a'.repeat(64), dmgSHA256: 'b'.repeat(64) };
  const publication = { ...context, sourceRun: '123', complete: false, macos: { amd64: mac, arm64: mac } };
  assert.doesNotThrow(() => validateResumableRelease(release, context, publication));
  assert.throws(() => validateResumableRelease(release, context), /already complete/);
  for (const change of [{ commit: 'b'.repeat(40) }, { baseRun: '456' }, { complete: true }, { macos: {} }]) {
    assert.throws(() => validateResumableRelease(release, context, { ...publication, ...change }));
  }
  const accepted = { ...mac, status: 'notarized', phase: 'ready', dmgSubmissionID: 'fedcba98-7654-3210-fedc-ba9876543210' };
  assert.throws(() => validateResumableRelease(release, context, { ...publication, complete: true, macos: { amd64: accepted, arm64: accepted } }), /already complete/);
});
