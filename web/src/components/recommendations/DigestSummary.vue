<template>
  <div class="digest-summary">
    <div><strong>{{ day.actualCount }}/{{ day.requestedCount }}</strong><span>实际 / 目标</span></div>
    <div><strong>{{ statusLabel }}</strong><span>{{ day.timezone || '用户时区' }}</span></div>
    <div v-if="shortageText"><strong>候选不足</strong><span>{{ shortageText }}</span></div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ day: { status: string; actualCount: number; requestedCount: number; timezone?: string; shortageReasons?: string } }>()
const statusLabel = computed(() => ({ published: '已发布', supplemented: '已补充', failed: '失败', draft: '草稿', missing: '未生成' }[props.day.status] || props.day.status))
const shortageText = computed(() => {
  if (!props.day.shortageReasons || props.day.actualCount >= props.day.requestedCount) return ''
  try {
    const audit = JSON.parse(props.day.shortageReasons)
    const excluded = Object.entries(audit.excluded || {}).filter(([, count]) => Number(count) > 0).map(([key, count]) => `${key} ${count}`).slice(0, 3)
    return excluded.length ? excluded.join('、') : `缺少 ${props.day.requestedCount - props.day.actualCount} 篇合格文章`
  } catch {
    return props.day.shortageReasons
  }
})
</script>

<style scoped>
.digest-summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 12px; margin: 14px 0; }
.digest-summary > div { padding: 12px; border-radius: 10px; background: #f2f3f5; display: flex; flex-direction: column; gap: 4px; }
.digest-summary strong { font-size: 16px; }.digest-summary span { color: #6b7785; font-size: 12px; word-break: break-word; }
</style>
