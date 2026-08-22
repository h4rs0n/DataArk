<template>
  <section class="panel">
    <div class="section-title">
      <div>
        <h2>爬取任务队列</h2>
        <span>文章爬取由后台自动执行；此处只观察任务状态，LLM 评估请到「评估」页手动启动</span>
      </div>
      <a-space wrap>
        <span class="queue-state" :class="`queue-state-${crawlQueue.state}`">{{ crawlQueueStateLabel }}</span>
        <a-button :loading="crawlQueueLoading || blacklistLoading" @click="refreshQueueTab">
          <template #icon><icon-refresh /></template>
          刷新
        </a-button>
      </a-space>
    </div>

    <div class="queue-summary">
      <div><strong>{{ crawlQueue.counts.pending }}</strong><span>等待执行</span></div>
      <div><strong>{{ crawlQueue.counts.running }}</strong><span>正在运行</span></div>
      <div><strong>{{ crawlQueue.counts.succeeded24h }}</strong><span>近 24 小时成功</span></div>
      <div><strong>{{ crawlQueue.counts.failed24h }}</strong><span>近 24 小时失败</span></div>
    </div>

    <a-empty v-if="displayedCrawlQueueTasks.length === 0" description="暂无爬取任务" />
    <div v-else class="queue-task-list">
      <template v-for="item in displayedCrawlQueueTasks" :key="item.key">
        <article v-if="item.type === 'task'" class="queue-task-row">
          <div>
            <strong>{{ crawlTaskKindLabel(item.task.kind) }}</strong>
            <span>{{ crawlTaskTargetLabel(item.task) }}</span>
          </div>
          <span class="task-status" :class="`task-status-${item.task.status}`">{{ crawlTaskStatusLabel(item.task.status) }}</span>
          <span>尝试 {{ item.task.attempts }} 次</span>
          <span>{{ crawlTaskTimeLabel(item.task) }}</span>
          <small v-if="item.task.status === 'failed'" class="queue-task-error">失败原因：{{ item.task.error || '未提供失败原因' }}</small>
          <small v-else-if="item.task.error" class="queue-task-error">{{ item.task.error }}</small>
        </article>
        <article v-else class="queue-task-row queue-task-summary">
          <div>
            <strong>{{ crawlTaskKindLabel(item.kind) }}</strong>
            <span>另有 {{ item.collapsedCount }} 条相同任务已合并<span v-if="item.contentVersion"> · 正文 v{{ item.contentVersion }}</span></span>
          </div>
          <span class="task-status" :class="`task-status-${item.status}`">{{ crawlTaskStatusLabel(item.status) }}</span>
          <span>尝试 {{ item.attempts }} 次</span>
          <span>{{ crawlTaskGroupTimeLabel(item) }}</span>
          <small v-if="item.status === 'failed'" class="queue-task-error">失败原因：{{ item.error || '未提供失败原因' }}</small>
          <small v-else-if="item.error" class="queue-task-error">{{ item.error }}</small>
        </article>
      </template>
    </div>
  </section>

  <section class="panel domain-blacklist-panel">
    <div class="section-title">
      <div>
        <h2>域名黑名单</h2>
        <span>命中域名及其子域不会再被访问，已爬取文章也会从候选列表和后续推荐中剔除</span>
      </div>
    </div>
    <form class="blacklist-form" @submit.prevent="addDomainBlacklist">
      <label class="source-field" for="blacklist-domain">
        <span>域名</span>
        <input id="blacklist-domain" v-model="blacklistForm.domain" name="blacklist-domain" autocomplete="off" placeholder="example.com" />
      </label>
      <label class="source-field" for="blacklist-reason">
        <span>备注（可选）</span>
        <input id="blacklist-reason" v-model="blacklistForm.reason" name="blacklist-reason" autocomplete="off" placeholder="屏蔽原因" />
      </label>
      <a-button type="primary" html-type="submit" :loading="addingBlacklist">添加域名</a-button>
    </form>
    <a-spin :loading="blacklistLoading">
      <a-empty v-if="domainBlacklist.length === 0" description="暂无域名黑名单" />
      <div v-else class="blacklist-list">
        <article v-for="entry in domainBlacklist" :key="entry.id" class="blacklist-row">
          <div><strong>{{ entry.domain }}</strong><span>{{ entry.reason || '未填写备注' }}</span></div>
          <span>同时匹配所有子域 · {{ formatDateTime(entry.createdAt) }}</span>
          <a-popconfirm content="删除后，不再被其他规则覆盖的候选文章将恢复显示，未完成任务也会恢复待抓取。确认删除？" @ok="deleteDomainBlacklist(entry.id)">
            <a-button size="small" status="danger" :loading="deletingBlacklistId === entry.id">
              <template #icon><icon-delete /></template>
              删除
            </a-button>
          </a-popconfirm>
        </article>
      </div>
    </a-spin>
  </section>
