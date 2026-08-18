<template>
  <section class="site-insight">
    <header><div><h3>{{ graph?.site?.displayName || source?.name }}</h3><p>{{ graph?.site?.rootUrl || source?.url }}</p></div><a-button size="small" @click="$emit('reload', source?.siteId)">刷新详情</a-button></header>
    <div v-if="graph" class="insight-grid">
      <div><strong>图谱深度 {{ graph.graphDepth }}</strong><span>{{ graph.independentInboundSites }} 个独立入链</span></div>
      <div><strong>发现路径</strong><span>{{ pathLabel }}</span></div>
      <div><strong>关系</strong><span>入 {{ graph.inbound?.length || 0 }} · 出 {{ graph.outbound?.length || 0 }}</span></div>
    </div>
    <div v-if="operations" class="operations">
      <h4>抓取端点与健康</h4>
      <article v-for="endpoint in operations.endpoints || []" :key="endpoint.source.id">
        <span>{{ endpoint.source.endpointType || endpoint.source.type }}</span>
        <span>成功 {{ formatTime(endpoint.source.lastSuccessAt) }}</span>
        <span>下次 {{ formatTime(endpoint.source.nextDueAt || endpoint.source.nextFetchAt) }}</span>
        <span v-if="endpoint.source.lastError" class="error">{{ endpoint.source.lastError }}</span>
      </article>
    </div>
    <div class="operations">
      <h4>历史回溯</h4>
      <article v-for="coverage in backfills" :key="coverage.state?.id || coverage.state?.strategy">
        <span>{{ coverage.state?.strategy }} · {{ coverage.state?.status }}</span>
        <span>访问 {{ coverage.visitedUrls }} · 待处理 {{ coverage.pendingUrls }}</span>
        <span>文章 {{ coverage.state?.articlesFound || 0 }} · {{ Math.round((coverage.estimatedCompletion || 0) * 100) }}%</span>
      </article>
      <a-button v-if="isOwner" size="small" @click="$emit('backfill', source?.siteId)">继续历史回溯</a-button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
const props = defineProps<{ source?: any; graph?: any; operations?: any; backfills: any[]; isOwner: boolean }>()
defineEmits<{ reload: [siteId: number]; backfill: [siteId: number] }>()
const pathLabel = computed(() => (props.graph?.shortestSeedPath?.sites || []).map((site: any) => site.displayName || site.hostKey).join(' → ') || '当前站点即种子或暂无路径')
function formatTime(value?: string) { return value ? new Date(value).toLocaleString() : '暂无' }
</script>

<style scoped>
.site-insight { margin-top: 16px; padding: 16px; border: 1px solid #e5e6eb; border-radius: 12px; }
.site-insight header { display:flex; justify-content:space-between; gap:12px; }.site-insight h3,.site-insight p,.site-insight h4 { margin:0; }
.site-insight p,.site-insight span { color:#6b7785; font-size:12px; }
.insight-grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(150px,1fr)); gap:10px; margin:14px 0; }
.insight-grid > div { display:flex; flex-direction:column; padding:10px; background:#f7f8fa; border-radius:8px; }
.operations { margin-top:12px; }.operations article { display:grid; grid-template-columns:repeat(auto-fit,minmax(130px,1fr)); gap:8px; padding:8px 0; border-bottom:1px solid #f2f3f5; }
.error { color:#d93026 !important; }
</style>
