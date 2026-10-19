// File and Media Types
export interface MediaFile {
  name: string
  path: string
  isDirectory: boolean
  isVideo: boolean
  isImage: boolean
  size?: number
  createdAt?: string
  modifiedAt?: string
  mimeType?: string
  fileCount?: number
  folderCount?: number
}

export interface DirectoryInfo {
  currentPath: string
  items: MediaFile[]
}

// API Response Types
export interface ApiResponse<T = unknown> {
  data: T
  status: number
  message?: string
}

export interface HealthResponse {
  status: 'ok' | 'error'
  timestamp: string
  mediaPath: string
  mediaPathExists: boolean
  authEnabled?: boolean
  bootId?: string // aleatório por boot do servidor; muda quando o container reinicia
}

// Montagem dinâmica de pastas do host (admin)
export interface MountEntry {
  name: string
  hostPath: string
  target: string // ex.: /data/<nome>
  legacy: boolean // entrada já existente no override antes do app gerenciar
}

export type MountJobState =
  | 'queued'
  | 'applying'
  | 'verifying'
  | 'done'
  | 'rolled_back'
  | 'rollback_failed'

export interface MountJob {
  id: string
  op: 'add' | 'remove'
  name: string
  state: MountJobState
  error?: string
  startedAt: string
  finishedAt?: string
}

export interface MountsResponse {
  helper: { available: boolean; version?: string }
  revision?: string // ausente quando o helper está offline
  mounts: MountEntry[]
  activeJob?: MountJob | null
  containerMounts: string[] // destinos /data/* montados no container (ex.: ["/data/Animes"])
}

export interface BrowseEntry {
  name: string
  path: string
  selectable: boolean
  reason?: string // motivo quando selectable === false
}

export interface BrowseResponse {
  path: string // '' quando lista os drives
  parent: string | null
  entries: BrowseEntry[]
}

export interface MountJobAccepted {
  jobId: string
  revision?: string
}

export interface MountApiError {
  error: string // código, ex.: revision_conflict, busy, already_mounted, name_conflict
  message?: string
}

export interface NetworkInfo {
  addresses: Array<{
    address: string
    port: number
  }>
  port: number
}

// Component Props Types
export interface FileBrowserProps {
  selectedFiles: Set<string>
  setSelectedFiles: (files: Set<string>) => void
  showSnackbar: (message: string, severity?: 'success' | 'error' | 'warning' | 'info') => void
}

export interface VideoPlayerProps {
  filePath: string
}

export interface ImageViewerProps {
  filePath: string
}

// State Management Types
export interface AppState {
  selectedFiles: Set<string>
  currentPath: string
  isLoading: boolean
  error: string | null
}

// Form Types
export interface SearchForm {
  query: string
  type: 'all' | 'video' | 'image'
}

export interface DownloadForm {
  files: string[]
  format: 'zip' | 'tar'
}

// Utility Types
export type FileType = 'video' | 'image' | 'directory' | 'unknown'

export type SortOrder = 'name' | 'size' | 'date' | 'type'

export type ViewMode = 'grid' | 'list' | 'compact'

// Error Types
export interface AppError {
  code: string
  message: string
  details?: unknown
}

// Theme Types
export type Theme = 'light' | 'dark' | 'system'

export interface ThemeConfig {
  theme: Theme
  primaryColor: string
  accentColor: string
}

// Settings Types
export interface AppSettings {
  theme: Theme
  viewMode: ViewMode
  sortBy: SortOrder
  sortOrder: 'asc' | 'desc'
  autoPlay: boolean
  showThumbnails: boolean
  gridSize: 'small' | 'medium' | 'large'
}

// Authentication Types
export interface LoginRequest {
  username: string
  password: string
  rememberMe?: boolean
}

export interface LoginResponse {
  success: boolean
  username: string
  email?: string
  role?: string
  permissions?: string[]
  expiresAt?: number
  message: string
  token?: string // Token returned for IP access fallback (when cookies don't work)
}

export interface AuthStatus {
  authenticated: boolean
  username: string | null
  email?: string
  role?: string
  permissions?: string[]
  authEnabled: boolean
}

export interface AuthState {
  isAuthenticated: boolean
  token: string | null
  username: string | null
  isLoading: boolean
  error: string | null
}

// User Management Types
export interface User {
  id: string
  username: string
  email: string
  role: string
  permissions: string[]
  createdAt: string
  active: boolean
}

export interface CreateUserRequest {
  username: string
  password: string
  email: string
  role: string
  permissions: string[]
}

export interface UpdateUserRequest {
  email?: string
  role?: string
  permissions?: string[]
}

export interface UsersResponse {
  users: User[]
}
export interface AudioTrack {
  order: number
  lang: string
  title: string
  codec: string
  channels: number
  default: boolean
  playable: boolean
}

export interface SubtitleTrack {
  id: string
  lang: string
  title: string
  codec: string
  external: boolean
  default: boolean
  supported: boolean
  kind?: 'text' | 'ass' | 'pgs' | ''
  // legenda de texto embutida já extraída no servidor
  ready?: boolean
}

export interface FontAttachment {
  index: number
  name: string
}

export type PlaybackMode = 'direct' | 'remux' | 'transcode'

export interface MediaInfo {
  duration: number
  container?: string
  videoIndex?: number
  videoCodec: string
  videoProfile?: string
  pixFmt?: string
  width?: number
  height?: number
  audio: AudioTrack[]
  subtitles: SubtitleTrack[]
  fonts?: FontAttachment[]
  // decisão do servidor para as caps enviadas
  playback?: { mode: PlaybackMode; reason: string }
  chapters?: MediaChapter[]
  // capítulo de abertura (OP), quando o arquivo marca
  intro?: MediaChapter
}

export interface MediaChapter { start: number; end: number; title: string }