</template>

<script setup lang="ts">
// Owner 爬取队列观察与域名黑名单。
import { formatDateTime } from '@/components/recommendations/format'
import { authHeaders, requestJSON } from '@/components/recommendations/http'
import {
  emptyCrawlQueue,
  type CrawlQueueSnapshot,
  type CrawlQueueTask,
  type CrawlTaskStatus,
  type DomainBlacklistEntry,
  type DomainBlacklistMutation,
} from '@/components/recommendations/types'
import { groupCrawlQueueTasks } from '@/utils/crawlQueueGrouping.mjs'
import { IconDelete, IconRefresh } from '@arco-design/web-vue/es/icon'
import { Message } from '@arco-design/web-vue'
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'

const props = defineProps<{ active: boolean; isOwner: boolean }>()
const emit = defineEmits<{ 'inventory-changed': [] }>()

const crawlQueueLoading = ref(false)
const blacklistLoading = ref(false)
const addingBlacklist = ref(false)
const deletingBlacklistId = ref<number | null>(null)
const crawlQueue = ref<CrawlQueueSnapshot>(emptyCrawlQueue())
const domainBlacklist = ref<DomainBlacklistEntry[]>([])
const blacklistForm = reactive({ domain: '', reason: '' })
let crawlQueueTimer: ReturnType<typeof window.setTimeout> | null = null

const crawlQueueStateLabel = computed(() => ({
  idle: '队列空闲',
  waiting: '排队等待工人',
  running: '正在执行',
}[crawlQueue.value.state]))

const displayedCrawlQueueTasks = computed(() => groupCrawlQueueTasks(crawlQueue.value.tasks))

function clearCrawlQueueTimer() {
  if (crawlQueueTimer !== null) {
    window.clearTimeout(crawlQueueTimer)
    crawlQueueTimer = null
  }
}

function scheduleCrawlQueueRefresh() {
  clearCrawlQueueTimer()
  if (!props.isOwner || !props.active) return
  const delay = crawlQueue.value.state === 'running' ? 2000 : 10000
  crawlQueueTimer = window.setTimeout(() => { void loadCrawlQueue(true) }, delay)
}

async function loadCrawlQueue(silent = false) {
  if (!props.isOwner) return
  const wasRunning = crawlQueue.value.state === 'running'
  try {
    if (!silent) crawlQueueLoading.value = true
    crawlQueue.value = await requestJSON<CrawlQueueSnapshot>('/api/admin/discovery/crawl-queue?limit=50', { headers: authHeaders() })
    if (wasRunning && crawlQueue.value.state !== 'running') emit('inventory-changed')
  } catch (error) {
    if (!silent) Message.error(error instanceof Error ? error.message : '加载爬取任务队列失败')
  } finally {
    crawlQueueLoading.value = false
    scheduleCrawlQueueRefresh()
  }
}

async function loadDomainBlacklist(silent = false) {
  if (!props.isOwner) return
  try {
    if (!silent) blacklistLoading.value = true
    domainBlacklist.value = (await requestJSON<DomainBlacklistEntry[]>('/api/admin/discovery/domain-blacklist', { headers: authHeaders() })) ?? []
  } catch (error) {
    if (!silent) Message.error(error instanceof Error ? error.message : '加载域名黑名单失败')
  } finally {
    blacklistLoading.value = false
  }
}

async function refreshQueueTab() {
  await Promise.all([loadCrawlQueue(), loadDomainBlacklist()])
}

async function addDomainBlacklist() {
  const domain = blacklistForm.domain.trim()
  if (!domain) {
    Message.warning('请输入要屏蔽的域名')
    return
  }
  try {
    addingBlacklist.value = true
    const mutation = await requestJSON<DomainBlacklistMutation>('/api/admin/discovery/domain-blacklist', {
      method: 'POST', headers: authHeaders(true), body: JSON.stringify({ domain, reason: blacklistForm.reason.trim() }),
    })
    blacklistForm.domain = ''
    blacklistForm.reason = ''
    await Promise.all([loadDomainBlacklist(true), loadCrawlQueue(true)])
    emit('inventory-changed')
    Message.success(`域名已加入黑名单，剔除 ${mutation.affectedCandidates || 0} 篇候选文章`)
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '添加域名黑名单失败')
  } finally {
    addingBlacklist.value = false
  }
}

