export interface PredictionScope {host?:string;owner?:string;repo?:string;root?:string}
export interface PredictionStatus {provider?:'typesafe'|'fake';configured:boolean;enabled:boolean;model:string;error:string}
export interface PredictionRequest {scope:PredictionScope;kind:'review'|'tickets'|'findings'|'conflict'|'ci';pullRequest?:number;headSha?:string;baseSha?:string;runId?:string;path?:string;conflictToken?:string;jobId?:number}
export interface PredictionAnswer {type:'noul'|'choice'|'score';noul?:number;choice?:string;score?:number;confidence?:number;probabilities?:Record<string,number>}
export interface PredictionRow {id:string;label:string;path?:string;line?:number;side?:'old'|'new';findingId?:string;ticketProvider?:string;ticketKey?:string;excerpt?:string;answer:PredictionAnswer;options?:Record<string,string>}
export interface PredictionResult {provider?:'typesafe'|'fake';id:string;kind:PredictionRequest['kind'];model:string;questionVersion:string;headSha?:string;baseSha?:string;rows:PredictionRow[];warnings:string[];usage:{input_tokens:number|null;output_tokens:number|null};durationMs:number;createdAt:string;cached:boolean}
export interface PredictionModel {name:string;description:string;release_date:string}
export interface PredictionFeedback {provider?:'typesafe'|'fake';id:string;kind:string;model:string;useful:boolean;durationMs:number;createdAt:string}
export const PREDICTIONS_CHANGED='mier:predictions-changed'
export function predictionValue(row:PredictionRow):string{
 const a=row.answer
 if(a.type==='choice')return row.options?.[a.choice??'']??a.choice??'Unknown'
 if(a.type==='noul')return a.noul===undefined?'Not reported':`${Math.round(a.noul*100)}% yes probability`
 return a.score===undefined?'Not reported':`${a.score.toFixed(1)} / 2`
}
export function reviewPriority(result:PredictionResult|undefined,path:string):number|undefined{return result?.rows.find(row=>row.path===path&&row.label==='Review attention')?.answer.score}
