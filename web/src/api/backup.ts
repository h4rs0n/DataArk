import { requestBlob, requestJSON } from '@/api/client'

export interface RestoreResult {
  meiliDumpFile: string
  databaseRestored: boolean
  archiveRestored: boolean
  indexedDocuments: number
  refreshedStatRows: number
}

function fallbackBackupFileName(): string {
  return `dataark-backup-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}.zip`
}

export async function downloadBackup(): Promise<{ blob: Blob; fileName: string }> {
  const { blob, fileName } = await requestBlob('/api/backup', { method: 'POST' })
  return {
    blob,
    fileName: fileName || fallbackBackupFileName(),
  }
}

export function restoreBackup(file: File): Promise<RestoreResult> {
  const formData = new FormData()
  formData.append('file', file)
  return requestJSON<RestoreResult>('/api/backup/restore', {
    method: 'POST',
    body: formData,
  })
}
