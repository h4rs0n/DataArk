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
          <p>发现、评估、推荐三条流水线独立运行：文章爬取自动执行，LLM 评估需手动启动，推荐只消费已评估库存。</p>
        </div>
        <a-button :loading="loading" @click="loadAll">
          <template #icon><icon-refresh /></template>
          刷新
        </a-button>
      </header>

      <a-tabs v-model:active-key="moduleTab" class="tabs module-tabs">
        <a-tab-pane key="recommend" title="推荐">
          <a-tabs v-model:active-key="activeTab" class="tabs">
            <a-tab-pane key="feed" title="推荐中心">
              <DiscoveryFeedPanel
                ref="feedPanel"
                :feedback-by-item="feedbackByItem"
                :archiving-candidate-id="archivingCandidateId"
                :feedback-loading-id="feedbackLoadingId"
                :load-item-feedback="loadItemFeedback"
                @open="openCandidate"
                @archive="archiveCandidate"
                @context="openItemContext"
                @mark-read="markCandidateRead"
                @feedback="sendFeedback"
                @scope="openImpactDialog"
                @revert="revertFeedback"
              />
            </a-tab-pane>

            <a-tab-pane key="today" title="每日推荐">
              <DailyDigestPanel
                ref="digestPanel"
                :is-owner="isOwner"
                :feedback-by-item="feedbackByItem"
                :archiving-candidate-id="archivingCandidateId"
                :feedback-loading-id="feedbackLoadingId"
                :load-item-feedback="loadItemFeedback"
                @open="openCandidate"
                @archive="archiveCandidate"
                @context="openItemContext"
                @mark-read="markCandidateRead"
                @feedback="sendFeedback"
                @scope="openImpactDialog"
                @revert="revertFeedback"
              />
            </a-tab-pane>

            <a-tab-pane key="settings" title="推荐设置">
              <SettingsPanel ref="settingsPanel" />
            </a-tab-pane>

            <a-tab-pane key="ranking" title="点击排行">
              <ArchiveRankingPanel ref="rankingPanel" @open-archive="openArchive" />
            </a-tab-pane>

            <a-tab-pane key="keywords" title="搜索热词">
              <SearchKeywordsPanel ref="keywordsPanel" @search-keyword="searchKeyword" />
            </a-tab-pane>

            <a-tab-pane key="archives" title="归档推荐">
              <ArchiveRecommendPanel ref="archivesPanel" @open-archive="openArchive" />
            </a-tab-pane>
          </a-tabs>
        </a-tab-pane>

        <a-tab-pane key="discover" title="发现">
          <a-tabs v-model:active-key="discoveryTab" class="tabs">
            <a-tab-pane key="discovery" title="内容发现">
              <DiscoverySourcesPanel ref="sourcesPanel" :is-owner="isOwner" @inventory-changed="reloadInventory" />
            </a-tab-pane>

            <a-tab-pane v-if="isOwner" key="queue" title="任务队列">
              <CrawlQueuePanel
                ref="queuePanel"
                :active="moduleTab === 'discover' && discoveryTab === 'queue'"
                :is-owner="isOwner"
                @inventory-changed="reloadInventory"
              />
            </a-tab-pane>
          </a-tabs>
        </a-tab-pane>

        <a-tab-pane v-if="isOwner" key="assess" title="评估">
          <!-- 独立包装，避免 Arco Spin 把评估模块挤成左侧窄列 -->
          <div class="assess-module">
            <AssessmentMetricsPanel :active="moduleTab === 'assess'" />
            <ArticleAssessmentWorkflow :active="moduleTab === 'assess'" />
          </div>
        </a-tab-pane>
      </a-tabs>
    </main>

    <ImpactFeedbackModal
      v-model:visible="impactDialog.visible"
      :item="impactDialog.item"
      :scope="impactDialog.scope"
      :loading="feedbackLoadingId === impactDialog.item?.id"
      @update:selected="selectedImpactValue = $event"
      @submit="submitImpactFeedback"
    />

    <RecommendationContextDrawer
      v-model:visible="contextDrawerVisible"
      :loading="contextLoading"
      :item-context="itemContext"
    />
  </div>
</template>

