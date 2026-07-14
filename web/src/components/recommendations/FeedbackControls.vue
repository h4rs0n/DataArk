<template>
  <div class="feedback-controls" :aria-label="`推荐项 ${itemId} 反馈`">
    <a-button v-for="action in actions" :key="action.value" size="small" :type="currentAction === action.value ? 'primary' : 'outline'" :loading="loading" @click="$emit('select', action.value)">
      {{ action.label }}
    </a-button>
    <a-dropdown trigger="click">
      <a-button size="small">调整范围</a-button>
      <template #content>
        <a-doption @click="$emit('scope', 'source')">屏蔽此来源</a-doption>
        <a-doption @click="$emit('scope', 'topic')">少推荐此主题</a-doption>
        <a-doption @click="$emit('scope', 'style')">少推荐此风格</a-doption>
      </template>
    </a-dropdown>
    <a-button v-if="currentAction" size="small" status="danger" :loading="loading" @click="$emit('revert')">撤销</a-button>
  </div>
</template>

<script setup lang="ts">
defineProps<{ itemId: number; currentAction?: string; loading?: boolean }>()
defineEmits<{
  select: [action: 'valuable' | 'not_interested' | 'too_repetitive' | 'deep_read']
  scope: [scope: 'source' | 'topic' | 'style']
  revert: []
}>()

const actions = [
  { value: 'valuable', label: '有价值' },
  { value: 'not_interested', label: '不感兴趣' },
  { value: 'too_repetitive', label: '太重复' },
  { value: 'deep_read', label: '值得深读' },
] as const
</script>

<style scoped>
.feedback-controls { display: flex; flex-wrap: wrap; gap: 8px; justify-content: flex-end; }
</style>
