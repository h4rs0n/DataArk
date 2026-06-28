<template>
  <div class="recommendations-view">
    <div class="back-button-container">
      <a-button type="text" class="back-button" @click="router.push('/')">
        <template #icon><icon-arrow-left /></template>
        返回首页
      </a-button>
    </div>

    <main class="content">
      <div class="header-section">
        <div class="icon-wrapper">
          <icon-bulb class="header-icon" />
        </div>
        <h1 class="page-title">推荐中心</h1>
        <p class="page-subtitle">查看近期使用趋势，管理外部博客源，并把感兴趣的文章加入归档。</p>
      </div>

      <div class="toolbar-band">
        <div>
          <strong>推荐数据</strong>
          <span>点击、搜索和外部内容发现</span>
        </div>
        <a-button :loading="loading" @click="loadAll">
          <template #icon><icon-refresh /></template>
          刷新
        </a-button>
      </div>

      <a-tabs v-model:active-key="activeTab" class="tabs">
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
              <article v-for="item in rankings" :key="item.path" class="archive-item">
                <div class="item-content">
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

        <a-tab-pane key="recommended" title="归档推荐">
          <section class="panel">
            <div class="section-title">
              <h2>网页归档文件推荐</h2>
              <span>基于点击、搜索词和内容元数据</span>
            </div>
            <a-empty v-if="recommendations.length === 0" description="暂无可推荐归档" />
            <div v-else class="item-list">
              <article v-for="item in recommendations" :key="item.path" class="archive-item">
                <div class="item-content">
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

        <a-tab-pane key="discovery" title="内容发现">
          <section class="panel source-panel">
            <div class="section-title">
              <h2>外部博客源</h2>
              <span>RSS/Atom 或同站发现</span>
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
                <div class="item-content">
                  <strong>{{ source.name }}</strong>
                  <span>{{ source.type }} · {{ source.url }}</span>
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
              <h2>候选文章</h2>
              <label class="status-select" for="candidate-status">
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
              <article v-for="candidate in candidates" :key="candidate.id" class="candidate-item">
                <div class="item-content">
                  <h3>{{ candidate.title }}</h3>
                  <p>{{ candidate.summary || candidate.url }}</p>
                  <span>{{ candidate.sourceName }} · {{ candidate.status }} · 分数 {{ candidate.score.toFixed(1) }}</span>
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
      </a-tabs>
    </main>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Message, Notification } from '@arco-design/web-vue'
import {
  IconArrowLeft,
  IconBulb,
  IconClose,
  IconDelete,
  IconEye,
  IconLink,
  IconPlus,
  IconRefresh,
  IconStorage,
  IconSync,
} from '@arco-design/web-vue/es/icon'

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
}

const router = useRouter()
const activeTab = ref('ranking')
const loading = ref(false)
const rankingWindow = ref<'7d' | 'all'>('7d')
const keywordWindow = ref<'7d' | 'all'>('7d')
const candidateStatus = ref('new')
const rankings = ref<ArchiveRankingItem[]>([])
const recommendations = ref<ArchiveRecommendationItem[]>([])
const keywords = ref<KeywordItem[]>([])
const sources = ref<DiscoverySource[]>([])
const candidates = ref<DiscoveryCandidate[]>([])
const savingSource = ref(false)
const fetchingSourceId = ref<number | null>(null)
const archivingCandidateId = ref<number | null>(null)
const sourceForm = reactive({
  name: '',
  url: '',
  type: 'feed',
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

const loadRankings = async () => {
  rankings.value = (await requestJSON<ArchiveRankingItem[]>(`/api/archive/rankings?window=${rankingWindow.value}&limit=20`, {
    headers: authHeaders(),
  })) ?? []
}

const loadKeywords = async () => {
  keywords.value = (await requestJSON<KeywordItem[]>(`/api/search/keywords?window=${keywordWindow.value}&limit=30`, {
    headers: authHeaders(),
  })) ?? []
}

const loadRecommendations = async () => {
  recommendations.value = (await requestJSON<ArchiveRecommendationItem[]>('/api/recommendations/archives?window=7d&limit=20', {
    headers: authHeaders(),
  })) ?? []
}

const loadSources = async () => {
  sources.value = (await requestJSON<DiscoverySource[]>('/api/discovery/sources', {
    headers: authHeaders(),
  })) ?? []
}

const loadCandidates = async () => {
  candidates.value = (await requestJSON<DiscoveryCandidate[]>(`/api/discovery/candidates?status=${candidateStatus.value}&limit=80`, {
    headers: authHeaders(),
  })) ?? []
}

const loadAll = async () => {
  try {
    loading.value = true
    await Promise.all([loadRankings(), loadKeywords(), loadRecommendations(), loadSources(), loadCandidates()])
  } catch (error) {
    Notification.error({
      title: '加载失败',
      content: error instanceof Error ? error.message : '推荐中心加载失败',
      position: 'topRight',
    })
  } finally {
    loading.value = false
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
    await requestJSON(`/api/discovery/sources/${sourceId}/fetch`, {
      method: 'POST',
      headers: authHeaders(),
    })
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
    await requestJSON(`/api/discovery/sources/${sourceId}`, {
      method: 'DELETE',
      headers: authHeaders(),
    })
    await loadSources()
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '删除内容源失败')
  }
}

const openArchive = (path: string) => {
  router.push({ path: '/htmlviewer', query: { loc: path } })
}

const searchKeyword = (keyword: string) => {
  router.push({ path: '/search', query: { q: keyword } })
}

const openCandidate = async (candidate: DiscoveryCandidate) => {
  try {
    await requestJSON(`/api/discovery/candidates/${candidate.id}/read`, {
      method: 'POST',
      headers: authHeaders(),
    })
    window.open(candidate.url, '_blank', 'noopener,noreferrer')
    await loadCandidates()
  } catch {
    window.open(candidate.url, '_blank', 'noopener,noreferrer')
  }
}

const archiveCandidate = async (candidateId: number) => {
  try {
    archivingCandidateId.value = candidateId
    await requestJSON(`/api/discovery/candidates/${candidateId}/archive`, {
      method: 'POST',
      headers: authHeaders(),
    })
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
    await requestJSON(`/api/discovery/candidates/${candidateId}/ignore`, {
      method: 'POST',
      headers: authHeaders(),
    })
    await loadCandidates()
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '忽略候选失败')
  }
}

