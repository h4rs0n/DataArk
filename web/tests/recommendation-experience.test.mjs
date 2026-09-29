import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

async function readSource(relativePath) {
  return readFile(new URL(relativePath, import.meta.url), 'utf8')
}

const view = await readSource('../src/views/RecommendationsView.vue')
const feed = await readSource('../src/components/recommendations/DiscoveryFeedPanel.vue')
const digest = await readSource('../src/components/recommendations/DailyDigestPanel.vue')
const settings = await readSource('../src/components/recommendations/SettingsPanel.vue')
const sources = await readSource('../src/components/recommendations/DiscoverySourcesPanel.vue')
const queue = await readSource('../src/components/recommendations/CrawlQueuePanel.vue')
const ranking = await readSource('../src/components/recommendations/ArchiveRankingPanel.vue')
const keywords = await readSource('../src/components/recommendations/SearchKeywordsPanel.vue')
const archives = await readSource('../src/components/recommendations/ArchiveRecommendPanel.vue')
const impact = await readSource('../src/components/recommendations/ImpactFeedbackModal.vue')
const drawer = await readSource('../src/components/recommendations/RecommendationContextDrawer.vue')
const types = await readSource('../src/components/recommendations/types.ts')
const format = await readSource('../src/components/recommendations/format.ts')
const feedback = await readSource('../src/components/recommendations/FeedbackControls.vue')
const recommendationCard = await readSource('../src/components/recommendations/RecommendationArticleCard.vue')
const siteInsight = await readSource('../src/components/recommendations/SiteInsightPanel.vue')
const metricsPanel = await readSource('../src/components/recommendations/AssessmentMetricsPanel.vue')
const surface = [
  view, feed, digest, settings, sources, queue, ranking, keywords, archives,
  impact, drawer, types, format, feedback, recommendationCard, siteInsight, metricsPanel,
].join('\n')

test('recommendation experience exposes scoped reversible feedback', () => {
  for (const action of ['valuable', 'not_interested', 'too_repetitive', 'low_value', 'deep_read', 'block_source', 'reduce_topic', 'reduce_style']) {
    assert.match(surface, new RegExp(action))
  }
  assert.match(feedback, /\{ value: 'low_value', label: '低价值' \}/)
  assert.doesNotMatch(feedback, /\{ value: 'deep_read', label: '值得深读' \}/)
  assert.match(feedback, /currentAction === 'deep_read'.*历史反馈：值得深读/s)
  assert.match(view, /method: 'DELETE'.*feedback/s)
  assert.match(view, /feedbackByItem/)
})

test('recommendation experience shows digest and discovery audit context', () => {
  for (const contract of ['DigestSummary', 'SiteInsightPanel', '/context', '/graph', '/backfill', '/operations', 'explorationReason', 'shortageReasons']) {
    assert.match(surface, new RegExp(contract.replace('/', '\\/')))
  }
})

test('frontend has no destructive daily regeneration interaction', () => {
  assert.doesNotMatch(surface, /admin\/recommendations\/generate/)
  assert.match(digest, /admin\/recommendations\/supplement/)
})

test('digest summary is LLM-only and archive recommendations show errors', () => {
  assert.doesNotMatch(digest, /统计总结/)
  assert.doesNotMatch(digest, /rule-based/)
  assert.match(digest, /AI 总结/)
  assert.doesNotMatch(archives, /基于点击、搜索词/)
  assert.match(archives, /由模型根据近期归档选出/)
  assert.match(archives, /loadError/)
  assert.match(archives, /归档推荐失败/)
})

test('discovery UI lists manual subscriptions without sitemap gap fill', () => {
  assert.match(sources, /仅显示管理员主动设置的第一优先级来源/)
  assert.match(view, /title="发现"/)
  assert.match(sources, /SiteInsightPanel v-if="selectedSource"/)
  assert.doesNotMatch(surface, /\/sitemap-backfill/)
  assert.doesNotMatch(siteInsight, /Sitemap 默认关闭/)
  assert.doesNotMatch(siteInsight, /sitemapBackfill/)
})

test('owner assessment module shows metrics and a manual LLM queue', () => {
  assert.match(view, /title="评估"/)
  assert.match(view, /AssessmentMetricsPanel/)
  assert.match(view, /class="assess-module"/)
  assert.match(metricsPanel, /\/api\/admin\/assessment\/metrics/)
  assert.match(metricsPanel, /\/api\/admin\/assessment\/queue\?limit=50/)
  assert.match(metricsPanel, /\/api\/admin\/assessment\/queue\/run/)
  assert.match(metricsPanel, /执行 LLM 评估/)
  assert.match(metricsPanel, /article-assessments\/backfill/)
  assert.match(metricsPanel, /article-assessments\/rollback/)
  assert.match(metricsPanel, /待评估队列/)
  assert.match(metricsPanel, /decode token\/s/)
  assert.match(metricsPanel, /预计完成/)
  assert.match(metricsPanel, /queue\.counts\.succeeded24h/)
  assert.match(metricsPanel, /queue\.counts\.failed24h/)
  assert.doesNotMatch(metricsPanel, /等待执行/)
  assert.doesNotMatch(metricsPanel, /正在运行/)
  assert.equal((metricsPanel.match(/本次成功/g) || []).length, 1)
  assert.equal((metricsPanel.match(/本次失败/g) || []).length, 1)
  assert.doesNotMatch(metricsPanel, /近 24 小时成功/)
  assert.doesNotMatch(metricsPanel, /近 24 小时失败/)
  assert.match(metricsPanel, /class="metrics-spin"/)
  assert.match(metricsPanel, /grid-template-columns: repeat\(4, minmax\(0, 1fr\)\)/)
  assert.match(metricsPanel, /loadMetrics = async \(silent = false\)/)
  assert.match(metricsPanel, /wasRunning \|\| queue.state === 'running'/)
  assert.match(metricsPanel, /void loadMetrics\(true\)/)
  assert.match(metricsPanel, /入队后仍需点击/)
})

