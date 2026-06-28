<template>
  <div class="recommendations-view">
    <div class="top-bar">
      <a-button type="text" class="back-button" @click="router.push('/')">
        <template #icon><icon-arrow-left /></template>
        返回首页
      </a-button>
    </div>

    <main class="content">
      <header class="page-header">
        <div class="header-mark"><icon-robot /></div>
        <div>
          <h1>推荐中心</h1>
          <p>今日推荐、历史日报、内容发现和推荐偏好统一管理。</p>
        </div>
        <a-button :loading="loading" @click="loadAll">
          <template #icon><icon-refresh /></template>
          刷新
        </a-button>
      </header>

      <a-tabs v-model:active-key="activeTab" class="tabs">
        <a-tab-pane key="today" title="今日推荐">
          <section class="panel">
            <div class="section-title">
              <div>
                <h2>{{ todaySnapshot.day.date }}</h2>
                <span>{{ todayStatusText }}</span>
              </div>
              <a-space wrap>
                <a-button :loading="generating" type="primary" @click="generateDaily(todaySnapshot.day.date)">
                  <template #icon><icon-calendar-clock /></template>
                  生成今日
                </a-button>
                <a-button @click="loadToday">
                  <template #icon><icon-refresh /></template>
                  刷新
                </a-button>
              </a-space>
            </div>

            <a-empty v-if="todaySnapshot.items.length === 0" description="暂无今日推荐" />
            <div v-else class="recommendation-list">
              <article v-for="item in todaySnapshot.items" :key="item.id" class="recommendation-card">
                <div class="item-main">
                  <div class="item-meta">
                    <span>#{{ item.rank }}</span>
                    <span>{{ item.candidate.sourceName || sourceHost(item.candidate.url) }}</span>
                    <span v-if="item.candidate.publishedAt">{{ formatDateTime(item.candidate.publishedAt) }}</span>
                  </div>
                  <h3>{{ item.candidate.title || `候选文章 ${item.candidateId}` }}</h3>
                  <p>{{ item.candidate.summary || item.candidate.url }}</p>
                  <div class="topic-row">
                    <span v-for="topic in parseList(item.candidate.topics).slice(0, 4)" :key="topic">{{ topic }}</span>
                  </div>
                  <small>{{ item.reason || '基于内容质量和反馈画像推荐' }}</small>
                </div>
                <div class="item-actions">
                  <a-button @click="openCandidate(item.candidate)">
                    <template #icon><icon-link /></template>
                    原文
                  </a-button>
                  <a-button type="primary" :loading="archivingCandidateId === item.candidateId" @click="archiveCandidate(item.candidateId)">
                    <template #icon><icon-storage /></template>
                    入库
                  </a-button>
                  <div class="feedback-row">
                    <a-tooltip content="有价值">
                      <a-button size="small" :loading="feedbackLoadingId === item.id" @click="sendFeedback(item, 'valuable')">
                        <template #icon><icon-thumb-up /></template>
                      </a-button>
                    </a-tooltip>
                    <a-tooltip content="不感兴趣">
                      <a-button size="small" :loading="feedbackLoadingId === item.id" @click="sendFeedback(item, 'not_interested')">
                        <template #icon><icon-thumb-down /></template>
                      </a-button>
                    </a-tooltip>
                    <a-tooltip content="太重复">
                      <a-button size="small" :loading="feedbackLoadingId === item.id" @click="sendFeedback(item, 'duplicate')">
                        <template #icon><icon-loop /></template>
                      </a-button>
                    </a-tooltip>
                    <a-tooltip content="值得深读">
                      <a-button size="small" :loading="feedbackLoadingId === item.id" @click="sendFeedback(item, 'deep_read')">
                        <template #icon><icon-star /></template>
                      </a-button>
                    </a-tooltip>
                    <a-tooltip content="屏蔽此类">
                      <a-button size="small" status="danger" @click="openBlockDialog(item)">
                        <template #icon><icon-stop /></template>
                      </a-button>
                    </a-tooltip>
                  </div>
                </div>
              </article>
            </div>
          </section>
        </a-tab-pane>

        <a-tab-pane key="history" title="历史日报">
          <section class="panel">
            <div class="section-title">
              <div>
                <h2>历史日报</h2>
                <span>历史快照保持生成时的排序和理由</span>
              </div>
              <label class="inline-field" for="history-date">
                <span>日期</span>
                <input id="history-date" v-model="historyDate" type="date" @change="loadHistoryDay" />
              </label>
            </div>
            <div class="history-layout">
              <aside class="day-list">
                <button v-for="day in historyDays" :key="day.id" type="button" :class="{ active: day.date === historyDate }" @click="selectHistoryDay(day.date)">
                  <strong>{{ day.date }}</strong>
                  <span>{{ day.actualCount }}/{{ day.requestedCount }} · {{ day.status }}</span>
                </button>
              </aside>
              <div class="history-detail">
                <a-empty v-if="historySnapshot.items.length === 0" description="暂无历史推荐" />
                <template v-else>
                  <article v-for="item in historySnapshot.items" :key="item.id" class="compact-card">
                    <div class="item-main">
                      <div class="item-meta">
                        <span>#{{ item.rank }}</span>
                        <span>{{ item.candidate.sourceName || sourceHost(item.candidate.url) }}</span>
                      </div>
                      <h3>{{ item.candidate.title }}</h3>
                      <p>{{ item.candidate.summary || item.candidate.url }}</p>
                      <small>{{ item.reason }}</small>
                    </div>
                    <a-button @click="openCandidate(item.candidate)">
                      <template #icon><icon-link /></template>
                      原文
                    </a-button>
                  </article>
                </template>
              </div>
            </div>
          </section>
        </a-tab-pane>

        <a-tab-pane key="discovery" title="内容发现">
          <section class="panel source-panel">
            <div class="section-title">
              <div>
                <h2>订阅源</h2>
                <span>RSS、RSSHub 或受限站点发现</span>
              </div>
            </div>
            <form class="source-form" @submit.prevent="saveSource">
              <label class="source-field" for="source-name">
                <span>名称</span>
                <input id="source-name" v-model="sourceForm.name" name="source-name" placeholder="名称" />
              </label>
              <label class="source-field source-url" for="source-url">
                <span>URL</span>
                <input id="source-url" v-model="sourceForm.url" name="source-url" placeholder="https://example.com/feed.xml" />
              </label>
              <label class="source-field" for="source-type">
                <span>类型</span>
                <select id="source-type" v-model="sourceForm.type" name="source-type">
                  <option value="feed">RSS</option>
                  <option value="rsshub">RSSHub</option>
                  <option value="site">站点</option>
                </select>
              </label>
              <a-button type="primary" html-type="submit" :loading="savingSource">
                <template #icon><icon-plus /></template>
                添加
              </a-button>
            </form>

            <div class="source-list">
              <article v-for="source in sources" :key="source.id" class="source-row">
                <div class="item-main">
                  <h3>{{ source.name }}</h3>
                  <p>{{ source.url }}</p>
                  <span>{{ source.type }} · {{ source.enabled ? '启用' : '停用' }}</span>
                  <small v-if="source.lastError">{{ source.lastError }}</small>
                </div>
                <a-space>
                  <a-button size="small" :loading="fetchingSourceId === source.id" @click="fetchSource(source.id)">
                    <template #icon><icon-sync /></template>
                    获取
                  </a-button>
                  <a-button size="small" status="danger" @click="deleteSource(source.id)">
                    <template #icon><icon-delete /></template>
                  </a-button>
                </a-space>
              </article>
            </div>
          </section>

          <section class="panel">
            <div class="section-title">
              <div>
                <h2>候选文章</h2>
                <span>采集后进入 enrichment 和日报生成流程</span>
              </div>
              <label class="inline-field" for="candidate-status">
                <span>状态</span>
                <select id="candidate-status" v-model="candidateStatus" name="candidate-status" @change="loadCandidates">
                  <option value="new">待读</option>
                  <option value="read">已读</option>
                  <option value="ignored">已忽略</option>
                  <option value="archived">已入库</option>
                </select>
              </label>
            </div>
            <a-empty v-if="candidates.length === 0" description="暂无候选文章" />
            <div v-else class="item-list">
              <article v-for="candidate in candidates" :key="candidate.id" class="compact-card">
                <div class="item-main">
                  <h3>{{ candidate.title }}</h3>
                  <p>{{ candidate.summary || candidate.url }}</p>
                  <span>{{ candidate.sourceName }} · {{ candidate.status }} · {{ candidate.enrichmentStatus || 'pending' }}</span>
                </div>
                <a-space class="candidate-actions" wrap>
                  <a-button @click="openCandidate(candidate)">
                    <template #icon><icon-link /></template>
                    原文
                  </a-button>
                  <a-button type="primary" :loading="archivingCandidateId === candidate.id" @click="archiveCandidate(candidate.id)">
                    <template #icon><icon-storage /></template>
                    入库
                  </a-button>
                  <a-button status="danger" @click="ignoreCandidate(candidate.id)">
                    <template #icon><icon-close /></template>
                    忽略
                  </a-button>
                </a-space>
              </article>
            </div>
          </section>
        </a-tab-pane>

        <a-tab-pane key="settings" title="推荐设置">
          <section class="panel settings-grid">
            <form class="settings-form" @submit.prevent="saveSettings">
              <h2>日报设置</h2>
              <label class="source-field" for="daily-limit">
                <span>每日数量</span>
                <input id="daily-limit" v-model.number="settingsForm.dailyLimit" name="daily-limit" type="number" min="1" max="50" />
              </label>
              <label class="source-field" for="candidate-window-days">
                <span>候选窗口天数</span>
                <input id="candidate-window-days" v-model.number="settingsForm.candidateWindowDays" name="candidate-window-days" type="number" min="1" max="365" />
              </label>
              <label class="source-field" for="recommendation-timezone">
                <span>时区</span>
                <input id="recommendation-timezone" v-model="settingsForm.timezone" name="recommendation-timezone" placeholder="Asia/Shanghai" />
              </label>
              <label class="source-field" for="generation-time">
                <span>生成时间</span>
                <input id="generation-time" v-model="settingsForm.generationTime" name="generation-time" type="time" />
              </label>
              <label class="toggle-row">
                <input id="recommendation-enabled" v-model="settingsForm.enabled" name="recommendation-enabled" type="checkbox" />
                <span>启用日报生成</span>
              </label>
              <a-button type="primary" html-type="submit" :loading="savingSettings">
                <template #icon><icon-settings /></template>
                保存设置
              </a-button>
            </form>

            <section class="block-list">
              <h2>屏蔽规则</h2>
              <a-empty v-if="blockRules.length === 0" description="暂无屏蔽规则" />
              <article v-for="rule in blockRules" :key="rule.id" class="block-row">
                <span>{{ blockRuleTypeLabel(rule.type) }}</span>
                <strong>{{ rule.value }}</strong>
                <a-button size="small" status="danger" @click="deleteBlockRule(rule.id)">
                  <template #icon><icon-delete /></template>
                </a-button>
              </article>
            </section>
          </section>
        </a-tab-pane>

        <a-tab-pane key="ranking" title="点击排行">
          <section class="panel">
            <div class="section-title">
              <h2>被点击网页文件排行榜</h2>
              <a-radio-group v-model="rankingWindow" type="button" size="small" @change="loadRankings">
                <a-radio value="7d">7 天</a-radio>
                <a-radio value="all">总榜</a-radio>
              </a-radio-group>
            </div>
            <a-empty v-if="rankings.length === 0" description="暂无点击记录" />
            <div v-else class="item-list">
              <article v-for="item in rankings" :key="item.path" class="compact-card">
                <div class="item-main">
                  <h3>{{ item.title || item.fileName }}</h3>
                  <p>{{ item.summary || item.path }}</p>
                  <span>{{ item.domain }} · {{ item.clickCount }} 次点击</span>
                </div>
                <a-button type="primary" @click="openArchive(item.path)">
                  <template #icon><icon-eye /></template>
                  查看
                </a-button>
              </article>
            </div>
          </section>
        </a-tab-pane>

        <a-tab-pane key="keywords" title="搜索热词">
          <section class="panel">
            <div class="section-title">
              <h2>最常用搜索关键词</h2>
              <a-radio-group v-model="keywordWindow" type="button" size="small" @change="loadKeywords">
                <a-radio value="7d">7 天</a-radio>
                <a-radio value="all">总榜</a-radio>
              </a-radio-group>
            </div>
            <a-empty v-if="keywords.length === 0" description="暂无搜索记录" />
            <div v-else class="keyword-grid">
              <button v-for="item in keywords" :key="item.keyword" class="keyword-tile" type="button" @click="searchKeyword(item.keyword)">
                <strong>{{ item.keyword }}</strong>
                <span>{{ item.count }} 次 · {{ item.resultCount }} 条结果</span>
              </button>
            </div>
          </section>
        </a-tab-pane>

        <a-tab-pane key="archives" title="归档推荐">
          <section class="panel">
            <div class="section-title">
              <h2>网页归档文件推荐</h2>
              <span>基于点击、搜索词和内容元数据</span>
            </div>
            <a-empty v-if="archiveRecommendations.length === 0" description="暂无可推荐归档" />
            <div v-else class="item-list">
              <article v-for="item in archiveRecommendations" :key="item.path" class="compact-card">
                <div class="item-main">
                  <h3>{{ item.title || item.fileName }}</h3>
                  <p>{{ item.summary || item.path }}</p>
                  <span>{{ item.domain }} · {{ item.reason }} · 分数 {{ item.score.toFixed(1) }}</span>
                </div>
                <a-button type="primary" @click="openArchive(item.path)">
                  <template #icon><icon-eye /></template>
                  查看
                </a-button>
              </article>
            </div>
          </section>
        </a-tab-pane>
      </a-tabs>
    </main>

    <a-modal v-model:visible="blockDialog.visible" title="屏蔽此类" :ok-loading="feedbackLoadingId === blockDialog.item?.id" @ok="submitBlockFeedback">
      <div class="block-options">
        <label v-for="option in blockOptions" :key="`${option.type}:${option.value}`">
          <input v-model="selectedBlockKeys" type="checkbox" :value="`${option.type}:${option.value}`" />
          <span>{{ blockRuleTypeLabel(option.type) }}：{{ option.value }}</span>
        </label>
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Message, Notification } from '@arco-design/web-vue'
import {
  IconArrowLeft,
  IconCalendarClock,
  IconClose,
  IconDelete,
  IconEye,
  IconLink,
  IconLoop,
  IconPlus,
  IconRefresh,
  IconRobot,
  IconSettings,
  IconStar,
  IconStop,
  IconStorage,
  IconSync,
  IconThumbDown,
  IconThumbUp,
} from '@arco-design/web-vue/es/icon'