onMounted(loadAll)
</script>

<style scoped>
.recommendations-view {
  min-height: 100vh;
  padding: 20px;
  background:
    linear-gradient(180deg, rgba(242, 247, 255, 0.9) 0%, rgba(248, 250, 252, 1) 42%),
    #f8fafc;
  color: #1d2129;
  position: relative;
}

.back-button-container {
  position: absolute;
  top: 20px;
  right: 20px;
  z-index: 10;
}

.back-button {
  color: #4b5563;
  background: rgba(255, 255, 255, 0.88);
  border-radius: 8px;
  padding: 8px 14px;
  box-shadow: 0 8px 24px rgba(15, 23, 42, 0.08);
}

.back-button:hover {
  color: #1d4ed8;
  background: #ffffff;
}

.content {
  width: min(1120px, 100%);
  margin: 0 auto;
  padding: 72px 0 40px;
}

.header-section {
  text-align: center;
  margin-bottom: 32px;
}

.icon-wrapper {
  width: 72px;
  height: 72px;
  border-radius: 18px;
  background: linear-gradient(135deg, #2563eb, #059669);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 16px;
  box-shadow: 0 16px 34px rgba(37, 99, 235, 0.22);
}

.header-icon {
  font-size: 36px;
  color: #ffffff;
}

.page-title {
  font-size: 32px;
  font-weight: 700;
  color: #111827;
  margin: 0 0 8px;
}

.page-subtitle {
  font-size: 16px;
  color: #64748b;
  margin: 0;
  line-height: 1.6;
}

.toolbar-band {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
  padding: 18px 20px;
  border: 1px solid rgba(203, 213, 225, 0.72);
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.94);
  box-shadow: 0 14px 36px rgba(15, 23, 42, 0.08);
}

.toolbar-band div {
  display: grid;
  gap: 4px;
}

.toolbar-band strong {
  font-size: 17px;
  color: #111827;
}

.toolbar-band span {
  color: #64748b;
  font-size: 14px;
}

.tabs {
  padding: 20px 24px 24px;
  border: 1px solid rgba(203, 213, 225, 0.72);
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.94);
  box-shadow: 0 14px 36px rgba(15, 23, 42, 0.08);
}

.panel {
  padding: 22px 0;
}

.section-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  margin-bottom: 16px;
}

.section-title h2 {
  margin: 0;
  font-size: 20px;
}

.section-title span {
  color: #86909c;
}

.item-list,
.source-list {
  display: grid;
  gap: 12px;
}

.archive-item,
.candidate-item,
.source-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  padding: 16px;
  background: #ffffff;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
}

.item-content {
  min-width: 0;
  flex: 1;
}

.archive-item > .arco-btn,
.source-row > .arco-space,
.candidate-actions {
  flex: 0 0 auto;
}

/* Keep RSS action buttons anchored while summaries wrap to different heights. */
.candidate-actions {
  align-self: flex-start;
}

.archive-item h3,
.candidate-item h3 {
  margin: 0 0 8px;
  font-size: 17px;
  line-height: 1.35;
}

.archive-item p,
.candidate-item p {
  display: -webkit-box;
  margin: 0 0 8px;
  overflow: hidden;
  color: #4e5969;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.archive-item span,
.candidate-item span,
.source-row span,
.source-row small {
  display: block;
  color: #86909c;
  font-size: 13px;
}

.keyword-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 12px;
}

.keyword-tile {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 14px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  background: #ffffff;
  color: #1d2129;
  cursor: pointer;
  text-align: left;
}

.keyword-tile:hover {
  border-color: #165dff;
}

.keyword-tile span {
  color: #86909c;
  font-size: 13px;
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
.status-select {
  display: grid;
  gap: 6px;
  color: #4e5969;
  font-size: 13px;
}

.source-field input,
.source-field select,
.status-select select {
  height: 32px;
  padding: 0 10px;
  border: 1px solid #c9cdd4;
  border-radius: 6px;
  background: #ffffff;
  color: #1d2129;
}

.source-field input:focus,
.source-field select:focus,
.status-select select:focus {
  border-color: #165dff;
  outline: none;
  box-shadow: 0 0 0 2px rgba(22, 93, 255, 0.12);
}

@media (max-width: 760px) {
  .archive-item,
  .candidate-item,
  .source-row {
    align-items: stretch;
    flex-direction: column;
  }

  .section-title {
    align-items: flex-start;
    flex-direction: column;
  }

  .source-form {
    grid-template-columns: 1fr;
  }

  .toolbar-band {
    align-items: stretch;
    flex-direction: column;
  }

  .candidate-actions {
    width: 100%;
  }
}
</style>
