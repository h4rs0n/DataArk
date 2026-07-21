<template>
  <article class="recommendation-card">
    <div class="item-main">
      <div class="item-meta">
        <span>#{{ item.rank }}</span>
        <span>{{ item.candidate.sourceName || sourceHost(item.candidate.url) }}</span>
        <span v-if="item.candidate.publishedAt">{{ formatDateTime(item.candidate.publishedAt) }}</span>
      </div>
      <h3>
        <a
          class="candidate-title-link"
          :href="item.candidate.url"
          target="_blank"
          rel="noopener noreferrer"
          @click="$emit('mark-read', item.candidate)"
        >
          {{ item.candidate.title || `候选文章 ${item.candidateId}` }}
        </a>
      </h3>
      <p>{{ item.candidate.summary || item.candidate.url }}</p>
      <div class="topic-row">
        <span v-for="topic in parseList(item.candidate.topics).slice(0, 4)" :key="topic">{{ topic }}</span>
        <span v-if="item.poolType">{{ item.poolType }}</span>
        <span v-if="item.explorationReason">探索：{{ item.explorationReason }}</span>
      </div>
      <small>{{ item.reason || '基于内容质量和反馈画像推荐' }}</small>
    </div>
    <div class="item-actions">
      <a-button @click="$emit('open', item.candidate)">
        <template #icon><icon-link /></template>
        原文
      </a-button>
      <a-button type="primary" :loading="archiving" @click="$emit('archive', item.candidateId)">
        <template #icon><icon-storage /></template>
        入库
      </a-button>
      <a-button size="small" @click="$emit('context', item)">查看发现路径</a-button>
      <FeedbackControls
        :item-id="item.id"
        :current-action="currentAction"
        :loading="feedbackLoading"
        @select="(action) => $emit('feedback', action)"
        @scope="(scope) => $emit('scope', scope)"
        @revert="$emit('revert')"
      />
    </div>
  </article>
</template>

<script setup lang="ts">
import FeedbackControls from '@/components/recommendations/FeedbackControls.vue'
import { IconLink, IconStorage } from '@arco-design/web-vue/es/icon'

type FeedbackAction = 'valuable' | 'not_interested' | 'too_repetitive' | 'low_value'
type BlockRuleType = 'topic' | 'source' | 'style'

interface CardCandidate {
  id: number
  sourceName: string
  url: string
  title: string
  summary: string
  status: string
  score: number
  topics?: string
  publishedAt?: string
}

interface CardItem {
  id: number
  candidateId: number
  rank: number
  reason: string
  rerankScore: number
  poolType?: string
  explorationReason?: string
  candidate: CardCandidate
}

defineProps<{
  item: CardItem
  archiving?: boolean
  currentAction?: string
  feedbackLoading?: boolean
}>()

defineEmits<{
  open: [candidate: CardCandidate]
  archive: [candidateId: number]
  context: [item: CardItem]
  'mark-read': [candidate: CardCandidate]
  feedback: [action: FeedbackAction]
  scope: [scope: BlockRuleType]
  revert: []
}>()

const parseList = (value?: string): string[] => {
  if (!value) return []
  try {
    const parsed = JSON.parse(value)
    if (Array.isArray(parsed)) return parsed.map(String)
  } catch { /* Fall through to comma-separated legacy values. */ }
  return value.split(',').map((item) => item.trim()).filter(Boolean)
}

const sourceHost = (rawURL: string) => {
  try { return new URL(rawURL).hostname }
  catch { return rawURL }
}

const formatDateTime = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })
</script>

<style scoped>
.recommendation-card {
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

.item-main { min-width: 0; }

.item-main h3 {
  margin: 0 0 8px;
  font-size: 17px;
  line-height: 1.35;
  overflow-wrap: anywhere;
}

.candidate-title-link {
  color: #1d2129;
  text-decoration: none;
  transition: color 0.2s ease;
}

.candidate-title-link:hover,
.candidate-title-link:focus-visible {
  color: #165dff;
  text-decoration: underline;
  text-underline-offset: 3px;
}

.item-main p {
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
.item-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.item-meta,
small {
  color: #86909c;
  font-size: 13px;
}

.item-meta { margin-bottom: 8px; }

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

@media (max-width: 820px) {
  .recommendation-card { grid-template-columns: 1fr; }
  .item-actions { width: 100%; justify-content: flex-start; }
}
</style>
