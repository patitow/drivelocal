import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { encodeMediaPath } from '@/lib/mediaPath'
import type { MediaFile, DirectoryInfo } from '@/types'

// Query keys
export const mediaKeys = {
  all: ['media'] as const,
  files: (path: string) => [...mediaKeys.all, 'files', path] as const,
  health: () => [...mediaKeys.all, 'health'] as const,
  network: () => [...mediaKeys.all, 'network'] as const,
}

// Hook para buscar arquivos
export function useMediaFiles(path: string = '') {
  return useQuery({
    queryKey: mediaKeys.files(path),
    queryFn: async (): Promise<DirectoryInfo> => {
      const response = await api.get<DirectoryInfo>('/api/files', {
        params: { path: path.replace(/\\/g, '/') }
      })
      return response
    },
    staleTime: 30 * 1000, // 30 seconds
    gcTime: 5 * 60 * 1000, // 5 minutes
  })
}

// Hook para health check
export function useHealthCheck() {
  return useQuery({
    queryKey: mediaKeys.health(),
    queryFn: async () => {
      const response = await api.get('/api/health')
      return response
    },
    refetchInterval: 30 * 1000, // 30 seconds
    retry: 3,
  })
}

// Hook para network info
export function useNetworkInfo() {
  return useQuery({
    queryKey: mediaKeys.network(),
    queryFn: async () => {
      const response = await api.get('/api/network-info')
      return response
    },
    staleTime: 5 * 60 * 1000, // 5 minutes
  })
}

// Hook para download múltiplo
export function useDownloadMultiple() {
  const queryClient = useQueryClient()
  
  return useMutation({
    mutationFn: async (files: string[]) => {
      const response = await api.post('/api/download-multiple', { files }, {
        responseType: 'blob'
      })
      return response
    },
    onSuccess: () => {
      // Invalidate files query to refresh the list
      queryClient.invalidateQueries({ queryKey: mediaKeys.all })
    },
  })
}

// Hook para download único
export function useDownloadSingle() {
  return useMutation({
    mutationFn: async (filePath: string) => {
      const response = await api.get(`/api/download/${encodeMediaPath(filePath)}`, {
        responseType: 'blob'
      })
      return response
    },
  })
}

// Hook para buscar arquivos com filtros
export function useFilteredFiles(path: string = '', filters?: {
  type?: 'all' | 'video' | 'image'
  search?: string
  sortBy?: 'name' | 'size' | 'date'
  sortOrder?: 'asc' | 'desc'
}) {
  return useQuery({
    queryKey: [...mediaKeys.files(path), filters],
    queryFn: async (): Promise<MediaFile[]> => {
      const response = await api.get<DirectoryInfo>('/api/files', {
        params: { path: path.replace(/\\/g, '/') }
      })
      
      let files = response.items
      
      // Apply filters
      if (filters?.type && filters.type !== 'all') {
        files = files.filter((file: MediaFile) => {
          if (filters.type === 'video') return file.isVideo
          if (filters.type === 'image') return file.isImage
          return true
        })
      }
      
      if (filters?.search) {
        const searchLower = filters.search.toLowerCase()
        files = files.filter((file: MediaFile) => 
          file.name.toLowerCase().includes(searchLower)
        )
      }
      
      // Apply sorting
      if (filters?.sortBy) {
        files.sort((a: MediaFile, b: MediaFile) => {
          let aVal: string | number = ''
          let bVal: string | number = ''
          
          switch (filters.sortBy) {
            case 'name':
              aVal = a.name.toLowerCase()
              bVal = b.name.toLowerCase()
              break
            case 'size':
              aVal = a.size || 0
              bVal = b.size || 0
              break
            case 'date':
              aVal = new Date(a.modifiedAt || '').getTime()
              bVal = new Date(b.modifiedAt || '').getTime()
              break
          }
          
          if (aVal < bVal) return filters.sortOrder === 'desc' ? 1 : -1
          if (aVal > bVal) return filters.sortOrder === 'desc' ? -1 : 1
          return 0
        })
      }
      
      return files
    },
    enabled: !!path,
    staleTime: 30 * 1000,
  })
}
