<template>
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
        <a-button type="primary" @click="$emit('open-archive', item.path)">
          <template #icon><icon-eye /></template>
          查看
        </a-button>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
// 基于点击与搜索词的归档推荐。
import { requestJSON } from '@/api/client'
import type { ArchiveRecommendationItem } from '@/components/recommendations/types'
import { IconEye } from '@arco-design/web-vue/es/icon'
import { ref } from 'vue'

defineEmits<{ 'open-archive': [path: string] }>()

const archiveRecommendations = ref<ArchiveRecommendationItem[]>([])

async function loadArchiveRecommendations() {
  archiveRecommendations.value = (await requestJSON<ArchiveRecommendationItem[]>('/api/recommendations/archives?window=7d&limit=20')) ?? []
}

defineExpose({ reload: loadArchiveRecommendations })
</script>

<style scoped src="./panel-shared.css"></style>
