<template>
  <section class="panel source-panel">
    <div class="section-title">
      <div>
        <h2>订阅源</h2>
        <span>仅显示管理员主动设置的第一优先级来源；友情链接博客在后台作为第二优先级扩展</span>
      </div>
    </div>
    <form v-if="isOwner" class="source-form" @submit.prevent="saveSource">
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
      <article v-for="source in pagedSources" :key="source.id" class="source-row">
        <div class="item-main">
          <h3>{{ source.name }}</h3>
          <p>{{ source.url }}</p>
          <span>第一优先级 · {{ source.type }} · {{ source.enabled ? '启用' : '停用' }}</span>
          <small v-if="source.lastError">{{ source.lastError }}</small>
        </div>
        <a-space>
          <a-button v-if="source.siteId" size="small" @click="loadSiteInsight(source)">图谱与回溯</a-button>
          <a-button v-if="isOwner" size="small" :loading="fetchingSourceId === source.id" @click="fetchSource(source.id)">
            <template #icon><icon-sync /></template>
            获取
          </a-button>
          <a-button v-if="isOwner" size="small" status="danger" @click="deleteSource(source.id)">
            <template #icon><icon-delete /></template>
          </a-button>
        </a-space>
      </article>
    </div>
    <a-pagination
      v-if="sources.length > sourcePageSize"
      v-model:current="sourcePage"
      v-model:page-size="sourcePageSize"
      :total="sources.length"
      :page-size-options="[10, 20, 50]"
      class="source-pagination"
      show-total
      show-page-size
    />
    <!-- 仅在 owner 点选某个订阅源后展示图谱，避免 source 为空时读取 name 抛错 -->
    <SiteInsightPanel v-if="selectedSource" :source="selectedSource" :graph="siteGraph" :operations="siteOperations" :backfills="siteBackfills" :is-owner="isOwner" @reload="reloadSiteInsight" @backfill="requestBackfill" />
  </section>
</template>

<script setup lang="ts">
// 订阅源列表、添加与站点图谱。
import SiteInsightPanel from '@/components/recommendations/SiteInsightPanel.vue'
import { requestJSON } from '@/api/client'
import type { DiscoverySource } from '@/components/recommendations/types'
import { IconDelete, IconPlus, IconSync } from '@arco-design/web-vue/es/icon'
import { Message } from '@arco-design/web-vue'
import { computed, reactive, ref, watch } from 'vue'

const props = defineProps<{ isOwner: boolean }>()
const emit = defineEmits<{ 'inventory-changed': [] }>()

const savingSource = ref(false)
const fetchingSourceId = ref<number | null>(null)
const sources = ref<DiscoverySource[]>([])
const sourcePage = ref(1)
const sourcePageSize = ref(10)
const selectedSource = ref<DiscoverySource>()
const siteGraph = ref<any>()
const siteOperations = ref<any>()
const siteBackfills = ref<any[]>([])
const sourceForm = reactive({ name: '', url: '', type: 'feed' })

const pagedSources = computed(() => {
  const start = (sourcePage.value - 1) * sourcePageSize.value
  return sources.value.slice(start, start + sourcePageSize.value)
})

watch(() => sources.value.length, (total) => {
  const maxPage = Math.max(1, Math.ceil(total / sourcePageSize.value))
  if (sourcePage.value > maxPage) sourcePage.value = maxPage
})

async function loadSources() {
  sources.value = (await requestJSON<DiscoverySource[]>('/api/discovery/sources')) ?? []
}

async function saveSource() {
  if (!sourceForm.url.trim()) {
    Message.warning('请输入内容源 URL')
    return
  }
  try {
    savingSource.value = true
    await requestJSON<DiscoverySource>('/api/discovery/sources', {
      method: 'POST',
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

async function fetchSource(sourceId: number) {
  try {
    fetchingSourceId.value = sourceId
    await requestJSON(`/api/discovery/sources/${sourceId}/fetch`, { method: 'POST' })
    await loadSources()
    emit('inventory-changed')
    Message.success('内容源刷新完成')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '刷新内容源失败')
  } finally {
    fetchingSourceId.value = null
  }
}

async function deleteSource(sourceId: number) {
  try {
    await requestJSON(`/api/discovery/sources/${sourceId}`, { method: 'DELETE' })
    await loadSources()
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '删除内容源失败')
  }
}

async function loadSiteInsight(source: DiscoverySource) {
  selectedSource.value = source
  await reloadSiteInsight(source.siteId || 0)
}

async function reloadSiteInsight(siteId: number) {
  if (!siteId) return
  const requests: Promise<void>[] = [
    requestJSON<any>(`/api/discovery/sites/${siteId}/graph`).then((value) => { siteGraph.value = value }),
    requestJSON<any[]>(`/api/discovery/sites/${siteId}/backfill`).then((value) => { siteBackfills.value = value || [] }),
  ]
  if (props.isOwner) requests.push(requestJSON<any>(`/api/discovery/sites/${siteId}/operations`).then((value) => { siteOperations.value = value }))
  try {
    await Promise.all(requests)
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '加载站点详情失败')
  }
}

async function requestBackfill(siteId: number) {
  try {
    await requestJSON(`/api/discovery/sites/${siteId}/backfill`, { method: 'POST' })
    await reloadSiteInsight(siteId)
    Message.success('历史回溯已排队')
  } catch (error) {
    Message.error(error instanceof Error ? error.message : '启动回溯失败')
  }
}

defineExpose({ reload: loadSources })
</script>

<style scoped src="./panel-shared.css"></style>
<style scoped>
.source-panel {
  border-bottom: 1px solid #e5e6eb;
}

.source-pagination {
  margin-top: 16px;
  justify-content: flex-end;
}

.source-form {
  display: grid;
  grid-template-columns: minmax(160px, 1fr) minmax(280px, 2fr) minmax(110px, 130px) auto;
  align-items: end;
  gap: 12px;
  margin-bottom: 16px;
}

@media (max-width: 820px) {
  .source-form {
    grid-template-columns: 1fr;
  }
}
</style>
