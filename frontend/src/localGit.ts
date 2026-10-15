import type { RepositoryDiff } from './services'
export interface Stash { sha:string; ref:string; message:string; createdAt:string }
export interface SaveStashRequest { message:string; mode:'all'|'keep-index'|'staged'; includeUntracked:boolean; expectedHead:string }
export interface StashRequest { sha:string; action:'apply'|'pop'|'drop'; restoreIndex:boolean; expectedHead:string }
export interface AmendRequest { message:string; expectedHead:string }
export interface DiscardPreview { path:string; token:string; diff:RepositoryDiff; untracked:boolean }
export interface DiscardRequest { path:string; token:string; hunk?:number; lines?:number[] }
export interface DiscardBackup { id:string; path:string; createdAt:string; afterToken:string }
