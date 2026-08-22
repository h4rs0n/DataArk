<template>
  <section class="panel">
    <div class="section-title">
      <div>
        <!-- 左右箭头按自然日翻阅今日与历史日报 -->
        <div class="digest-date-nav">
          <a-button class="digest-date-btn" type="text" aria-label="前一天" :disabled="digestLoading" @click="shiftDigestDate(-1)">
            <template #icon><icon-left /></template>
          </a-button>
          <!-- 点击日期弹出日历；无日报日期由 disabledDate 显示为灰色 -->
          <a-config-provider :locale="zhCN">
            <a-date-picker
              class="digest-date-picker"
              :model-value="digestDate"
              value-format="YYYY-MM-DD"
              format="YYYY-MM-DD"
              :allow-clear="false"
              :show-now-btn="false"
              :disabled="digestLoading"
              :disabled-date="disableDigestCalendarDate"
              :day-start-of-week="1"
              position="bl"
              :trigger-props="{ contentClass: 'digest-calendar-popup' }"
              @change="onDigestCalendarChange"
              @popup-visible-change="onDigestCalendarVisible"
            >
              <h2 class="digest-date-label" role="button" tabindex="0" aria-label="选择日报日期">{{ digestDate }}</h2>
            </a-date-picker>
          </a-config-provider>
          <a-button class="digest-date-btn" type="text" aria-label="后一天" :disabled="isViewingToday || digestLoading" @click="shiftDigestDate(1)">
            <template #icon><icon-right /></template>
          </a-button>
          <a-tag v-if="isViewingToday" size="small" color="arcoblue">今天</a-tag>
        </div>
        <span>{{ digestStatusText }}</span>
      </div>
      <a-space wrap>
        <a-button v-if="isOwner && isViewingToday && digestSnapshot.day.actualCount < digestSnapshot.day.requestedCount && ['published', 'supplemented'].includes(digestSnapshot.day.status)" :loading="generating" type="primary" @click="supplementDaily(digestSnapshot.day.date)">
          补充缺少文章
        </a-button>
        <a-button :loading="digestLoading" @click="reloadSelectedDigest">
          <template #icon><icon-refresh /></template>
          刷新
        </a-button>
      </a-space>
    </div>

    <a-spin :loading="digestLoading" class="digest-spin">
      <div class="today-summary">
        <a-spin v-if="digestSummaryLoading" :size="16" tip="正在生成日报总结…" class="today-summary-loading" />
        <template v-else-if="digestSummary?.available">
          <p class="today-summary-overview">{{ digestSummary.overview }}</p>
          <ul v-if="digestSummary.highlights.length" class="today-summary-highlights">
            <li v-for="(highlight, index) in digestSummary.highlights" :key="index">{{ highlight }}</li>
          </ul>
          <div v-if="digestSummary.topics.length" class="today-summary-topics">
            <a-tag v-for="topic in digestSummary.topics" :key="topic" size="small">{{ topic }}</a-tag>
          </div>
          <small class="today-summary-meta">
            {{ digestSummary.model === 'rule-based' ? '统计总结' : 'AI 总结' }}
            <template v-if="digestSummary.generatedAt"> · 生成于 {{ formatDateTime(digestSummary.generatedAt) }}</template>
          </small>
        </template>
        <p v-else-if="digestSummaryError" class="today-summary-error">总结暂不可用：{{ digestSummaryError }}</p>
        <p v-else-if="digestSummary" class="today-summary-empty">{{ digestSummary.reason || '日报生成后将自动总结' }}</p>
      </div>

      <DigestSummary :day="digestSnapshot.day" />

      <a-empty v-if="digestSnapshot.items.length === 0" :description="isViewingToday ? '暂无每日推荐' : '该日暂无推荐'" />
      <div v-else class="recommendation-list">
        <RecommendationArticleCard
          v-for="item in digestSnapshot.items"
          :key="item.id"
          :item="item"
          :archiving="archivingCandidateId === item.candidateId"
          :current-action="feedbackByItem[item.id]?.action"
          :feedback-loading="feedbackLoadingId === item.id"
          @open="$emit('open', $event)"
          @archive="$emit('archive', $event)"
          @context="$emit('context', $event)"
          @mark-read="$emit('mark-read', $event)"
          @feedback="(action) => $emit('feedback', item, action)"
          @scope="(scope) => $emit('scope', item, scope)"
          @revert="$emit('revert', item)"
        />
      </div>
    </a-spin>
  </section>
</template>

