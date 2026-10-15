export interface Tag { name:string; object:string; commit:string; message:string; createdAt:string; annotated:boolean }
export interface TagRequest { action:'create'|'rename'|'delete'|'push'; name:string; newName:string; sha:string; message:string; expectedObject:string; remote:string }
export interface RemoteRequest { action:'add'|'url'|'push-url'|'rename'|'remove'; name:string; newName:string; url:string; expectedFetchUrl:string; expectedPushUrl:string }
export interface SigningConfig { enabled:boolean; format:'openpgp'|'ssh'|'x509'; key:string }
export interface CommitSignature { status:string; signer:string; key:string }
export interface Checkpoint { ref:string; sha:string; label:string; createdAt:string }
export interface ReflogEntry { sha:string; selector:string; message:string; actor:string }
export interface ResetRequest { sha:string; mode:'soft'|'mixed'|'hard'; expectedHead:string; expectedBranch:string }
export interface RecoverBranchRequest { sha:string; name:string }
export interface CherryPickManyRequest { shas:string[]; expectedHead:string; expectedBranch:string }