test('candidate article titles open the source in a safe new tab', () => {
  assert.match(view + recommendationCard, /class="candidate-title-link"/)
  assert.match(view + recommendationCard, /:href="(?:candidate|item\.candidate)\.url"/)
  assert.match(view + recommendationCard, /target="_blank"/)
  assert.match(view + recommendationCard, /rel="noopener noreferrer"/)
  assert.match(feed + digest, /@mark-read="\$emit\('mark-read'/)
  assert.match(view, /@mark-read="markCandidateRead"/)
})

test('daily recommendation tab navigates history with date arrows', () => {
  assert.match(view, /title="每日推荐"/)
  assert.doesNotMatch(view, /title="今日推荐"/)
  assert.doesNotMatch(surface, /title="历史日报"/)
  assert.doesNotMatch(surface, /key="history"/)
  assert.match(digest, /shiftDigestDate\(-1\)/)
  assert.match(digest, /shiftDigestDate\(1\)/)
  assert.match(digest, /aria-label="前一天"/)
  assert.match(digest, /aria-label="后一天"/)
  assert.match(digest, /isViewingToday/)
  assert.match(digest, /\/api\/recommendations\/days\/\$\{date\}/)
  assert.match(digest, /\/api\/recommendations\/days\/\$\{date\}\/summary/)
  assert.match(format, /addCalendarDays/)
  assert.match(digest, /暂无每日推荐/)
  assert.match(digest, /<a-date-picker/)
  assert.match(digest, /disableDigestCalendarDate/)
  assert.match(digest, /\/api\/recommendations\/history\?page=/)
  assert.match(digest, /aria-label="选择日报日期"/)
  assert.match(digest, /day\.status === 'published' \|\| day\.status === 'supplemented'/)
  assert.match(digest, /digest-calendar-popup/)
})

test('today recommendation titles open the source in a safe new tab', () => {
  assert.match(recommendationCard, /:href="item\.candidate\.url"/)
  assert.match(recommendationCard, /\$emit\('mark-read', item\.candidate\)/)
  assert.match(recommendationCard, /item\.candidate\.title \|\| `候选文章 \$\{item\.candidateId\}`/)
})

test('recommendation cards show only article topic tags', () => {
  assert.doesNotMatch(recommendationCard, /poolType/)
  assert.doesNotMatch(recommendationCard, /explorationReason/)
  assert.doesNotMatch(recommendationCard, /探索：/)
  assert.match(recommendationCard, /v-if="topicTags\.length"/)
  assert.match(recommendationCard, /topicTags\.slice\(0, 4\)/)
  assert.match(recommendationCard, /topic-more/)
  assert.match(recommendationCard, /\+{{ topicTags\.length - 4 }}/)
})

test('unread discovery candidates use a refreshable personalized feed', () => {
  assert.match(feed, /<h2>猜你喜欢<\/h2>/)
  assert.match(feed, /\/api\/recommendations\/discovery-feed/)
  assert.match(feed, /\/api\/recommendations\/discovery-feed\/refresh/)
  assert.match(feed, /换一换/)
  assert.match(feed, /当前仅有.*篇符合推荐条件/s)
  assert.ok(((feed + digest).match(/<RecommendationArticleCard/g) || []).length >= 2)
  for (const label of ['原文', '入库', '查看发现路径']) assert.match(recommendationCard, new RegExp(label))
})

test('owner UI exposes the automatic crawl queue in a dedicated tab', () => {
  assert.match(queue, /爬取任务队列/)
  assert.doesNotMatch(surface, /执行待处理任务/)
  assert.match(queue, /文章爬取由后台自动执行/)
  assert.match(queue, /\/api\/admin\/discovery\/crawl-queue\?limit=50/)
  assert.doesNotMatch(surface, /\/api\/admin\/discovery\/crawl-queue\/run/)
  assert.match(view, /<a-tab-pane v-if="isOwner" key="queue" title="任务队列">/)
  assert.ok(view.indexOf('key="queue"') > view.indexOf('key="discovery"'))
  assert.match(queue, /!props\.isOwner \|\| !props\.active/)
  assert.match(queue, /watch\(\(\) => props\.active/)
  assert.match(queue, /crawlQueue\.value\.state === 'running' \? 2000 : 10000/)
  assert.match(queue, /displayedCrawlQueueTasks/)
  assert.match(queue, /另有.*条相同任务已合并/)
  assert.match(queue, /失败原因：.*未提供失败原因/)
})

test('owner queue tab manages the discovery domain blacklist', () => {
  assert.match(queue, /域名黑名单/)
  assert.match(queue, /同时匹配所有子域/)
  assert.match(queue, /\/api\/admin\/discovery\/domain-blacklist/)
  assert.match(queue, /method: 'POST'.*domain-blacklist/s)
  assert.match(queue, /method: 'DELETE'.*domain-blacklist/s)
  assert.match(queue, /affectedCandidates/)
  assert.match(queue, /已爬取文章也会从候选列表和后续推荐中剔除/)
  assert.match(queue, /剔除.*篇候选文章/)
  assert.ok(queue.indexOf('domain-blacklist-panel') > queue.indexOf('爬取任务队列'))
})
