import test from 'node:test'
import assert from 'node:assert/strict'
import { fileChangeStatus } from './fileChangeStatus.ts'

test('a partially staged addition shows added in the index and modified in the working tree', () => {
  const file = { status: 'added', indexStatus: 'A', worktreeStatus: 'M' }
  assert.equal(fileChangeStatus(file, 'index').label, 'Added')
  assert.equal(fileChangeStatus(file, 'working').label, 'Modified')
  const deletion = { status: 'deleted', indexStatus: 'M', worktreeStatus: 'D' }
  assert.equal(fileChangeStatus(deletion, 'index').label, 'Modified')
  assert.equal(fileChangeStatus(deletion, 'working').label, 'Deleted')
})
test('rename, copy, type changes, untracked files and merge conflicts retain distinct meanings', () => {
  assert.equal(fileChangeStatus({ status: 'modified', indexStatus: 'R' }, 'index').label, 'Renamed')
  assert.equal(fileChangeStatus({ status: 'modified', indexStatus: 'C' }, 'index').label, 'Copied')
  assert.equal(fileChangeStatus({ status: 'modified', worktreeStatus: 'T' }, 'working').label, 'File type changed')
  assert.equal(fileChangeStatus({ status: 'added', untracked: true }, 'working').label, 'Untracked')
  assert.equal(fileChangeStatus({ status: 'added', indexStatus: 'A', worktreeStatus: 'U', conflict: true }, 'index').label, 'Merge conflict')
})
test('older snapshots and PR files work without XY metadata; unknown statuses remain neutral', () => {
  assert.equal(fileChangeStatus({ status: 'modified' }).icon, 'dot')
  assert.equal(fileChangeStatus({ status: 'added' }).icon, 'plus')
  assert.equal(fileChangeStatus({ status: 'removed' }).icon, 'minus')
  assert.equal(fileChangeStatus({ status: 'renamed' }).icon, 'arrow-right')
  assert.equal(fileChangeStatus({ status: '__proto__' }).label, 'Changed')
})
