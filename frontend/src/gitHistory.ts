import type { RepositoryCommit } from './services'
export interface HistoryQuery { revision:string; query:string; search:'message'|'author'|'sha'; path:string; cursor:string; limit:number }
export interface HistoryPage { commits:RepositoryCommit[]; nextCursor:string }
export interface RevisionFile { path:string; sha:string; content:string; binary:boolean }
export interface BlameLine { sha:string; originalLine:number; line:number; text:string; author:string; createdAt:string; summary:string }