type FeedbackAction = 'valuable' | 'not_interested' | 'duplicate' | 'deep_read' | 'block'
type BlockRuleType = 'topic' | 'source' | 'style'

interface ArchiveRankingItem {
  path: string
  domain: string
  fileName: string
  title: string
  summary: string
  clickCount: number
}

interface ArchiveRecommendationItem extends ArchiveRankingItem {
  score: number
  reason: string
}

interface KeywordItem {
  keyword: string
  count: number
  resultCount: number
}

interface DiscoverySource {
  id: number
  name: string
  url: string
  type: string
  enabled: boolean
  lastError: string
}

interface DiscoveryCandidate {
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
}

interface RecommendationDay {
  id: number
  userId: number
  date: string
  timezone: string
  status: string
  requestedCount: number
  actualCount: number
  generatedAt?: string
}

interface RecommendationItem {
  id: number
  candidateId: number
  rank: number
  reason: string
  rerankScore: number
  candidate: DiscoveryCandidate
}

interface RecommendationSnapshot {
  day: RecommendationDay
  items: RecommendationItem[]
}

interface RecommendationSettings {
  dailyLimit: number
  timezone: string
  generationTime: string
  candidateWindowDays: number
  explorationRate: number
  enabled: boolean
}

interface BlockRule {
  id: number
  type: BlockRuleType
  value: string
  active: boolean
}

