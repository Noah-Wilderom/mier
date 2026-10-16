import {strict as assert} from 'node:assert'
import {test} from 'node:test'
import {diffSections,sourceLines,type ReviewFileContent} from './diffContext.ts'
import type {ChangedFile} from './data.ts'
const source=(old:string,next:string):ReviewFileContent=>({path:'file.ts',previousPath:'',headSha:'b'.repeat(40),baseSha:'a'.repeat(40),mergeBaseSha:'a'.repeat(40),old,new:next,oldExists:true,newExists:true})
test('context expands exact old/new line pairs around a changed section',()=>{
 const file:ChangedFile={path:'file.ts',status:'modified',additions:1,deletions:1,rawPatch:'@@ -3,1 +3,1 @@\n-old\n+new',lines:[{hunk:0,kind:'remove',oldLine:3,text:'old'},{hunk:0,kind:'add',newLine:3,text:'new'}]}
 const result=diffSections(file,source('one\ntwo\nold\nfour\nfive\n','one\ntwo\nnew\nfour\nfive\n'),{0:{above:20,below:20}})[0]
 assert.deepEqual(result.lines.map(l=>[l.kind,l.oldLine,l.newLine,l.text]),[['context',1,1,'one'],['context',2,2,'two'],['remove',3,undefined,'old'],['add',undefined,3,'new'],['context',4,4,'four'],['context',5,5,'five']])
 assert.equal(result.above,0);assert.equal(result.below,0)
})
test('overlapping expanded hunks never duplicate context or hide changed lines',()=>{
 const file:ChangedFile={path:'file.ts',status:'modified',additions:2,deletions:2,rawPatch:'@@ -2 +2 @@\n-b\n+B\n@@ -5 +5 @@\n-e\n+E',lines:[{hunk:0,kind:'remove',oldLine:2,text:'b'},{hunk:0,kind:'add',newLine:2,text:'B'},{hunk:1,kind:'remove',oldLine:5,text:'e'},{hunk:1,kind:'add',newLine:5,text:'E'}]}
 const sections=diffSections(file,source('a\nb\nc\nd\ne\nf\n','a\nB\nc\nd\nE\nf\n'),{0:{above:20,below:20},1:{above:20,below:20}})
 const lines=sections.flatMap(s=>s.lines)
 assert.deepEqual(lines.filter(l=>l.newLine!==undefined).map(l=>l.newLine),[1,2,3,4,5,6]);assert.equal(lines.filter(l=>l.kind==='remove').length,2)
})
test('zero-length hunk anchors preserve alignment after insertions and deletions',()=>{
 const file:ChangedFile={path:'file.ts',status:'modified',additions:1,deletions:0,rawPatch:'@@ -2,0 +3 @@\n+insert',lines:[{hunk:0,kind:'add',newLine:3,text:'insert'}]}
 const result=diffSections(file,source('a\nb\nc\n','a\nb\ninsert\nc\n'),{0:{above:20,below:20}})[0]
 assert.deepEqual(result.lines.map(l=>[l.oldLine,l.newLine]),[[1,1],[2,2],[undefined,3],[3,4]])
})
test('truncated patches and mismatched source do not fabricate unchanged context',()=>{
 const file:ChangedFile={path:'file.ts',status:'modified',additions:1,deletions:1,rawPatch:'@@ -2 +2 @@',lines:[{hunk:0,kind:'remove',oldLine:2,text:'old'},{hunk:0,kind:'add',newLine:2,text:'new'}]}
 assert.equal(diffSections({...file,truncated:true},source('a\nold\nc','a\nnew\nc'),{0:{above:20,below:20}})[0].lines.length,2)
 assert.equal(diffSections(file,source('different\nold\nx','a\nnew\nc'),{0:{above:20,below:20}})[0].lines.length,2)
 assert.deepEqual(sourceLines('a\r\nb\r\n'),['a','b']);assert.deepEqual(sourceLines(''),[])
})
