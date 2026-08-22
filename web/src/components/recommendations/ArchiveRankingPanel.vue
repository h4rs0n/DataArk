<template>
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
        <a-button type="primary" @click="$emit('open-archive', item.path)">
          <template #icon><icon-eye /></template>
          查看
        </a-button>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
// 归档点击排行。
import { authHeaders, requestJSON } from '@/components/recommendations/http'
import type { ArchiveRankingItem } from '@/components/recommendations/types'
import { IconEye } from '@arco-design/web-vue/es/icon'
import { ref } from 'vue'

defineEmits<{ 'open-archive': [path: string] }>()

const rankingWindow = ref<'7d' | 'all'>('7d')
const rankings = ref<ArchiveRankingItem[]>([])

async function loadRankings() {
  rankings.value = (await requestJSON<ArchiveRankingItem[]>(`/api/archive/rankings?window=${rankingWindow.value}&limit=20`, { headers: authHeaders() })) ?? []
}

defineExpose({ reload: loadRankings })
</script>

<style scoped src="./panel-shared.css"></style>
