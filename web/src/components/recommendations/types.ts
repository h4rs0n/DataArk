// 推荐中心各面板共用的数据结构。

export type FeedbackAction =
  | 'valuable'
  | 'not_interested'
  | 'too_repetitive'
  | 'low_value'
  | 'deep_read'
  | 'block_source'
  | 'reduce_topic'
  | 'reduce_style'

export type BlockRuleType = 'topic' | 'source' | 'style'

export interface ArchiveRankingItem {
  path: string
  domain: string
  fileName: string
  title: string
  summary: string
  clickCount: number
}

export interface ArchiveRecommendationItem extends ArchiveRankingItem {
  score: number
  reason: string
}

export interface KeywordItem {
  keyword: string
  count: number
  resultCount: number
}

export interface DiscoverySource {
  id: number
  name: string
  url: string
  type: string
  enabled: boolean
  lastError: string
  siteId?: number
  endpointType?: string
  nextDueAt?: string
  nextFetchAt?: string
  lastSuccessAt?: string
  userManaged?: boolean
}

export interface DiscoveryCandidate {
  id: number
  sourceName: string
  url: string
  title: string
  summary: string
  status: string
  score: number
  topics?: string
  contentType?: string
  contentStyle?: string
  enrichmentStatus?: string
  publishedAt?: string
  processingState?: string
  eligibilityState?: string
  dedupeState?: string
  assessmentState?: string
  contentVersion?: number
  userState?: { currentFeedback?: string; openedAt?: string; archivedAt?: string }
}

export interface RecommendationDay {
  id: number
  userId: number
  date: string
  timezone: string
  status: string
  requestedCount: number
  actualCount: number
  generatedAt?: string
  shortageReasons?: string
  degraded?: boolean
  degradationReason?: string
}

export interface RecommendationItem {
  id: number
  candidateId: number
  rank: number
  reason: string
  rerankScore: number
  poolType?: string
  explorationReason?: string
  snapshotTitle?: string
  candidate: DiscoveryCandidate
}

export interface RecommendationSnapshot {
  day: RecommendationDay
  items: RecommendationItem[]
}

export interface TodayDigestSummary {
  date: string
  available: boolean
  overview: string
  highlights: string[]
  topics: string[]
  model: string
  promptVersion: string
  generatedAt?: string
  reason?: string
}

export interface RecommendationFeedBatch {
  id: number
  userId: number
  status: string
  requestedCount: number
  actualCount: number
  policyVersion: string
  profileVersion: number
  shortageReasons: string
  createdAt: string
}

export interface RecommendationFeedSnapshot {
  batch: RecommendationFeedBatch | null
  items: RecommendationItem[]
}

export interface RecommendationSettings {
  dailyLimit: number
  timezone: string
  generationTime: string
  candidateWindowDays: number
  explorationRate: number
  preferredTopics: string
  preferredLanguages: string
  preferredLength: string
  preferredDepth: number
  favoriteSources: string
  enabled: boolean
}

export interface BlockRule {
  id: number
  type: BlockRuleType
  value: string
  active: boolean
}

export interface BlockTarget {
  type: BlockRuleType
  value: string
}

export type CrawlTaskStatus = 'pending' | 'running' | 'succeeded' | 'failed'

export interface CrawlQueueTask {
  id: string
  kind: string
  targetType: string
  targetId: number
  contentVersion?: string
  status: CrawlTaskStatus
  attempts: number
  scheduledAt?: string
  startedAt?: string
  finishedAt?: string
  error?: string
  createdAt: string
}

export interface CrawlQueueSnapshot {
  mode: 'automatic' | 'manual'
  state: 'idle' | 'waiting' | 'running'
  canRun: boolean
  updatedAt: string
  counts: { pending: number; running: number; succeeded24h: number; failed24h: number }
  tasks: CrawlQueueTask[]
}

export interface DomainBlacklistEntry {
  id: number
  domain: string
  reason: string
  createdAt: string
  updatedAt: string
}

export interface DomainBlacklistMutation {
  entry: DomainBlacklistEntry
  affectedCandidates: number
}

export type FeedbackByItem = Record<number, { action: string } | undefined>

export function emptyCrawlQueue(): CrawlQueueSnapshot {
  return {
    mode: 'automatic',
    state: 'idle',
    canRun: false,
    updatedAt: '',
    counts: { pending: 0, running: 0, succeeded24h: 0, failed24h: 0 },
    tasks: [],
  }
}

export function emptySnapshot(date: string): RecommendationSnapshot {
  return {
    day: { id: 0, userId: 0, date, timezone: 'Asia/Shanghai', status: 'missing', requestedCount: 10, actualCount: 0 },
    items: [],
  }
}
