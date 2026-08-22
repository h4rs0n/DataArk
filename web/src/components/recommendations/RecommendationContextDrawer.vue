<template>
  <a-drawer :visible="visible" :width="520" title="推荐追溯与文章评估" @update:visible="$emit('update:visible', $event)">
    <a-spin :loading="loading" style="width:100%">
      <template v-if="itemContext">
        <h3>{{ itemContext.item.snapshotTitle }}</h3>
        <p>文章评估：{{ itemContext.assessment ? `${Math.round(itemContext.assessment.overallQuality * 100)} 分 · ${itemContext.assessment.assessor}` : '规则评估详情不可用' }}</p>
        <p>个人状态：{{ itemContext.userState?.currentFeedback || '无反馈' }}</p>
        <article v-for="entry in itemContext.provenance" :key="entry.provenance.id" class="trace-entry">
          <strong>{{ entry.site.displayName || entry.site.hostKey }}</strong>
          <span>{{ entry.provenance.discoveryMethod }} · {{ entry.provenance.sourcePageUrl || entry.provenance.originalUrl }}</span>
          <span>路径：{{ (entry.graph?.shortestSeedPath?.sites || []).map((site: any) => site.displayName || site.hostKey).join(' → ') || '种子站点' }}</span>
        </article>
      </template>
    </a-spin>
  </a-drawer>
</template>

<script setup lang="ts">
// 推荐条目的发现路径与评估追溯。
defineProps<{
  visible: boolean
  loading: boolean
  itemContext: any
}>()

defineEmits<{ 'update:visible': [visible: boolean] }>()
</script>

<style scoped>
.trace-entry {
  display: grid;
  gap: 4px;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid #e5e6eb;
}

.trace-entry span {
  color: #86909c;
  font-size: 13px;
}
</style>
