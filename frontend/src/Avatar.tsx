import { useEffect, useState, type CSSProperties } from 'react'
import { people, type UserId } from './data'
import './Avatar.css'

export function personLabel(user: UserId): string {
  return (people[user]?.name || user).replace(/\s*\[bot\]/gi,'').trim() || 'Bot'
}
export function personIsBot(user: UserId): boolean {
  return !!people[user]?.bot || /\[bot\]/i.test(user) || /\[bot\]/i.test(people[user]?.name || '')
}
function imageURL(value?: string): string | undefined {
  if(!value) return undefined
  try {const url=new URL(value);return ['https:','http:'].includes(url.protocol)&&!url.username&&!url.password?url.href:undefined} catch {return undefined}
}

export function Avatar({user,size=28}: {user:UserId;size?:number}) {
  const person=people[user]
  const source=imageURL(person?.avatarUrl)
  const [failed,setFailed]=useState(false)
  useEffect(()=>setFailed(false),[source])
  const label=personLabel(user)
  return <span className="avatar profile-avatar" role="img" aria-label={`${label} profile picture`} title={label} style={{'--avatar-color':person?.color??'var(--muted)',width:size,height:size} as CSSProperties}>
    {source&&!failed?<img src={source} alt="" referrerPolicy="no-referrer" loading="lazy" decoding="async" onError={()=>setFailed(true)}/>:person?.initials??Array.from(label).slice(0,2).join('').toUpperCase()}
  </span>
}

export function ActorName({user,author,compact=false}: {user:UserId;author?:UserId;compact?:boolean}) {
  const label=personLabel(user)
  return <span className="actor-name"><span>{compact?label.split(' ')[0]:label}</span>{personIsBot(user)&&<span className="actor-badge bot-badge">Bot</span>}{author?.toLowerCase()===user.toLowerCase()&&<span className="actor-badge author-badge">Author</span>}</span>
}
