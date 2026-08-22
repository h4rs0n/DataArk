<template>
  <a-modal :visible="visible" :title="impactDialogTitle" :ok-loading="loading" @update:visible="$emit('update:visible', $event)" @ok="$emit('submit')">
    <div class="block-options">
      <label v-for="option in options" :key="`${option.type}:${option.value}`">
        <input v-model="selectedValue" type="radio" :value="option.value" />
        <span>{{ blockRuleTypeLabel(option.type) }}：{{ option.value }}</span>
      </label>
    </div>
  </a-modal>
</template>

<script setup lang="ts">
// 屏蔽来源/主题/风格时选择影响范围。
import { blockRuleTypeLabel, parseList, sourceHost } from '@/components/recommendations/format'
import type { BlockRuleType, BlockTarget, RecommendationItem } from '@/components/recommendations/types'
import { computed, ref, watch } from 'vue'

const props = defineProps<{
  visible: boolean
  item: RecommendationItem | null
  scope: BlockRuleType
  loading?: boolean
}>()

const emit = defineEmits<{
  'update:visible': [visible: boolean]
  submit: []
  'update:selected': [value: string]
}>()

const selectedValue = ref('')

const options = computed<BlockTarget[]>(() => {
  const item = props.item
  if (!item) return []
  const candidate = item.candidate
  const result: BlockTarget[] = []
  if (props.scope === 'topic') {
    for (const topic of parseList(candidate.topics).slice(0, 3)) result.push({ type: 'topic', value: topic })
  }
  const host = candidate.sourceName || sourceHost(candidate.url)
  if (props.scope === 'source' && host) result.push({ type: 'source', value: host })
  if (props.scope === 'style' && candidate.contentStyle) result.push({ type: 'style', value: candidate.contentStyle })
  if (props.scope === 'style' && candidate.contentType && candidate.contentType !== candidate.contentStyle) result.push({ type: 'style', value: candidate.contentType })
  return result
})

const impactDialogTitle = computed(() => ({ source: '屏蔽此来源', topic: '少推荐此主题', style: '少推荐此风格' }[props.scope]))

watch([() => props.visible, options], () => {
  selectedValue.value = options.value[0]?.value || ''
  emit('update:selected', selectedValue.value)
})

watch(selectedValue, (value) => emit('update:selected', value))
</script>

<style scoped>
.block-options {
  display: grid;
  gap: 12px;
}

.block-options label {
  display: flex;
  align-items: center;
  gap: 10px;
}
</style>
