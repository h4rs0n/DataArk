<template>
  <section class="panel">
    <div class="section-title">
      <h2>网页归档文件推荐</h2>
      <span>由模型根据近期归档选出</span>
    </div>
    <a-empty v-if="loadError" :description="loadError" />
    <a-empty v-else-if="archiveRecommendations.length === 0" description="暂无可推荐归档" />
    <div v-else class="item-list">
      <article v-for="item in archiveRecommendations" :key="item.path" class="compact-card">
        <div class="item-main">
          <h3>{{ item.title || item.fileName }}</h3>
          <p>{{ item.summary || item.path }}</p>
          <span>{{ item.domain }} · {{ item.reason }}</span>
        </div>
        <a-button type="primary" @click="$emit('open-archive', item.path)">
          <template #icon><icon-eye /></template>
          查看
        </a-button>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
// 归档推荐强制走 LLM；失败展示错误态，不回退关键词分。
import { requestJSON } from '@/api/client'
import type { ArchiveRecommendationItem } from '@/components/recommendations/types'
import { IconEye } from '@arco-design/web-vue/es/icon'
import { ref } from 'vue'

defineEmits<{ 'open-archive': [path: string] }>()

const archiveRecommendations = ref<ArchiveRecommendationItem[]>([])
const loadError = ref('')

async function loadArchiveRecommendations() {
  loadError.value = ''
  try {
    archiveRecommendations.value = (await requestJSON<ArchiveRecommendationItem[]>('/api/recommendations/archives?window=7d&limit=20')) ?? []
  } catch (error) {
    archiveRecommendations.value = []
    loadError.value = error instanceof Error ? error.message : '归档推荐失败'
  }
}

defineExpose({ reload: loadArchiveRecommendations })
</script>

<style scoped src="./panel-shared.css"></style>
