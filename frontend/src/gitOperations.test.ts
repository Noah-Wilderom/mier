import { strict as assert } from 'node:assert'
import { test } from 'node:test'
import { bisectFinished, conflictChunks, containsConflictMarkers, resolveChunk } from './gitOperations.ts'

test('bisect completion handles old Git, quoted terms, ambiguous results, and structured state',()=>{
 for(const term of ['bad',"'bad'","'slow'"])assert.equal(bisectFinished({message:`# first ${term} commit: [${'a'.repeat(40)}] Failure`}),true)
 assert.equal(bisectFinished({message:"# possible first 'bad' commit: [sha] Maybe"}),false)
 assert.equal(bisectFinished({message:'# only skipped commits left to test'}),true)
 assert.equal(bisectFinished({message:'',bisectDone:true}),true)
})

test('conflict choices preserve surrounding code, multiple sections, and CRLF',()=>{
  const source='before\r\n<<<<<<< HEAD\r\ncurrent\r\n=======\r\nincoming\r\n>>>>>>> feature\r\nafter\r\n<<<<<<< HEAD\r\nleft\r\n=======\r\nright\r\n>>>>>>> feature'
  const chunks=conflictChunks(source)
  assert.equal(chunks.length,2)
  const next=resolveChunk(source,chunks[0],'both')
  assert.ok(resolveChunk(source,chunks[0],'incoming-first').startsWith('before\r\nincoming\r\ncurrent\r\nafter\r\n'))
  assert.ok(next.startsWith('before\r\ncurrent\r\nincoming\r\nafter\r\n'))
  assert.equal(conflictChunks(next).length,1)
  const final=resolveChunk(next,conflictChunks(next)[0],'incoming')
  assert.equal(final,'before\r\ncurrent\r\nincoming\r\nafter\r\nright\r\n')
  assert.equal(containsConflictMarkers(final),false)
})
test('diff3 and custom marker sizes expose the common ancestor without merging it into the result',()=>{
  const source='<<<<<<<<<< ours\ncurrent\n|||||||||| base\nancestor\n==========\nincoming\n>>>>>>>>>> theirs\n'
  const [chunk]=conflictChunks(source)
  assert.equal(chunk.base,'ancestor\n')
  assert.equal(resolveChunk(source,chunk,'both'),'current\nincoming\n')
})
test('malformed or unmatched markers cannot be saved as a resolved result',()=>{
  const source='<<<<<<< HEAD\ncurrent\n=======\nincoming\n'
  assert.equal(conflictChunks(source).length,0)
  assert.equal(containsConflictMarkers(source),true)
})