interface BlockTarget {
  type: BlockRuleType
  value: string
}

const router = useRouter()
const activeTab = ref('today')
const loading = ref(false)
const generating = ref(false)
const savingSettings = ref(false)
const savingSource = ref(false)
const fetchingSourceId = ref<number | null>(null)
const archivingCandidateId = ref<number | null>(null)
const feedbackLoadingId = ref<number | null>(null)
const rankingWindow = ref<'7d' | 'all'>('7d')
const keywordWindow = ref<'7d' | 'all'>('7d')
const candidateStatus = ref('new')
const historyDate = ref(formatLocalDate(new Date()))
const rankings = ref<ArchiveRankingItem[]>([])
const archiveRecommendations = ref<ArchiveRecommendationItem[]>([])
const keywords = ref<KeywordItem[]>([])
const sources = ref<DiscoverySource[]>([])
const candidates = ref<DiscoveryCandidate[]>([])
const historyDays = ref<RecommendationDay[]>([])
const blockRules = ref<BlockRule[]>([])
const todaySnapshot = ref<RecommendationSnapshot>(emptySnapshot(formatLocalDate(new Date())))
const historySnapshot = ref<RecommendationSnapshot>(emptySnapshot(historyDate.value))
const sourceForm = reactive({ name: '', url: '', type: 'feed' })
const settingsForm = reactive<RecommendationSettings>({
  dailyLimit: 10,
  timezone: 'Asia/Shanghai',
  generationTime: '07:00',
  candidateWindowDays: 30,
  explorationRate: 0.15,
  enabled: false,
})
const blockDialog = reactive<{ visible: boolean; item: RecommendationItem | null }>({ visible: false, item: null })
const selectedBlockKeys = ref<string[]>([])

