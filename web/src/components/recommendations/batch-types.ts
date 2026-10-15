export interface BatchRunSummary {
  id: number
  active: boolean
  status: string
  pausedAt?: string
  errorCode?: number
  error?: string
  model: string
  createdAt: string
  finishedAt?: string
  counts: { total: number; returned?: number; pending: number; processing: number; succeeded: number; failed: number; reused: number; skipped: number }
}

export interface BatchCleanupPause {
  runId: number
  errorCode: number
  error: string
  pausedAt: string
}
