export interface CrawlQueueTaskLike {
  id: string
  kind: string
  status: string
  attempts: number
  contentVersion?: string
  error?: string
  scheduledAt?: string
  startedAt?: string
  finishedAt?: string
  createdAt: string
}

export interface CrawlQueueTaskDisplayItem<T extends CrawlQueueTaskLike> {
  type: 'task'
  key: string
  task: T
}

export interface CrawlQueueSummaryDisplayItem<T extends CrawlQueueTaskLike> {
  type: 'summary'
  key: string
  kind: T['kind']
  status: T['status']
  attempts: number
  contentVersion?: string
  error?: string
  collapsedCount: number
  windowStart: string
  windowEnd: string
}

export type CrawlQueueDisplayItem<T extends CrawlQueueTaskLike> = CrawlQueueTaskDisplayItem<T> | CrawlQueueSummaryDisplayItem<T>

export const CRAWL_TASK_GROUP_THRESHOLD: 3
export const CRAWL_TASK_DETAIL_LIMIT: 1
export const CRAWL_TASK_GROUP_WINDOW_MS: number
export function groupCrawlQueueTasks<T extends CrawlQueueTaskLike>(tasks: readonly T[]): CrawlQueueDisplayItem<T>[]