const todayStatusText = computed(() => {
  const day = todaySnapshot.value.day
  if (day.status === 'generated') {
    return `${day.actualCount}/${day.requestedCount} 篇 · ${day.generatedAt ? formatDateTime(day.generatedAt) : '已生成'}`
  }
  if (day.status === 'missing') return '尚未生成'
  return day.status
})

const blockOptions = computed<BlockTarget[]>(() => {
  const item = blockDialog.item
  if (!item) return []
  const candidate = item.candidate
  const options: BlockTarget[] = []
  for (const topic of parseList(candidate.topics).slice(0, 3)) {
    options.push({ type: 'topic', value: topic })
  }
  const host = candidate.sourceName || sourceHost(candidate.url)
  if (host) options.push({ type: 'source', value: host })
  if (candidate.contentStyle) options.push({ type: 'style', value: candidate.contentStyle })
  if (candidate.contentType && candidate.contentType !== candidate.contentStyle) options.push({ type: 'style', value: candidate.contentType })
  return options
})

const authHeaders = (json = false): Record<string, string> => {
  const token = localStorage.getItem('token') || sessionStorage.getItem('token')
  return {
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...(json ? { 'Content-Type': 'application/json' } : {}),
  }
}

const requestJSON = async <T>(url: string, options: RequestInit = {}): Promise<T> => {
  const response = await fetch(url, options)
  const payload = await response.json().catch(() => ({}))
  if (!response.ok || payload.Status === '0') {
    throw new Error(payload.Message || `HTTP ${response.status}`)
  }
  return payload.Data as T
}