<script setup lang="ts">
// 每日推荐：按日历翻阅今日与历史日报。
import DigestSummary from '@/components/recommendations/DigestSummary.vue'
import RecommendationArticleCard from '@/components/recommendations/RecommendationArticleCard.vue'
import { addCalendarDays, formatDateTime, formatLocalDate } from '@/components/recommendations/format'
import { authHeaders, requestJSON } from '@/components/recommendations/http'
import {
  emptySnapshot,
  type BlockRuleType,
  type DiscoveryCandidate,
  type FeedbackAction,
  type FeedbackByItem,
  type RecommendationDay,
  type RecommendationItem,
  type RecommendationSnapshot,
  type TodayDigestSummary,
} from '@/components/recommendations/types'
import { IconLeft, IconRefresh, IconRight } from '@arco-design/web-vue/es/icon'
import { Message } from '@arco-design/web-vue'
import zhCN from '@arco-design/web-vue/es/locale/lang/zh-cn'
import { computed, ref } from 'vue'

const props = defineProps<{
  isOwner: boolean
  feedbackByItem: FeedbackByItem
  archivingCandidateId: number | null
  feedbackLoadingId: number | null
  loadItemFeedback: (item: RecommendationItem) => Promise<void>
}>()

defineEmits<{
  open: [candidate: DiscoveryCandidate]
  archive: [candidateId: number]
  context: [item: RecommendationItem]
  'mark-read': [candidate: DiscoveryCandidate]
  feedback: [item: RecommendationItem, action: FeedbackAction]
  scope: [item: RecommendationItem, scope: BlockRuleType]
  revert: [item: RecommendationItem]
}>()

const generating = ref(false)
const digestDate = ref(formatLocalDate(new Date()))
const todayDate = ref(digestDate.value)
const digestSnapshot = ref<RecommendationSnapshot>(emptySnapshot(digestDate.value))
const digestSummary = ref<TodayDigestSummary | null>(null)
const digestSummaryLoading = ref(false)
const digestSummaryError = ref('')
const digestLoading = ref(false)
const digestAvailableDates = ref<Set<string>>(new Set())
const digestCalendarReady = ref(false)
let digestLoadToken = 0

const isViewingToday = computed(() => digestDate.value === todayDate.value)

const digestStatusText = computed(() => {
  const day = digestSnapshot.value.day
  let status = day.status
  if (day.status === 'published' || day.status === 'supplemented') {
    status = `${day.actualCount}/${day.requestedCount} 篇 · ${day.generatedAt ? formatDateTime(day.generatedAt) : '已生成'}`
  } else if (day.status === 'missing') {
    status = isViewingToday.value ? '尚未生成' : '该日尚未生成日报'
  }
  if (!isViewingToday.value && day.status !== 'missing') {
    return `${status} · 历史快照保持生成时的排序和理由`
  }
  return status
})

function normalizeSnapshot(snapshot: RecommendationSnapshot | null | undefined): RecommendationSnapshot {
  const fallback = emptySnapshot(formatLocalDate(new Date()))
  if (!snapshot?.day) return fallback
  return { day: snapshot.day, items: snapshot.items ?? [] }
}

// loadTodayAnchor 用服务器时区下的“今天”校正可翻阅上限，避免浏览器时区与用户设置不一致。
async function loadTodayAnchor() {
  const snapshot = normalizeSnapshot(await requestJSON<RecommendationSnapshot>('/api/recommendations/today', { headers: authHeaders() }))
  todayDate.value = snapshot.day.date || formatLocalDate(new Date())
  if (!digestDate.value || digestDate.value >= todayDate.value) {
    digestDate.value = todayDate.value
  }
  return snapshot
}

const digestSnapshotURL = (date: string) => (
  date === todayDate.value ? '/api/recommendations/today' : `/api/recommendations/days/${date}`
)

const digestSummaryURL = (date: string) => (
  date === todayDate.value ? '/api/recommendations/today/summary' : `/api/recommendations/days/${date}/summary`
)

async function applyDigestSnapshot(snapshot: RecommendationSnapshot) {
  digestSnapshot.value = snapshot
  await Promise.all(digestSnapshot.value.items.map((item) => props.loadItemFeedback(item)))
}

async function loadSelectedDigest(refreshToday = false) {
  const token = ++digestLoadToken
  digestLoading.value = true
  try {
    let snapshot: RecommendationSnapshot
    const selected = digestDate.value
    if (refreshToday || selected === todayDate.value) {
      const todaySnapshot = await loadTodayAnchor()
      if (token !== digestLoadToken) return
      snapshot = digestDate.value === todayDate.value
        ? todaySnapshot
        : normalizeSnapshot(await requestJSON<RecommendationSnapshot>(digestSnapshotURL(digestDate.value), { headers: authHeaders() }))
    } else {
      snapshot = normalizeSnapshot(await requestJSON<RecommendationSnapshot>(digestSnapshotURL(selected), { headers: authHeaders() }))
    }
    if (token !== digestLoadToken) return
    digestSummary.value = null
    digestSummaryError.value = ''
    await applyDigestSnapshot(snapshot)
    await loadDigestSummary(digestDate.value, token)
  } finally {
    if (token === digestLoadToken) digestLoading.value = false
  }
}

async function reloadSelectedDigest() {
  await loadSelectedDigest(true)
}