<script setup lang="ts">
// 推荐中心页签壳：组装推荐/发现/评估模块，并处理跨面板的反馈与归档。
import ArchiveRankingPanel from '@/components/recommendations/ArchiveRankingPanel.vue'
import ArchiveRecommendPanel from '@/components/recommendations/ArchiveRecommendPanel.vue'
import ArticleAssessmentWorkflow from '@/components/recommendations/ArticleAssessmentWorkflow.vue'
import AssessmentMetricsPanel from '@/components/recommendations/AssessmentMetricsPanel.vue'
import CrawlQueuePanel from '@/components/recommendations/CrawlQueuePanel.vue'
import DailyDigestPanel from '@/components/recommendations/DailyDigestPanel.vue'
import DiscoveryFeedPanel from '@/components/recommendations/DiscoveryFeedPanel.vue'
import DiscoverySourcesPanel from '@/components/recommendations/DiscoverySourcesPanel.vue'
import ImpactFeedbackModal from '@/components/recommendations/ImpactFeedbackModal.vue'
import RecommendationContextDrawer from '@/components/recommendations/RecommendationContextDrawer.vue'
import SearchKeywordsPanel from '@/components/recommendations/SearchKeywordsPanel.vue'
import SettingsPanel from '@/components/recommendations/SettingsPanel.vue'
import { authHeaders, requestJSON } from '@/components/recommendations/http'
import type {
  BlockRuleType,
  DiscoveryCandidate,
  FeedbackAction,
  RecommendationItem,
} from '@/components/recommendations/types'
import { IconArrowLeft, IconRefresh, IconRobot } from '@arco-design/web-vue/es/icon'
import { Message, Notification } from '@arco-design/web-vue'
import { nextTick, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'

const router = useRouter()
const moduleTab = ref('recommend')
const discoveryTab = ref('discovery')
const activeTab = ref('feed')
const loading = ref(false)
const archivingCandidateId = ref<number | null>(null)
const feedbackLoadingId = ref<number | null>(null)
const feedbackByItem = reactive<Record<number, { action: string } | undefined>>({})
const isOwner = ref(false)
const contextDrawerVisible = ref(false)
const contextLoading = ref(false)
const itemContext = ref<any>()
const impactDialog = reactive<{ visible: boolean; item: RecommendationItem | null; scope: BlockRuleType }>({ visible: false, item: null, scope: 'source' })
const selectedImpactValue = ref('')

const feedPanel = ref<{ reload: () => Promise<void> }>()
const digestPanel = ref<{ reload: () => Promise<void> }>()
const settingsPanel = ref<{ reload: () => Promise<unknown>; loadBlocks: () => Promise<void> }>()
const rankingPanel = ref<{ reload: () => Promise<void> }>()
const keywordsPanel = ref<{ reload: () => Promise<void> }>()
const archivesPanel = ref<{ reload: () => Promise<void> }>()
const sourcesPanel = ref<{ reload: () => Promise<void> }>()
const queuePanel = ref<{ reload: () => Promise<void> }>()

async function loadIdentity() {
  const user = await requestJSON<{ role?: string }>('/api/authChecker', { headers: authHeaders() })
  isOwner.value = user?.role === 'owner'
}

async function loadItemFeedback(item: RecommendationItem) {
  const data = await requestJSON<{ current?: { action: string } }>(`/api/recommendations/items/${item.id}/feedback`, { headers: authHeaders() })
  feedbackByItem[item.id] = data?.current ? { action: data.current.action === 'duplicate' ? 'too_repetitive' : data.current.action } : undefined
}

async function reloadInventory() {
  await Promise.all([
    feedPanel.value?.reload() ?? Promise.resolve(),
    sourcesPanel.value?.reload() ?? Promise.resolve(),
  ])
}

async function loadAll() {
  try {
    loading.value = true
    await loadIdentity()
    await nextTick()
    await Promise.all([
      feedPanel.value?.reload() ?? Promise.resolve(),
      digestPanel.value?.reload() ?? Promise.resolve(),
      settingsPanel.value?.reload() ?? Promise.resolve(),
      rankingPanel.value?.reload() ?? Promise.resolve(),
      keywordsPanel.value?.reload() ?? Promise.resolve(),
      archivesPanel.value?.reload() ?? Promise.resolve(),
      sourcesPanel.value?.reload() ?? Promise.resolve(),
    ])
    if (moduleTab.value === 'discover' && discoveryTab.value === 'queue' && isOwner.value) {
      await queuePanel.value?.reload()
    }
  } catch (error) {
    Notification.error({ title: '加载失败', content: error instanceof Error ? error.message : '推荐中心加载失败', position: 'topRight' })
  } finally {
    loading.value = false
  }
}

async function sendFeedback(item: RecommendationItem, action: FeedbackAction, blockTargets: { type: BlockRuleType; value: string }[] = []) {
  try {
    feedbackLoadingId.value = item.id
    await requestJSON(`/api/recommendations/items/${item.id}/feedback`, {
      method: 'POST',
      headers: authHeaders(true),
      body: JSON.stringify({ action, blockTargets }),
    })
    await Promise.all([settingsPanel.value?.loadBlocks() ?? Promise.resolve(), loadItemFeedback(item)])
    Message.success('反馈已记录')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '记录反馈失败')
  } finally {
    feedbackLoadingId.value = null
  }
}