const loadToday = async () => {
  todaySnapshot.value = normalizeSnapshot(await requestJSON<RecommendationSnapshot>('/api/recommendations/today', { headers: authHeaders() }))
}

const loadHistory = async () => {
  historyDays.value = (await requestJSON<RecommendationDay[]>('/api/recommendations/history?page=1&pageSize=30', { headers: authHeaders() })) ?? []
  if (!historyDate.value && historyDays.value.length > 0) {
    historyDate.value = historyDays.value[0].date
  }
  await loadHistoryDay()
}

const loadHistoryDay = async () => {
  if (!historyDate.value) return
  historySnapshot.value = normalizeSnapshot(await requestJSON<RecommendationSnapshot>(`/api/recommendations/days/${historyDate.value}`, { headers: authHeaders() }))
}

const selectHistoryDay = async (date: string) => {
  historyDate.value = date
  await loadHistoryDay()
}

const loadSettings = async () => {
  const settings = await requestJSON<RecommendationSettings>('/api/recommendations/settings', { headers: authHeaders() })
  Object.assign(settingsForm, settings)
}

const loadBlocks = async () => {
  blockRules.value = (await requestJSON<BlockRule[]>('/api/recommendations/blocks', { headers: authHeaders() })) ?? []
}

const loadRankings = async () => {
  rankings.value = (await requestJSON<ArchiveRankingItem[]>(`/api/archive/rankings?window=${rankingWindow.value}&limit=20`, { headers: authHeaders() })) ?? []
}

const loadKeywords = async () => {
  keywords.value = (await requestJSON<KeywordItem[]>(`/api/search/keywords?window=${keywordWindow.value}&limit=30`, { headers: authHeaders() })) ?? []
}