async function loadDigestSummary(date = digestDate.value, token = digestLoadToken) {
  digestSummaryLoading.value = true
  digestSummaryError.value = ''
  try {
    const summary = await requestJSON<TodayDigestSummary>(digestSummaryURL(date), { headers: authHeaders() })
    if (token !== digestLoadToken) return
    digestSummary.value = summary
  } catch (error) {
    if (token !== digestLoadToken) return
    digestSummary.value = null
    digestSummaryError.value = error instanceof Error ? error.message : '总结生成失败'
  } finally {
    if (token === digestLoadToken) digestSummaryLoading.value = false
  }
}

// loadDigestCalendarDates 拉取已发布日报日期，供日历把无日报的日子标灰。
async function loadDigestCalendarDates() {
  try {
    const dates = new Set<string>()
    for (let page = 1; page <= 12; page++) {
      const days = (await requestJSON<RecommendationDay[]>(`/api/recommendations/history?page=${page}&pageSize=100`, { headers: authHeaders() })) ?? []
      for (const day of days) {
        if (day.status === 'published' || day.status === 'supplemented') dates.add(day.date)
      }
      if (days.length < 100) break
    }
    digestAvailableDates.value = dates
    digestCalendarReady.value = true
  } catch {
    digestCalendarReady.value = false
  }
}

// disableDigestCalendarDate 禁止选择明天及以后；已加载日历时，没有发布日报的历史日期显示为灰色。
function disableDigestCalendarDate(current?: Date) {
  if (!current) return true
  const date = formatLocalDate(current)
  if (!todayDate.value || date > todayDate.value) return true
  if (date === todayDate.value) return false
  if (!digestCalendarReady.value) return false
  return !digestAvailableDates.value.has(date)
}

function onDigestCalendarVisible(visible: boolean) {
  if (visible) void loadDigestCalendarDates()
}

async function onDigestCalendarChange(value?: string | Date | number) {
  const date = typeof value === 'string' ? value : value ? formatLocalDate(new Date(value)) : ''
  if (!date || date === digestDate.value || date > todayDate.value) return
  digestDate.value = date
  await loadSelectedDigest()
}

// shiftDigestDate 按公历日前后翻页，不能翻到用户时区的明天及以后。
async function shiftDigestDate(delta: number) {
  const next = addCalendarDays(digestDate.value, delta)
  if (next > todayDate.value) return
  digestDate.value = next
  await loadSelectedDigest()
}

async function supplementDaily(date: string) {
  try {
    generating.value = true
    const query = date ? `?date=${encodeURIComponent(date)}` : ''
    digestSnapshot.value = normalizeSnapshot(await requestJSON<RecommendationSnapshot>(`/api/admin/recommendations/supplement${query}`, { method: 'POST', headers: authHeaders() }))
    void loadDigestSummary()
    void loadDigestCalendarDates()
    Message.success('日报已按缺口追加')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '补充日报失败')
  } finally {
    generating.value = false
  }
}

defineExpose({
  reload: async () => {
    await Promise.all([loadSelectedDigest(true), loadDigestCalendarDates()])
  },
})
</script>

<style scoped src="./panel-shared.css"></style>
<style scoped>
.digest-spin {
  display: block;
  width: 100%;
  min-height: 180px;
}

.digest-date-nav {
  display: flex;
  align-items: center;
  gap: 4px;
}

.digest-date-picker {
  display: inline-flex;
  align-items: center;
}

.digest-date-nav :deep(.arco-picker) {
  width: auto;
}

.digest-date-label {
  margin: 0;
  min-width: 11ch;
  padding: 2px 8px;
  border-radius: 6px;
  text-align: center;
  font-size: 20px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  cursor: pointer;
  user-select: none;
}

.digest-date-label:hover,
.digest-date-label:focus-visible {
  background: #f2f3f5;
  outline: none;
}

.digest-date-btn {
  padding: 0 6px;
  color: #4e5969;
}

.today-summary {
  margin-bottom: 16px;
  padding: 14px 16px;
  background: #f7f8fa;
  border-radius: 8px;
  min-height: 48px;
}

.today-summary-loading {
  display: flex;
  align-items: center;
}

.today-summary-overview {
  margin: 0;
  color: #1d2129;
  line-height: 1.7;
}

.today-summary-highlights {
  margin: 8px 0 0;
  padding-left: 20px;
  color: #4e5969;
}

.today-summary-highlights li {
  margin-top: 4px;
}

.today-summary-topics {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
}

.today-summary-meta {
  display: block;
  margin-top: 8px;
  color: #86909c;
}

.today-summary-error,
.today-summary-empty {
  margin: 0;
  color: #86909c;
}
</style>

<style>
/* 日历面板 teleport 到 body，无日报日期用灰色且不可点 */
.digest-calendar-popup .arco-picker-cell-disabled,
.digest-calendar-popup .arco-picker-cell-disabled .arco-picker-date {
  color: #c9cdd4;
  background: transparent;
  cursor: not-allowed;
}
</style>