async function revertFeedback(item: RecommendationItem) {
  try {
    feedbackLoadingId.value = item.id
    await requestJSON(`/api/recommendations/items/${item.id}/feedback`, { method: 'DELETE', headers: authHeaders() })
    feedbackByItem[item.id] = undefined
    await settingsPanel.value?.loadBlocks()
    Message.success('当前反馈已撤销，历史事件仍保留')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '撤销反馈失败')
  } finally {
    feedbackLoadingId.value = null
  }
}

function openImpactDialog(item: RecommendationItem, scope: BlockRuleType) {
  impactDialog.item = item
  impactDialog.scope = scope
  impactDialog.visible = true
}

async function submitImpactFeedback() {
  if (!impactDialog.item) return
  if (!selectedImpactValue.value) {
    Message.warning('请选择影响范围')
    return
  }
  const action: FeedbackAction = impactDialog.scope === 'source' ? 'block_source' : impactDialog.scope === 'topic' ? 'reduce_topic' : 'reduce_style'
  await sendFeedback(impactDialog.item, action, [{ type: impactDialog.scope, value: selectedImpactValue.value }])
  impactDialog.visible = false
}

async function openItemContext(item: RecommendationItem) {
  contextDrawerVisible.value = true
  contextLoading.value = true
  itemContext.value = undefined
  try {
    itemContext.value = await requestJSON<any>(`/api/recommendations/items/${item.id}/context`, { headers: authHeaders() })
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '加载追溯信息失败')
  } finally {
    contextLoading.value = false
  }
}

function openArchive(path: string) {
  router.push({ path: '/htmlviewer', query: { loc: path } })
}

function searchKeyword(keyword: string) {
  router.push({ path: '/search', query: { q: keyword } })
}

async function markCandidateRead(candidate: DiscoveryCandidate) {
  if (!candidate?.url) return
  try {
    await requestJSON(`/api/discovery/candidates/${candidate.id}/read`, { method: 'POST', headers: authHeaders() })
  } catch { /* Opening the article should not depend on recording read state. */ }
}

function openCandidate(candidate: DiscoveryCandidate) {
  if (!candidate?.url) return
  window.open(candidate.url, '_blank', 'noopener,noreferrer')
  void markCandidateRead(candidate)
}

async function archiveCandidate(candidateId: number) {
  try {
    archivingCandidateId.value = candidateId
    await requestJSON(`/api/discovery/candidates/${candidateId}/archive`, { method: 'POST', headers: authHeaders() })
    await feedPanel.value?.reload()
    Message.success('已加入归档队列')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '加入归档失败')
  } finally {
    archivingCandidateId.value = null
  }
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

/* 评估页占满页签内容区，子组件卡片纵向排列 */
.assess-module {
  display: grid;
  gap: 16px;
  width: 100%;
  min-width: 0;
}

.module-tabs :deep(.arco-tabs-content),
.module-tabs :deep(.arco-tabs-pane),
.assess-module :deep(.arco-spin) {
  display: block;
  width: 100%;
  min-width: 0;
}

@media (max-width: 820px) {
  .page-header {
    grid-template-columns: 1fr;
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