const loadArchiveRecommendations = async () => {
  archiveRecommendations.value = (await requestJSON<ArchiveRecommendationItem[]>('/api/recommendations/archives?window=7d&limit=20', { headers: authHeaders() })) ?? []
}

const loadSources = async () => {
  sources.value = (await requestJSON<DiscoverySource[]>('/api/discovery/sources', { headers: authHeaders() })) ?? []
}

const loadCandidates = async () => {
  candidates.value = (await requestJSON<DiscoveryCandidate[]>(`/api/discovery/candidates?status=${candidateStatus.value}&limit=80`, { headers: authHeaders() })) ?? []
}

const loadAll = async () => {
  try {
    loading.value = true
    await Promise.all([
      loadToday(),
      loadHistory(),
      loadSettings(),
      loadBlocks(),
      loadRankings(),
      loadKeywords(),
      loadArchiveRecommendations(),
      loadSources(),
      loadCandidates(),
    ])
  } catch (error) {
    Notification.error({ title: '加载失败', content: error instanceof Error ? error.message : '推荐中心加载失败', position: 'topRight' })
  } finally {
    loading.value = false
  }
}

const generateDaily = async (date: string) => {
  try {
    generating.value = true
    const query = date ? `?date=${encodeURIComponent(date)}` : ''
    todaySnapshot.value = normalizeSnapshot(await requestJSON<RecommendationSnapshot>(`/api/admin/recommendations/generate${query}`, {
      method: 'POST',
      headers: authHeaders(),
    }))
    await loadHistory()
    Message.success('推荐日报已生成')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '生成推荐日报失败')
  } finally {
    generating.value = false
  }
}

const saveSettings = async () => {
  try {
    savingSettings.value = true
    const saved = await requestJSON<RecommendationSettings>('/api/recommendations/settings', {
      method: 'PUT',
      headers: authHeaders(true),
      body: JSON.stringify(settingsForm),
    })
    Object.assign(settingsForm, saved)
    Message.success('推荐设置已保存')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '保存设置失败')
  } finally {
    savingSettings.value = false
  }
}

const saveSource = async () => {
  if (!sourceForm.url.trim()) {
    Message.warning('请输入内容源 URL')
    return
  }
  try {
    savingSource.value = true
    await requestJSON<DiscoverySource>('/api/discovery/sources', {
      method: 'POST',
      headers: authHeaders(true),
      body: JSON.stringify({ ...sourceForm, enabled: true }),
    })
    sourceForm.name = ''
    sourceForm.url = ''
    sourceForm.type = 'feed'
    await loadSources()
    Message.success('内容源已添加')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '添加内容源失败')
  } finally {
    savingSource.value = false
  }
}

const fetchSource = async (sourceId: number) => {
  try {
    fetchingSourceId.value = sourceId
    await requestJSON(`/api/discovery/sources/${sourceId}/fetch`, { method: 'POST', headers: authHeaders() })
    await Promise.all([loadSources(), loadCandidates()])
    Message.success('内容源刷新完成')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '刷新内容源失败')
  } finally {
    fetchingSourceId.value = null
  }
}

const deleteSource = async (sourceId: number) => {
  try {
    await requestJSON(`/api/discovery/sources/${sourceId}`, { method: 'DELETE', headers: authHeaders() })
    await loadSources()
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '删除内容源失败')
  }
}

const sendFeedback = async (item: RecommendationItem, action: FeedbackAction, blockTargets: BlockTarget[] = []) => {
  try {
    feedbackLoadingId.value = item.id
    await requestJSON(`/api/recommendations/items/${item.id}/feedback`, {
      method: 'POST',
      headers: authHeaders(true),
      body: JSON.stringify({ action, blockTargets }),
    })
    await loadBlocks()
    Message.success('反馈已记录')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '记录反馈失败')
  } finally {
    feedbackLoadingId.value = null
  }
}

const openBlockDialog = (item: RecommendationItem) => {
  blockDialog.item = item
  blockDialog.visible = true
  selectedBlockKeys.value = blockOptions.value.slice(0, 1).map((option) => `${option.type}:${option.value}`)
}

const submitBlockFeedback = async () => {
  if (!blockDialog.item) return
  const targets = blockOptions.value.filter((option) => selectedBlockKeys.value.includes(`${option.type}:${option.value}`))
  if (targets.length === 0) {
    Message.warning('请选择屏蔽范围')
    return
  }
  await sendFeedback(blockDialog.item, 'block', targets)
  blockDialog.visible = false
}

