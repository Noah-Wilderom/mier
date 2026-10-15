import { useEffect, useRef } from 'react'
import { basicSetup, EditorView } from 'codemirror'
import { Compartment, EditorState, Transaction } from '@codemirror/state'
import { LanguageDescription, syntaxHighlighting } from '@codemirror/language'
import { languages } from '@codemirror/language-data'
import { oneDarkHighlightStyle } from '@codemirror/theme-one-dark'
import './CodeEditor.css'

// A controlled editor keeps the user's cursor and undo history while React
// updates the surrounding conflict UI. Language parsers load only when needed.
export default function CodeEditor({value,onChange,path,label,disabled,dark,revealLine,revealKey}:{value:string;onChange:(value:string)=>void;path:string;label:string;disabled:boolean;dark:boolean;revealLine?:number;revealKey?:string}){
  const parent=useRef<HTMLDivElement>(null)
  const editor=useRef<EditorView>()
  const change=useRef(onChange);change.current=onChange
  const compartments=useRef({language:new Compartment(),theme:new Compartment(),access:new Compartment()})
  useEffect(()=>{
    if(!parent.current)return
    const {language,theme,access}=compartments.current
    const view=new EditorView({parent:parent.current,state:EditorState.create({doc:value.replace(/\r\n/g,'\n'),extensions:[basicSetup,EditorState.tabSize.of(4),language.of([]),theme.of([]),access.of([]),EditorView.contentAttributes.of({'aria-label':label,'aria-multiline':'true',role:'textbox'}),EditorView.updateListener.of(update=>{if(update.docChanged&&!update.transactions.some(transaction=>transaction.annotation(Transaction.remote)))change.current(update.state.doc.toString())})]})})
    editor.current=view
    return()=>{editor.current=undefined;view.destroy()}
  },[path,label])
  useEffect(()=>{
    const view=editor.current;if(!view)return
    const normalized=value.replace(/\r\n/g,'\n')
    if(view.state.doc.toString()!==normalized)view.dispatch({changes:{from:0,to:view.state.doc.length,insert:normalized},annotations:Transaction.remote.of(true)})
  },[value,path,label])
  useEffect(()=>{
    const view=editor.current;if(!view)return
    view.dispatch({effects:compartments.current.access.reconfigure([EditorState.readOnly.of(disabled),EditorView.editable.of(!disabled)])})
  },[disabled,path,label])
  useEffect(()=>{
    const view=editor.current;if(!view)return
    view.dispatch({effects:compartments.current.theme.reconfigure([EditorView.theme({'&':{height:'100%',backgroundColor:'var(--surface)',color:'var(--text)'},'.cm-scroller':{fontFamily:'var(--mono)',fontSize:'var(--review-code-font-size,13px)',lineHeight:'var(--review-code-line-height,23px)'},'.cm-gutters':{backgroundColor:'var(--panel)',color:'var(--muted)',borderColor:'var(--border)'},'.cm-cursor':{borderLeftColor:'var(--text)'},'.cm-activeLine':{backgroundColor:'color-mix(in srgb, var(--blue, #58a6ff) 8%, transparent)'},'.cm-activeLineGutter':{backgroundColor:'var(--hover)'},'&.cm-focused .cm-selectionBackground, .cm-selectionBackground':{backgroundColor:'color-mix(in srgb, var(--blue, #58a6ff) 24%, transparent)'}},{dark}),...(dark?[syntaxHighlighting(oneDarkHighlightStyle)]:[])])})
  },[dark,path,label])
  useEffect(()=>{
    const view=editor.current;if(!view)return
    let canceled=false
    const language=LanguageDescription.matchFilename(languages,path)
    if(language)void language.load().then(support=>{if(!canceled&&editor.current===view)view.dispatch({effects:compartments.current.language.reconfigure(support)})}).catch(()=>{/* Editing remains usable as plain text if a parser cannot load. */})
    return()=>{canceled=true}
  },[path,label])
  useEffect(()=>{
    const view=editor.current;if(!view||!revealLine)return
    const line=view.state.doc.line(Math.max(1,Math.min(revealLine,view.state.doc.lines)))
    view.dispatch({effects:EditorView.scrollIntoView(line.from,{y:'center'})})
  },[revealKey,path,label])
  return <div ref={parent} className="code-editor" />
}
