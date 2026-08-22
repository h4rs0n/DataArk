<template>
  <section class="panel">
    <div class="section-title">
      <div>
        <h2>猜你喜欢</h2>
        <span>根据阅读与反馈偏好，每次推荐最多 10 篇符合条件的文章</span>
      </div>
      <a-button @click="loadDiscoveryFeed">
        <template #icon><icon-refresh /></template>
        刷新
      </a-button>
    </div>
    <a-spin :loading="discoveryFeedLoading" class="discovery-feed-spin">
      <a-empty v-if="discoveryFeed.items.length === 0" description="暂无符合推荐条件的文章" />
      <div v-else class="recommendation-list">
        <RecommendationArticleCard
          v-for="item in discoveryFeed.items"
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
      <p v-if="discoveryFeed.batch && discoveryFeed.batch.actualCount < discoveryFeed.batch.requestedCount" class="feed-shortage">
        当前仅有 {{ discoveryFeed.batch.actualCount }} 篇符合推荐条件
      </p>
      <div class="feed-refresh-row">
        <a-button type="primary" :loading="refreshingDiscoveryFeed" @click="refreshDiscoveryFeedBatch">
          <template #icon><icon-refresh /></template>
          换一换
        </a-button>
      </div>
    </a-spin>
  </section>
</template>

<script setup lang="ts">
// 猜你喜欢：个性化发现流与换一批。
import RecommendationArticleCard from '@/components/recommendations/RecommendationArticleCard.vue'
import { authHeaders, requestJSON } from '@/components/recommendations/http'
import type {
  BlockRuleType,
  DiscoveryCandidate,
  FeedbackAction,
  FeedbackByItem,
  RecommendationFeedSnapshot,
  RecommendationItem,
} from '@/components/recommendations/types'
import { IconRefresh } from '@arco-design/web-vue/es/icon'
import { Message } from '@arco-design/web-vue'
import { ref } from 'vue'

const props = defineProps<{
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

const discoveryFeedLoading = ref(false)
const refreshingDiscoveryFeed = ref(false)
const discoveryFeed = ref<RecommendationFeedSnapshot>({ batch: null, items: [] })

async function loadDiscoveryFeed() {
  try {
    discoveryFeedLoading.value = true
    let snapshot = await requestJSON<RecommendationFeedSnapshot>('/api/recommendations/discovery-feed', { headers: authHeaders() })
    if (!snapshot?.batch) {
      snapshot = await requestJSON<RecommendationFeedSnapshot>('/api/recommendations/discovery-feed/refresh', { method: 'POST', headers: authHeaders() })
    }
    discoveryFeed.value = { batch: snapshot?.batch ?? null, items: snapshot?.items ?? [] }
    await Promise.all(discoveryFeed.value.items.map((item) => props.loadItemFeedback(item)))
  } finally {
    discoveryFeedLoading.value = false
  }
}

async function refreshDiscoveryFeedBatch() {
  try {
    refreshingDiscoveryFeed.value = true
    const snapshot = await requestJSON<RecommendationFeedSnapshot>('/api/recommendations/discovery-feed/refresh', { method: 'POST', headers: authHeaders() })
    discoveryFeed.value = { batch: snapshot?.batch ?? null, items: snapshot?.items ?? [] }
    await Promise.all(discoveryFeed.value.items.map((item) => props.loadItemFeedback(item)))
    Message.success('已换一批猜你喜欢')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '换一换失败')
  } finally {
    refreshingDiscoveryFeed.value = false
  }
}

defineExpose({ reload: loadDiscoveryFeed })
</script>

<style scoped src="./panel-shared.css"></style>
<style scoped>
.discovery-feed-spin {
  display: block;
  width: 100%;
  min-height: 180px;
}

.feed-shortage {
  margin: 14px 0 0;
  color: #86909c;
  font-size: 13px;
  text-align: center;
}

.feed-refresh-row {
  display: flex;
  justify-content: center;
  margin-top: 18px;
}
</style>