const deleteBlockRule = async (ruleId: number) => {
  try {
    await requestJSON(`/api/recommendations/blocks/${ruleId}`, { method: 'DELETE', headers: authHeaders() })
    await loadBlocks()
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '删除屏蔽规则失败')
  }
}

const openArchive = (path: string) => {
  router.push({ path: '/htmlviewer', query: { loc: path } })
}

const searchKeyword = (keyword: string) => {
  router.push({ path: '/search', query: { q: keyword } })
}

const openCandidate = async (candidate: DiscoveryCandidate) => {
  if (!candidate?.url) return
  try {
    await requestJSON(`/api/discovery/candidates/${candidate.id}/read`, { method: 'POST', headers: authHeaders() })
    window.open(candidate.url, '_blank', 'noopener,noreferrer')
    await loadCandidates()
  } catch {
    window.open(candidate.url, '_blank', 'noopener,noreferrer')
  }
}

const archiveCandidate = async (candidateId: number) => {
  try {
    archivingCandidateId.value = candidateId
    await requestJSON(`/api/discovery/candidates/${candidateId}/archive`, { method: 'POST', headers: authHeaders() })
    await loadCandidates()
    Message.success('已加入归档队列')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '加入归档失败')
  } finally {
    archivingCandidateId.value = null
  }
}

const ignoreCandidate = async (candidateId: number) => {
  try {
    await requestJSON(`/api/discovery/candidates/${candidateId}/ignore`, { method: 'POST', headers: authHeaders() })
    await loadCandidates()
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '忽略候选失败')
  }
}

function emptySnapshot(date: string): RecommendationSnapshot {
  return {
    day: { id: 0, userId: 0, date, timezone: 'Asia/Shanghai', status: 'missing', requestedCount: 10, actualCount: 0 },
    items: [],
  }
}

function normalizeSnapshot(snapshot: RecommendationSnapshot | null | undefined): RecommendationSnapshot {
  const fallback = emptySnapshot(formatLocalDate(new Date()))
  if (!snapshot?.day) return fallback
  return { day: snapshot.day, items: snapshot.items ?? [] }
}

function formatLocalDate(date: Date): string {
  const year = date.getFullYear()
  const month = `${date.getMonth() + 1}`.padStart(2, '0')
  const day = `${date.getDate()}`.padStart(2, '0')
  return `${year}-${month}-${day}`
}

function formatDateTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function parseList(raw?: string): string[] {
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.filter((item) => typeof item === 'string' && item.trim()).map((item) => item.trim()) : []
  } catch {
    return []
  }
}

function sourceHost(rawUrl?: string): string {
  if (!rawUrl) return ''
  try {
    return new URL(rawUrl).hostname
  } catch {
    return rawUrl
  }
}

function blockRuleTypeLabel(type: string): string {
  if (type === 'topic') return '主题'
  if (type === 'source') return '来源'
  if (type === 'style') return '类型'
  return type
}

onMounted(loadAll)
</script>

<style scoped>
.recommendations-view {
  min-height: 100vh;
  padding: 20px;
  background: #f7f8fa;
  color: #1d2129;
  position: relative;
}

.top-bar {
  position: absolute;
  top: 20px;
  right: 20px;
  z-index: 10;
}

.back-button {
  color: #4e5969;
  background: #ffffff;
  border-radius: 8px;
  box-shadow: 0 8px 22px rgba(29, 33, 41, 0.08);
}

.content {
  width: min(1180px, 100%);
  margin: 0 auto;
  padding: 72px 0 40px;
}

.page-header {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  gap: 18px;
  margin-bottom: 18px;
  padding: 20px 22px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  background: #ffffff;
  box-shadow: 0 12px 30px rgba(29, 33, 41, 0.06);
}

.header-mark {
  width: 52px;
  height: 52px;
  display: grid;
  place-items: center;
  border-radius: 8px;
  background: #165dff;
  color: #ffffff;
  font-size: 28px;
}

.page-header h1 {
  margin: 0 0 6px;
  font-size: 28px;
  color: #111827;
}

.page-header p {
  margin: 0;
  color: #64748b;
  line-height: 1.5;
}

