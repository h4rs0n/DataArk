import type { ApiEnvelope } from '@/api/types'
import type { ArchiveTask } from '@/api/archive'
export interface SearchArchiveAction {
  status: 'submitting' | 'queued' | 'success' | 'failed'
  error?: string
}
export function submitSearchArchive(
  actions: Record<string, SearchArchiveAction>,
  item: { id: string; link: string },
  submit: (url: string) => Promise<ApiEnvelope<ArchiveTask>>,
): Promise<string | null>
