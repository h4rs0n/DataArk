<template>
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
      <button v-for="item in keywords" :key="item.keyword" class="keyword-tile" type="button" @click="$emit('search-keyword', item.keyword)">
        <strong>{{ item.keyword }}</strong>
        <span>{{ item.count }} 次 · {{ item.resultCount }} 条结果</span>
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
// 搜索热词。
import { requestJSON } from '@/api/client'
import type { KeywordItem } from '@/components/recommendations/types'
import { ref } from 'vue'

defineEmits<{ 'search-keyword': [keyword: string] }>()

const keywordWindow = ref<'7d' | 'all'>('7d')
const keywords = ref<KeywordItem[]>([])

async function loadKeywords() {
  keywords.value = (await requestJSON<KeywordItem[]>(`/api/search/keywords?window=${keywordWindow.value}&limit=30`)) ?? []
}

defineExpose({ reload: loadKeywords })
</script>

<style scoped src="./panel-shared.css"></style>
<style scoped>
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

.keyword-tile:hover {
  border-color: #165dff;
  box-shadow: inset 3px 0 0 #165dff;
}

.keyword-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 12px;
}
</style>
