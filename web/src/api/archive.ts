import { ApiResponseError, requestEnvelope, requestJSON } from '@/api/client'
import type { ApiEnvelope } from '@/api/types'

export interface ArchiveStatItem {
  source: string
  fileCount: number
}

export interface ArchiveStats {
  totalFiles: number
  sources: ArchiveStatItem[]
}

export type ArchiveTaskStatus = 'pending' | 'running' | 'success' | 'failed' | string

export interface ArchiveTask {
  id: string
  url: string
  domain: string
  status: ArchiveTaskStatus
  fileName: string
  error: string
  externalTaskId: string
  createdAt: string
  updatedAt: string
  startedAt: string | null
  finishedAt: string | null
}

export interface ConsistencyIssue {
  severity: string
  store: string
  domain: string
  filename: string
  path: string
  documentIds: string[]
  message: string
  recoverable: boolean
}

export interface ConsistencyReport {
  checkedAt: string
  consistent: boolean
  htmlFiles: number
  meiliDocuments: number
  databaseStatTotal: number
  recoverableIssues: ConsistencyIssue[] | null
  unrecoverableIssues: ConsistencyIssue[] | null
  actions: string[] | null
  indexedDocuments: number
  refreshedStatSources: number
}

export function emptyArchiveStats(): ArchiveStats {
  return { totalFiles: 0, sources: [] }
}

export function getArchiveStats(): Promise<ArchiveStats> {
  return requestJSON<ArchiveStats>('/api/archiveStats').then((data) => data ?? emptyArchiveStats())
}

export function refreshArchiveStats(): Promise<ArchiveStats> {
  return requestJSON<ArchiveStats>('/api/archiveStats/refresh', { method: 'POST' }).then((data) => data ?? emptyArchiveStats())
}

export async function getConsistencyReport(): Promise<ConsistencyReport> {
  const data = await requestJSON<ConsistencyReport>('/api/archiveConsistency')
  if (!data) {
    throw new ApiResponseError('一致性响应缺少数据', 200)
  }
  return data
}

export async function repairConsistency(): Promise<ConsistencyReport> {
  const data = await requestJSON<ConsistencyReport>('/api/archiveConsistency/repair', { method: 'POST' })
  if (!data) {
    throw new ApiResponseError('一致性响应缺少数据', 200)
  }
  return data
}

export function archiveByURL(url: string): Promise<ApiEnvelope<ArchiveTask>> {
  return requestEnvelope<ArchiveTask>('/api/archiveByURL', {
    method: 'POST',
    body: JSON.stringify({ url }),
  })
}

export function getArchiveTask(taskId: string): Promise<ApiEnvelope<ArchiveTask>> {
  return requestEnvelope<ArchiveTask>(`/api/archiveTask/${encodeURIComponent(taskId)}`)
}

export function submitArchiveUpload(payload: { domain: string; sourceUrl: string; files: unknown[] }): Promise<unknown> {
  return requestJSON('/api/upload', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function recordArchiveClick(path: string, keyword = ''): Promise<unknown> {
  return requestJSON('/api/archive/clicks', {
    method: 'POST',
    body: JSON.stringify({ path, keyword }),
  })
}

export function deleteArchiveDocument(path: string): Promise<ApiEnvelope<unknown>> {
  return requestEnvelope(`/api/archive?path=${encodeURIComponent(path)}`, { method: 'DELETE' })
}
