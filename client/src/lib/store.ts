import { create } from 'zustand'
import { immer } from 'zustand/middleware/immer'
import { persist } from 'zustand/middleware'
import type { AppState, AppSettings, Theme, ViewMode, SortOrder } from '@/types'

interface AppStore extends AppState {
  // Actions
  setSelectedFiles: (files: Set<string>) => void
  addSelectedFile: (file: string) => void
  removeSelectedFile: (file: string) => void
  clearSelectedFiles: () => void
  setCurrentPath: (path: string) => void
  setLoading: (loading: boolean) => void
  setError: (error: string | null) => void
  
  // Settings
  settings: AppSettings
  updateSettings: (settings: Partial<AppSettings>) => void
  setTheme: (theme: Theme) => void
  setViewMode: (mode: ViewMode) => void
  setSortBy: (sortBy: SortOrder) => void
  setSortOrder: (order: 'asc' | 'desc') => void
  
  // UI State
  sidebarOpen: boolean
  setSidebarOpen: (open: boolean) => void
  searchQuery: string
  setSearchQuery: (query: string) => void
  filterType: 'all' | 'video' | 'image'
  setFilterType: (type: 'all' | 'video' | 'image') => void
}

const defaultSettings: AppSettings = {
  theme: 'system',
  viewMode: 'grid',
  sortBy: 'name',
  sortOrder: 'asc',
  autoPlay: false,
  showThumbnails: true,
  gridSize: 'medium',
}

export const useAppStore = create<AppStore>()(
  persist(
    immer((set) => ({
      // Initial state
      selectedFiles: new Set(),
      currentPath: '',
      isLoading: false,
      error: null,
      settings: defaultSettings,
      sidebarOpen: typeof window !== 'undefined' ? window.innerWidth >= 1024 : true,
      searchQuery: '',
      filterType: 'all',
      
      // Actions
      setSelectedFiles: (files) => set((state) => {
        state.selectedFiles = files
      }),
      
      addSelectedFile: (file) => set((state) => {
        state.selectedFiles.add(file)
      }),
      
      removeSelectedFile: (file) => set((state) => {
        state.selectedFiles.delete(file)
      }),
      
      clearSelectedFiles: () => set((state) => {
        state.selectedFiles.clear()
      }),
      
      setCurrentPath: (path) => set((state) => {
        state.currentPath = path
      }),
      
      setLoading: (loading) => set((state) => {
        state.isLoading = loading
      }),
      
      setError: (error) => set((state) => {
        state.error = error
      }),
      
      // Settings actions
      updateSettings: (newSettings) => set((state) => {
        state.settings = { ...state.settings, ...newSettings }
      }),
      
      setTheme: (theme) => set((state) => {
        state.settings.theme = theme
      }),
      
      setViewMode: (mode) => set((state) => {
        state.settings.viewMode = mode
      }),
      
      setSortBy: (sortBy) => set((state) => {
        state.settings.sortBy = sortBy
      }),
      
      setSortOrder: (order) => set((state) => {
        state.settings.sortOrder = order
      }),
      
      // UI actions
      setSidebarOpen: (open) => set((state) => {
        state.sidebarOpen = open
      }),
      
      setSearchQuery: (query) => set((state) => {
        state.searchQuery = query
      }),
      
      setFilterType: (type) => set((state) => {
        state.filterType = type
      }),
    })),
    {
      name: 'drivelocal-store',
      version: 2,
      migrate: (persisted) => {
        const prev = persisted as { settings?: AppSettings } | undefined
        return { settings: prev?.settings ?? defaultSettings }
      },
      partialize: (state) => ({
        settings: state.settings,
      }),
    }
  )
)

// Selectors
export const useSelectedFiles = () => useAppStore((state) => state.selectedFiles)
export const useCurrentPath = () => useAppStore((state) => state.currentPath)
export const useIsLoading = () => useAppStore((state) => state.isLoading)
export const useError = () => useAppStore((state) => state.error)
export const useSettings = () => useAppStore((state) => state.settings)
export const useSidebarOpen = () => useAppStore((state) => state.sidebarOpen)
export const useSearchQuery = () => useAppStore((state) => state.searchQuery)
export const useFilterType = () => useAppStore((state) => state.filterType)

// Actions selectors
export const useAppActions = () => useAppStore((state) => ({
  setSelectedFiles: state.setSelectedFiles,
  addSelectedFile: state.addSelectedFile,
  removeSelectedFile: state.removeSelectedFile,
  clearSelectedFiles: state.clearSelectedFiles,
  setCurrentPath: state.setCurrentPath,
  setLoading: state.setLoading,
  setError: state.setError,
  updateSettings: state.updateSettings,
  setTheme: state.setTheme,
  setViewMode: state.setViewMode,
  setSortBy: state.setSortBy,
  setSortOrder: state.setSortOrder,
  setSidebarOpen: state.setSidebarOpen,
  setSearchQuery: state.setSearchQuery,
  setFilterType: state.setFilterType,
}))