async function deleteDomainBlacklist(id: number) {
  try {
    deletingBlacklistId.value = id
    const mutation = await requestJSON<DomainBlacklistMutation>(`/api/admin/discovery/domain-blacklist/${id}`, { method: 'DELETE', headers: authHeaders() })
    await Promise.all([loadDomainBlacklist(true), loadCrawlQueue(true)])
    emit('inventory-changed')
    Message.success(`域名黑名单已删除，恢复 ${mutation.affectedCandidates || 0} 篇候选文章`)
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '删除域名黑名单失败')
  } finally {
    deletingBlacklistId.value = null
  }
}

function crawlTaskKindLabel(kind: string): string {
  return ({
    discovery_fetch_source: '订阅源抓取',
    discovery_scan_blogroll: 'Blogroll 扫描',
    discovery_backfill_site: '历史回溯',
    discovery_process_candidate: '文章正文处理',
  } as Record<string, string>)[kind] || kind
}

function crawlTaskStatusLabel(status: CrawlTaskStatus): string {
  return ({ pending: '等待', running: '运行中', succeeded: '成功', failed: '失败' } as Record<CrawlTaskStatus, string>)[status]
}

function crawlTaskTargetLabel(task: CrawlQueueTask): string {
  const type = ({ source: '来源', site: '站点', candidate: '候选文章' } as Record<string, string>)[task.targetType] || '目标'
  const version = task.contentVersion ? ` · 正文 v${task.contentVersion}` : ''
  return `${type} #${task.targetId || '-'}${version}`
}

function crawlTaskTimeLabel(task: CrawlQueueTask): string {
  const value = task.finishedAt || task.startedAt || task.scheduledAt || task.createdAt
  return value ? formatDateTime(value) : '暂无时间'
}

function crawlTaskGroupTimeLabel(item: { windowStart: string; windowEnd: string }): string {
  const start = formatDateTime(item.windowStart)
  const end = formatDateTime(item.windowEnd)
  return start === end ? start : `${start} 至 ${end}`
}

watch(() => props.active, (active) => {
  if (active && props.isOwner) {
    void refreshQueueTab()
    return
  }
  clearCrawlQueueTimer()
})

onBeforeUnmount(clearCrawlQueueTimer)
defineExpose({ reload: refreshQueueTab })
</script>

<style scoped src="./panel-shared.css"></style>
<style scoped>
.queue-state {
  padding: 5px 10px;
  border-radius: 999px;
  background: #f2f3f5;
  color: #4e5969;
  font-size: 13px;
}

.queue-state-running {
  background: #e8f3ff;
  color: #165dff;
}

.queue-state-waiting {
  background: #fff7e8;
  color: #d46b08;
}

.queue-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.queue-summary > div {
  display: grid;
  gap: 4px;
  padding: 14px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  background: #f7f8fa;
}

.queue-summary strong {
  font-size: 24px;
}

.queue-summary span,
.queue-task-row span,
.queue-task-row small {
  color: #86909c;
  font-size: 12px;
}

.queue-task-list {
  display: grid;
  gap: 8px;
}

.queue-task-row {
  display: grid;
  grid-template-columns: minmax(180px, 1.4fr) 90px 90px minmax(150px, 1fr);
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
}

.queue-task-row > div {
  display: grid;
  gap: 4px;
}

.queue-task-summary {
  border-style: dashed;
  background: #f7f8fa;
}

.queue-task-summary strong {
  color: #4e5969;
}

.task-status {
  width: fit-content;
  padding: 3px 8px;
  border-radius: 999px;
  background: #f2f3f5;
}

.task-status-running { background: #e8f3ff; color: #165dff !important; }
.task-status-succeeded { background: #e8ffea; color: #00a854 !important; }
.task-status-failed { background: #ffece8; color: #f53f3f !important; }
.queue-task-error { grid-column: 1 / -1; color: #f53f3f !important; overflow-wrap: anywhere; }

.domain-blacklist-panel {
  border-top: 1px solid #e5e6eb;
}

.blacklist-form {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) minmax(280px, 2fr) auto;
  align-items: end;
  gap: 12px;
  margin-bottom: 16px;
}

.blacklist-list {
  display: grid;
  gap: 8px;
}

.blacklist-row {
  display: grid;
  grid-template-columns: minmax(220px, 1.2fr) minmax(220px, 1fr) auto;
  align-items: center;
  gap: 14px;
  padding: 12px 14px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
}

.blacklist-row > div {
  display: grid;
  gap: 4px;
}

.blacklist-row span {
  color: #86909c;
  font-size: 12px;
  overflow-wrap: anywhere;
}

@media (max-width: 820px) {
  .blacklist-form,
  .blacklist-row,
  .queue-summary,
  .queue-task-row {
    grid-template-columns: 1fr;
  }
}
</style>
