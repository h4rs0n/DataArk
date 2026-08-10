export const PROCESS_CANDIDATE_KIND = 'discovery_process_candidate'
export const BACKFILL_SITE_KIND = 'discovery_backfill_site'
export const CRAWL_TASK_GROUP_THRESHOLD = 3
export const CRAWL_TASK_DETAIL_LIMIT = 1
export const CRAWL_TASK_GROUP_WINDOW_MS = 10 * 60 * 1000

function isGroupableTask(task) {
  if (task.kind === PROCESS_CANDIDATE_KIND) return true
  return task.kind === BACKFILL_SITE_KIND && task.status === 'succeeded'
}

function effectiveTaskTimestamp(task) {
  const value = task.finishedAt || task.startedAt || task.scheduledAt || task.createdAt
  const timestamp = Date.parse(value || '')
  return Number.isFinite(timestamp) ? timestamp : null
}

function taskGroupSignature(task) {
  return JSON.stringify([
    task.status,
    task.attempts,
    task.contentVersion || '',
    task.error || '',
  ])
}

function taskDisplayItem(task) {
  return { type: 'task', key: `task:${task.id}`, task }
}

export function groupCrawlQueueTasks(tasks) {
  const source = Array.isArray(tasks) ? tasks : []
  const bucketsBySignature = new Map()
  const bucketByIndex = new Map()
  const buckets = []

  source.forEach((task, index) => {
    if (!isGroupableTask(task)) return
    const timestamp = effectiveTaskTimestamp(task)
    if (timestamp === null) return

    const signature = taskGroupSignature(task)
    const signatureBuckets = bucketsBySignature.get(signature) || []
    let bucket = signatureBuckets.find((candidate) => {
      const minimum = Math.min(candidate.minimum, timestamp)
      const maximum = Math.max(candidate.maximum, timestamp)
      return maximum - minimum <= CRAWL_TASK_GROUP_WINDOW_MS
    })
    if (!bucket) {
      bucket = { signature, minimum: timestamp, maximum: timestamp, members: [] }
      signatureBuckets.push(bucket)
      bucketsBySignature.set(signature, signatureBuckets)
      buckets.push(bucket)
    }
    bucket.minimum = Math.min(bucket.minimum, timestamp)
    bucket.maximum = Math.max(bucket.maximum, timestamp)
    bucket.members.push({ index, task, timestamp })
    bucketByIndex.set(index, bucket)
  })

  const condensedBuckets = new Map()
  for (const bucket of buckets) {
    if (bucket.members.length <= CRAWL_TASK_GROUP_THRESHOLD) continue
    const newestFirst = [...bucket.members].sort((left, right) => right.timestamp - left.timestamp || left.index - right.index)
    const detailIndexes = new Set(newestFirst.slice(0, CRAWL_TASK_DETAIL_LIMIT).map((member) => member.index))
    const hiddenMembers = newestFirst.slice(CRAWL_TASK_DETAIL_LIMIT)
    const summaryAfterIndex = Math.max(...detailIndexes)
    condensedBuckets.set(bucket, { newestFirst, detailIndexes, hiddenMembers, summaryAfterIndex })
  }

  const displayItems = []
  source.forEach((task, index) => {
    const bucket = bucketByIndex.get(index)
    const condensed = bucket ? condensedBuckets.get(bucket) : undefined
    if (!bucket || !condensed) {
      displayItems.push(taskDisplayItem(task))
      return
    }
    if (condensed.detailIndexes.has(index)) {
      displayItems.push(taskDisplayItem(task))
      if (index === condensed.summaryAfterIndex) {
        const representative = condensed.newestFirst[0].task
        displayItems.push({
          type: 'summary',
          key: `summary:${bucket.signature}:${bucket.maximum}:${condensed.summaryAfterIndex}`,
          kind: representative.kind,
          status: representative.status,
          attempts: representative.attempts,
          contentVersion: representative.contentVersion,
          error: representative.error,
          collapsedCount: condensed.hiddenMembers.length,
          windowStart: new Date(bucket.minimum).toISOString(),
          windowEnd: new Date(bucket.maximum).toISOString(),
        })
      }
      return
    }
  })
  return displayItems
}
