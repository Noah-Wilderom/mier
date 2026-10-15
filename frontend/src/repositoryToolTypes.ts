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
export interface RebasePreviewRequest { onto:string; upstream:string; expectedHead:string; expectedBranch:string }
export interface RebasePreview { token:string; onto:string; upstream:string; head:string; branch:string; commits:import('./services').RepositoryCommit[] }
export interface RebaseStep { sha:string; action:'pick'|'reword'|'edit'|'squash'|'fixup'|'drop'; message:string }
export interface RebaseRequest { preview:RebasePreviewRequest; token:string; steps:RebaseStep[] }
export interface Worktree { path:string;sha:string;branch:string;token:string;primary:boolean;current:boolean;locked:boolean;lockReason:string;prunable:boolean;changes:number;error:string }
export interface WorktreeRequest { action:'add'|'lock'|'unlock'|'remove';path:string;mode:'new'|'existing'|'detached';branch:string;startSha:string;expectedHead:string;expectedBranch:string;token:string;reason:string }
export interface ExportPatchRequest { mode:'working'|'staged'|'commit';sha:string }
export interface ApplyPatchRequest { patch:string;mode:'working'|'index'|'commits';reverse:boolean;expectedHead:string;expectedBranch:string;token:string }
export interface PatchPreview { token:string;stat:string;paths:string[] }
export interface Submodule { name:string;path:string;url:string;recordedSha:string;currentSha:string;branch:string;initialized:boolean;changes:number;token:string;error:string }
export interface SubmoduleRequest { action:'add'|'update'|'sync'|'set-url'|'deinit';path:string;url:string;token:string;expectedHead:string;expectedBranch:string }
export interface LFSPattern { pattern:string;source:string;lockable:boolean }
export interface LFSFile { name:string;oid:string;size:number;downloaded:boolean }
export interface LFSState { installed:boolean;version:string;initialized:boolean;patterns:LFSPattern[];files:LFSFile[];summary:string;token:string }
export interface LFSRequest { action:'install'|'track'|'untrack'|'pull'|'fetch'|'checkout'|'lock'|'unlock';pattern:string;filename:boolean;lockable:boolean;remote:string;expectedHead:string;expectedBranch:string;token:string }
export interface LFSLock { id:string;path:string;owner:{name:string};locked_at:string }