.tabs {
  padding: 18px 22px 24px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  background: #ffffff;
  box-shadow: 0 12px 30px rgba(29, 33, 41, 0.06);
}

.panel {
  padding: 20px 0;
}

.section-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
}

.section-title h2 {
  margin: 0 0 4px;
  font-size: 20px;
}

.section-title span,
.item-meta,
.compact-card span,
.source-row span,
.source-row small,
.recommendation-card small {
  color: #86909c;
  font-size: 13px;
}

.recommendation-list,
.item-list,
.source-list,
.history-detail {
  display: grid;
  gap: 12px;
}

.recommendation-card,
.compact-card,
.source-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: start;
  gap: 18px;
  min-height: 126px;
  padding: 16px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  background: #ffffff;
}

.item-main {
  min-width: 0;
}

.item-main h3,
.source-row h3 {
  margin: 0 0 8px;
  font-size: 17px;
  line-height: 1.35;
  overflow-wrap: anywhere;
}

.item-main p,
.source-row p {
  min-height: 44px;
  display: -webkit-box;
  margin: 0 0 8px;
  overflow: hidden;
  color: #4e5969;
  line-height: 1.55;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.item-meta,
.topic-row,
.feedback-row,
.item-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.item-meta {
  margin-bottom: 8px;
}

.topic-row {
  min-height: 24px;
  margin: 8px 0;
}

.topic-row span {
  padding: 2px 8px;
  border-radius: 6px;
  background: #eef2ff;
  color: #1d4ed8;
  font-size: 12px;
}

.item-actions {
  width: 236px;
  justify-content: flex-end;
}

.feedback-row {
  justify-content: flex-end;
}

.history-layout {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  gap: 16px;
}

.day-list {
  display: grid;
  align-content: start;
  gap: 8px;
}

.day-list button,
.keyword-tile {
  display: grid;
  gap: 6px;
  padding: 12px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  background: #ffffff;
  color: #1d2129;
  cursor: pointer;
  text-align: left;
}

.day-list button.active,
.day-list button:hover,
.keyword-tile:hover {
  border-color: #165dff;
  box-shadow: inset 3px 0 0 #165dff;
}

.settings-grid {
  display: grid;
  grid-template-columns: minmax(260px, 360px) minmax(0, 1fr);
  gap: 24px;
}

.settings-form,
.block-list {
  display: grid;
  align-content: start;
  gap: 14px;
}

.settings-form h2,
.block-list h2 {
  margin: 0;
  font-size: 20px;
}

.source-panel {
  border-bottom: 1px solid #e5e6eb;
}

.source-form {
  display: grid;
  grid-template-columns: minmax(160px, 1fr) minmax(280px, 2fr) minmax(110px, 130px) auto;
  align-items: end;
  gap: 12px;
  margin-bottom: 16px;
}

.source-field,
.inline-field {
  display: grid;
  gap: 6px;
  color: #4e5969;
  font-size: 13px;
}

.source-field input,
.source-field select,
.inline-field input,
.inline-field select {
  height: 32px;
  padding: 0 10px;
  border: 1px solid #c9cdd4;
  border-radius: 6px;
  background: #ffffff;
  color: #1d2129;
}

.source-field input:focus,
.source-field select:focus,
.inline-field input:focus,
.inline-field select:focus {
  border-color: #165dff;
  outline: none;
  box-shadow: 0 0 0 2px rgba(22, 93, 255, 0.12);
}

.toggle-row,
.block-row,
.block-options label {
  display: flex;
  align-items: center;
  gap: 10px;
}

.block-row {
  justify-content: space-between;
  padding: 12px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
}

.block-options {
  display: grid;
  gap: 12px;
}

.keyword-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 12px;
}

@media (max-width: 820px) {
  .page-header,
  .recommendation-card,
  .compact-card,
  .source-row,
  .history-layout,
  .settings-grid,
  .source-form {
    grid-template-columns: 1fr;
  }

  .section-title {
    align-items: flex-start;
    flex-direction: column;
  }

  .item-actions {
    width: 100%;
    justify-content: flex-start;
  }

  .top-bar {
    position: static;
    display: flex;
    justify-content: flex-end;
    margin-bottom: 12px;
  }

  .content {
    padding-top: 0;
  }
}
</style>
