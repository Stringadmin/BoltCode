export interface Message {
  role: 'user' | 'assistant' | 'tool' | 'error'
  text: string
  detail?: string
  failed?: boolean
  output?: string
  progress?: boolean
  durationMs?: number
  references?: string[]
  projectRules?: string[]
  workspaceMemory?: WorkspaceMemory
  turnId?: string
  undoable?: boolean
  undone?: boolean
}

export interface Chat {
  id: string
  title: string
  base: string
  branch: string
  merged: boolean
  archived?: boolean
  runState?: 'running' | 'interrupted' | 'failed' | 'done'
  runMode?: string
  messages: Message[] | null
}

export interface Approval {
  id: string
  command: string
}

export interface Model {
  id: string
  channelId?: string
  label: string
  provider: string
  name: string
  baseURL: string
  ready: boolean
  hasKey: boolean
  reasoningEffort?: 'auto' | 'low' | 'medium' | 'high' | 'xhigh'
  reasoningSupported?: boolean
}

export type ReasoningEffort = NonNullable<Model['reasoningEffort']>

export interface ModelChannel {
  id: string
  label: string
  provider: string
  baseURL: string
  hasKey: boolean
}

export interface Provider {
  id: string
  label: string
  baseURL: string
  custom: boolean
}

export interface Repository {
  path: string
  name: string
}

export interface State {
  repository: string
  name: string
  chats: Chat[]
  running: string
  checking: string
  projectChecks: ProjectChecks
  approval?: Approval
  model: Model
  models: Model[]
  modelChannels: ModelChannel[]
  fullAccess: boolean
  recent: Repository[]
  archivedProjects: Repository[]
  providers: Provider[]
}

export interface ReviewFile {
  path: string
  added: number
  deleted: number
  binary?: boolean
}

export interface Review {
  token: string
  files: ReviewFile[]
  base: string
  verification: Verification
}

export interface ReviewOpening extends Review {
  selected: string
  patch: string
  patchError?: string
}

export interface ProjectChecks {
  test: string
  typecheck: string
}

export interface WorkspaceMemory {
  facts?: { key: string; value: string; source: string; version: string; firstSeen?: string }[]
  checks?: { id: string; tree: string; finishedAt: string }[]
  toolLessons?: { tool: string; kind: string; field?: string; type?: string }[]
}

export interface CheckResult {
  command: string
  status: string
  exitCode?: number
  durationMs: number
  finishedAt: string
  output: string
}

export interface Verification {
  checks: { id: string; command: string; status: string; result?: CheckResult }[]
  running: boolean
  changed: boolean
}

export interface UndoPreview {
  files: { path: string; action: string; block?: string }[]
  allowed: boolean
  alreadyRestored: boolean
}

export interface TurnEvent {
  chatId: string
  kind: 'started' | 'finished' | 'phase' | 'text_delta' | 'text_reset' | 'text' | 'tool_started' | 'tool_finished' | 'confirm' | 'confirmed' |
    'checks_started' | 'checks_finished'
  text?: string
  detail?: string
  failed?: boolean
  output?: string
  progress?: boolean
  durationMs?: number
  prompt?: Approval
  projectRules?: string[]
  workspaceMemory?: WorkspaceMemory
}

interface Runtime {
  Call: { ByName(name: string, ...args: unknown[]): Promise<unknown> }
  Events: { On(name: string, listener: (event: { data: TurnEvent }) => void): () => void }
  Clipboard: { SetText(text: string): Promise<void> }
  Window: {
    Close(): Promise<void>
    Minimise(): Promise<void>
    ToggleMaximise(): Promise<void>
    IsMaximised(): Promise<boolean>
  }
}

const runtimePath = '/wails/runtime.js'
let runtime: Promise<Runtime> | undefined

export function desktopRuntime(): Promise<Runtime> {
  runtime ??= import(/* @vite-ignore */ runtimePath) as Promise<Runtime>
  return runtime
}

export async function native<T>(method: string, ...args: unknown[]): Promise<T> {
  const { Call } = await desktopRuntime()
  return Call.ByName('github.com/Stringadmin/BoltCode/desktop/workbench.Service.' + method, ...args) as Promise<T>
}

export async function subscribe(listener: (event: TurnEvent) => void) {
  const { Events } = await desktopRuntime()
  return Events.On('boltcode:turn', ({ data }) => listener(data))
}

export async function copyText(text: string) {
  const { Clipboard } = await desktopRuntime()
  await Clipboard.SetText(text)
}
