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
export interface RebaseRequest { preview:RebasePreviewRequest; token:string; steps:RebaseStep[];stackToken?:string }
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
export interface BisectRequest { action:'start'|'good'|'bad'|'skip'|'checkout'|'reset';good:string;bad:string;expectedHead:string;expectedBranch:string;token:string }
export interface FlowConfig { main:string;develop:string;featurePrefix:string;releasePrefix:string;hotfixPrefix:string }
export interface FlowSession { kind:'feature'|'release'|'hotfix';source:string;sourceSha:string;config:FlowConfig;mainSha:string;developSha:string;tag:string;message:string;deleteBranch:boolean;stage:number;mergeTarget:string;mergeBefore:string;mergeSource:string }
export interface FlowState { config:FlowConfig;configured:boolean;kind:string;session?:FlowSession;token:string }
export interface FlowRequest { action:'configure'|'start'|'prepare'|'advance'|'cancel';config:FlowConfig;kind:string;name:string;tag:string;message:string;deleteBranch:boolean;expectedHead:string;expectedBranch:string;token:string }
export interface CustomCommand { id:string;name:string;executable:string;arguments:string[];timeoutSeconds:number }
export interface CommandRun { id:string;root:string;name:string;status:'running'|'completed'|'failed'|'canceled';stdout:string;stderr:string;truncated:boolean;exitCode:number;error:string;startedAt:string;finishedAt:string }
export interface Terminal { id:string;name:string;available:boolean }
export interface StackBranch { name:string;sha:string;parent:string;parentSha:string;baseSha:string;current:boolean;needsRestack:boolean;error:string }
export interface BranchStacks { branches:StackBranch[];token:string }
export interface StackBranchRequest { action:'create'|'link'|'unlink';name:string;parent:string;baseSha:string;expectedHead:string;expectedBranch:string;token:string }
export interface StackPushRequest { branch:string;remote:string;expectedHead:string;expectedBranch:string;token:string }
export interface StackPushPreview { branch:string;localSha:string;remoteSha:string;url:string;rewrites:boolean;token:string }
export interface PRStackNode { number:number;title:string;head:string;base:string;headSha:string;baseSha:string;url:string;status:string;parents:number[];depth:number }
export interface PullRequestStack { nodes:PRStackNode[];warnings:string[] }
