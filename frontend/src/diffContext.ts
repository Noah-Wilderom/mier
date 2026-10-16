import type { ChangedFile, DiffLine } from './data'

export interface ReviewFileRequest {host:string;owner:string;repository:string;pullRequest:number;headSha:string;baseSha:string;path:string}
export interface ReviewFileContent {path:string;previousPath:string;headSha:string;baseSha:string;mergeBaseSha:string;old:string;new:string;oldExists:boolean;newExists:boolean}
export interface ContextWindow {above:number;below:number}
export interface DiffSection {hunk:number;header:string;lines:DiffLine[];above:number;below:number}
export const sourceLines=(text:string):string[]=>text===''?[]:text.replace(/\r\n/g,'\n').replace(/\n$/,'').split('\n')

/** Expand only verified unchanged pairs between original, complete hunks. */
export function diffSections(file:ChangedFile,source?:ReviewFileContent,windows:Record<number,ContextWindow>={}):DiffSection[]{
  const headers=[...(file.rawPatch??'').matchAll(/^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@.*$/gm)]
  const sections:DiffSection[]=[]
  const old=source?sourceLines(source.old):[],next=source?sourceLines(source.new):[]
  for(let hunk=0;hunk<headers.length;hunk++){
    const header=headers[hunk],oldCount=header[2]===undefined?1:Number(header[2]),newCount=header[4]===undefined?1:Number(header[4])
    const oldBefore=Number(header[1])-(oldCount?1:0),newBefore=Number(header[3])-(newCount?1:0)
    const lines=file.lines.filter(line=>(line.hunk??0)===hunk)
    // Incomplete patches cannot establish that the gap is unchanged.
    const complete=!file.truncated&&lines.filter(l=>l.oldLine!==undefined).length===oldCount&&lines.filter(l=>l.newLine!==undefined).length===newCount
    let above=0,below=0
    if(source&&complete){
      const previous=headers[hunk-1],following=headers[hunk+1]
      const previousEnd=(side:1|3)=>previous?Number(previous[side])-(Number(previous[side+1]??1)?1:0)+Number(previous[side+1]??1):0
      const nextStart=(side:1|3,total:number)=>following?Number(following[side])-(Number(following[side+1]??1)?1:0):total
      const maxAbove=Math.min(oldBefore-previousEnd(1),newBefore-previousEnd(3))
      while(above<maxAbove&&old[oldBefore-above-1]!==undefined&&old[oldBefore-above-1]===next[newBefore-above-1])above++
      const maxBelow=Math.min(nextStart(1,old.length)-oldBefore-oldCount,nextStart(3,next.length)-newBefore-newCount)
      while(below<maxBelow&&old[oldBefore+oldCount+below]!==undefined&&old[oldBefore+oldCount+below]===next[newBefore+newCount+below])below++
    }
    const before=Math.min(above,windows[hunk]?.above??0),after=Math.min(below,windows[hunk]?.below??0)
    const prefix=Array.from({length:before},(_,i):DiffLine=>({hunk,kind:'context',oldLine:oldBefore-before+i+1,newLine:newBefore-before+i+1,text:old[oldBefore-before+i]}))
    const suffix=Array.from({length:after},(_,i):DiffLine=>({hunk,kind:'context',oldLine:oldBefore+oldCount+i+1,newLine:newBefore+newCount+i+1,text:old[oldBefore+oldCount+i]}))
    // Shared gaps belong to the preceding hunk when its expansion reaches them.
    const previousSection=sections[sections.length-1]
    const previousLast=previousSection?.lines[previousSection.lines.length-1]
    const expanded=[...prefix,...lines,...suffix].filter(l=>!previousLast||l.kind!=='context'||l.oldLine===undefined||previousLast.oldLine===undefined||l.oldLine>previousLast.oldLine)
    sections.push({hunk,header:header[0],lines:expanded,above:above-before,below:below-after})
  }
  return sections.length?sections:[{hunk:0,header:'Changed section',lines:file.lines,above:0,below:0}]
}
