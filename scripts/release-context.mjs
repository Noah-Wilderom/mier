import { appendFileSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';
import { macOSReleasePaths, parseTag } from './release.mjs';
import { readMacOSCheckpoint } from './macos-notarization.mjs';

export function validateReleaseContext(context) {
  if (context?.format !== 1 || !/^[a-f0-9]{40}$/.test(context.commit || '') || !/^[1-9][0-9]*$/.test(context.baseRun || '') ||
      parseTag(context.tag).version !== context.version) throw new Error('Invalid saved release context.');
  return context;
}

export function validateSourceRun(run, repository) {
  const workflows = { release: '.github/workflows/release.yml', workflow_dispatch: '.github/workflows/resume-release.yml' };
  if (run?.head_repository?.full_name !== repository || run.status !== 'completed' || !workflows[run.event] ||
      run.path !== workflows[run.event] || (run.event === 'workflow_dispatch' && run.head_branch !== 'master')) {
    throw new Error('Source must be a completed release or Resume release run from this repository.');
  }
  return run;
}

export function validateResumeContext(source, base, context) {
  validateReleaseContext(context);
  if (base.event !== 'release' || base.head_sha !== context.commit || base.head_branch !== context.tag ||
      String(base.id) !== context.baseRun || (source.event === 'release' && source.id !== base.id)) {
    throw new Error('Saved context does not match the original release run.');
  }
  return context;
}

export function validateResumeParent(sourceID, currentID, runs) {
  const child = runs.find(run => String(run.id) !== currentID && run.event === 'workflow_dispatch' && run.head_branch === 'master' &&
    run.display_title === 'Resume release from run ' + sourceID && run.conclusion === 'success');
  if (child) throw new Error('This source has newer saved checkpoints. Resume run ' + child.id + ' instead.');
}

export function validateUnpublishedRelease(release, context) {
  if (release?.tag_name !== context.tag || !Array.isArray(release.assets)) throw new Error('Saved release no longer exists.');
  if (release.assets.some(asset => asset.name === 'manifest.json')) throw new Error('This release is already complete; no resume is needed.');
}

function github(path) {
  const result = spawnSync('gh', ['api', path], { encoding: 'utf8', timeout: 30_000, maxBuffer: 1024 * 1024 });
  if (result.status !== 0) throw new Error('Could not read release metadata from GitHub: ' + (result.stderr || 'request failed'));
  return JSON.parse(result.stdout);
}

function repository() {
  const value = process.env.GITHUB_REPOSITORY;
  if (!/^[\w.-]+\/[\w.-]+$/.test(value || '')) throw new Error('Missing GitHub repository.');
  return value;
}

function sourceRun(id) {
  if (!/^[1-9][0-9]*$/.test(id || '')) throw new Error('Source run ID must be a positive integer.');
  return validateSourceRun(github('repos/' + repository() + '/actions/runs/' + id), repository());
}

function output(context) {
  if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT,
    ['tag', 'version', 'commit', 'baseRun'].map(key => key + '=' + context[key]).join('\n') + '\n');
}

function validateTag(context) {
  let object = github('repos/' + repository() + '/git/ref/tags/' + encodeURIComponent(context.tag)).object;
  for (let depth = 0; object?.type === 'tag' && depth < 5; depth++) object = github('repos/' + repository() + '/git/tags/' + object.sha).object;
  if (object?.type !== 'commit' || object.sha !== context.commit) throw new Error('Release tag moved or no longer matches the saved build.');
}

export function releaseReady(version, commit, root = 'dist', summaryPath) {
  const phases = ['amd64', 'arm64'].map(arch => readMacOSCheckpoint(macOSReleasePaths(arch, version, commit, root)).phase);
  const ready = phases.every(phase => phase === 'ready');
  if (!ready) {
    const message = 'Release is saved, awaiting Apple: amd64=' + phases[0] + ', arm64=' + phases[1] +
      '. No assets or update feed were published. Run Resume release later using source run ' + (process.env.GITHUB_RUN_ID || 'this run') + '.';
    console.log(message);
    if (summaryPath) appendFileSync(summaryPath, '\n' + message + '\n');
  }
  return ready;
}

function main(args) {
  if (args[0] === 'create') {
    const context = validateReleaseContext({ format: 1, tag: args[1], version: parseTag(args[1]).version, commit: args[2], baseRun: args[3] });
    mkdirSync(dirname(args[4]), { recursive: true });
    writeFileSync(args[4], JSON.stringify(context, null, 2) + '\n');
    output(context);
  } else if (args[0] === 'validate-run') {
    if (process.env.GITHUB_REF && process.env.GITHUB_REF !== 'refs/heads/master') throw new Error('Run Resume release from the master branch.');
    sourceRun(args[1]);
    const runs = github('repos/' + repository() + '/actions/workflows/resume-release.yml/runs?per_page=100').workflow_runs;
    validateResumeParent(args[1], process.env.GITHUB_RUN_ID, runs);
  } else if (args[0] === 'resume') {
    const source = sourceRun(args[1]);
    const context = validateReleaseContext(JSON.parse(readFileSync(args[2], 'utf8')));
    const base = sourceRun(context.baseRun);
    validateResumeContext(source, base, context);
    validateTag(context);
    validateUnpublishedRelease(github('repos/' + repository() + '/releases/tags/' + encodeURIComponent(context.tag)), context);
    output(context);
  } else if (args[0] === 'ready') {
    const ready = releaseReady(args[1], args[2], 'dist', process.env.GITHUB_STEP_SUMMARY);
    if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, 'ready=' + ready + '\n');
  } else if (args[0] === 'validate-tag') {
    parseTag(args[1]);
    if (!/^[a-f0-9]{40}$/.test(args[2] || '')) throw new Error('Invalid release commit.');
    validateTag({ tag: args[1], commit: args[2] });
  } else {
    throw new Error('Usage: release-context.mjs create TAG COMMIT RUN_ID FILE | validate-run RUN_ID | resume RUN_ID FILE | ready VERSION COMMIT | validate-tag TAG COMMIT');
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try { main(process.argv.slice(2)); }
  catch (error) { console.error(error.message); process.exitCode = 1; }
}
