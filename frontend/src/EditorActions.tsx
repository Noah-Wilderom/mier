import { useEffect, useRef, useState } from 'react'
import { Icon } from './Icon'
import { hasNativeBackend, services } from './services'
import { openWebURL } from './native'
import './EditorActions.css'

const editors = [{id:'vscode',name:'VS Code'},{id:'phpstorm',name:'PhpStorm'},{id:'zed',name:'Zed'}] as const

export default function EditorActions({root,githubURL,notify}: {root?:string;githubURL?:string;notify:(message:string)=>void}) {
  const [editor,setEditor] = useState('vscode')
  const [menu,setMenu] = useState(false)
  const [launching,setLaunching] = useState(false)
  const pending = useRef(false)
  const anchor = useRef<HTMLDivElement>(null)
  const chooser = useRef<HTMLButtonElement>(null)
  const native = hasNativeBackend()
  const chosen = editors.find(item=>item.id===editor) ?? editors[0]
  useEffect(()=>{
    if(native)void services.editor.preferredEditor().then(value=>{if(editors.some(item=>item.id===value))setEditor(value)}).catch(()=>{})
  },[native])
  useEffect(()=>{
    if(!menu)return
    anchor.current?.querySelector<HTMLButtonElement>('[role="menuitemradio"][aria-checked="true"]')?.focus()
    const outside=(event:PointerEvent)=>{if(!anchor.current?.contains(event.target as Node))setMenu(false)}
    window.addEventListener('pointerdown',outside)
    return()=>window.removeEventListener('pointerdown',outside)
  },[menu])
  const launch=async()=>{
    if(pending.current||!root||!native)return
    pending.current=true;setLaunching(true)
    try{await services.editor.openRepository(root,editor)}catch(issue){notify(issue instanceof Error?issue.message:String(issue))}finally{pending.current=false;setLaunching(false)}
  }
  return <div className="repository-open-actions">
    <div className="editor-open-split" ref={anchor}>
      <button className="editor-open-primary" disabled={!native||!root||launching} title={!native?'Available in the desktop app':`Open repository in ${chosen.name}`} onClick={()=>void launch()}><img className="editor-logo" src={`/editors/${editor}.${editor==='zed'?'png':'svg'}`} alt="" aria-hidden="true" width={17} height={17}/><span>{launching?'Opening…':`Open in ${chosen.name}`}</span></button>
      <button className="editor-open-chooser" ref={chooser} aria-label="Choose editor" aria-haspopup="menu" aria-expanded={menu} disabled={launching} onClick={()=>setMenu(value=>!value)}><Icon name="chevron-down" size={13}/></button>
      {menu&&<div className="editor-open-menu" role="menu" aria-label="Supported editors" onKeyDown={event=>{
        if(event.key==='Escape'){event.preventDefault();event.stopPropagation();setMenu(false);chooser.current?.focus()}
        if(['ArrowDown','ArrowUp','Home','End'].includes(event.key)){event.preventDefault();const options=Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]'));const current=options.indexOf(document.activeElement as HTMLButtonElement);options[event.key==='Home'?0:event.key==='End'?options.length-1:(current+(event.key==='ArrowDown'?1:-1)+options.length)%options.length]?.focus()}
      }}><span className="editor-open-menu-label">Open repository in</span>{editors.map(item=><button key={item.id} role="menuitemradio" aria-checked={editor===item.id} onClick={()=>{setEditor(item.id);setMenu(false);chooser.current?.focus()}}><img className="editor-logo" src={`/editors/${item.id}.${item.id==='zed'?'png':'svg'}`} alt="" aria-hidden="true" width={19} height={19}/>{item.name}{editor===item.id&&<Icon name="check" size={14} className="text-green"/>}</button>)}</div>}
    </div>
    <button className="btn repository-open-github" aria-label="Open in GitHub" title="Open in GitHub" disabled={!githubURL} onClick={()=>{if(githubURL)void openWebURL(githubURL).catch(issue=>notify(issue instanceof Error?issue.message:String(issue)))}}><Icon name="github" size={16}/><span>Open in GitHub</span></button>
  </div>
}
