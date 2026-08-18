import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const view = await readFile(new URL('../src/views/RecommendationsView.vue', import.meta.url), 'utf8')
const feedback = await readFile(new URL('../src/components/recommendations/FeedbackControls.vue', import.meta.url), 'utf8')
const recommendationCard = await readFile(new URL('../src/components/recommendations/RecommendationArticleCard.vue', import.meta.url), 'utf8')
const siteInsight = await readFile(new URL('../src/components/recommendations/SiteInsightPanel.vue', import.meta.url), 'utf8')
const metricsPanel = await readFile(new URL('../src/components/recommendations/AssessmentMetricsPanel.vue', import.meta.url), 'utf8')

test('recommendation experience exposes scoped reversible feedback', () => {
  for (const action of ['valuable', 'not_interested', 'too_repetitive', 'low_value', 'deep_read', 'block_source', 'reduce_topic', 'reduce_style']) {
    assert.match(view + feedback, new RegExp(action))
  }
  assert.match(feedback, /\{ value: 'low_value', label: '低价值' \}/)
  assert.doesNotMatch(feedback, /\{ value: 'deep_read', label: '值得深读' \}/)
  assert.match(feedback, /currentAction === 'deep_read'.*历史反馈：值得深读/s)
  assert.match(view, /method: 'DELETE'.*feedback/s)
  assert.match(view, /feedbackByItem/)
})

test('recommendation experience shows digest and discovery audit context', () => {
  for (const contract of ['DigestSummary', 'SiteInsightPanel', '/context', '/graph', '/backfill', '/operations', 'explorationReason', 'shortageReasons']) {
    assert.match(view, new RegExp(contract.replace('/', '\\/')))
  }
})

test('frontend has no destructive daily regeneration interaction', () => {
  assert.doesNotMatch(view, /admin\/recommendations\/generate/)
  assert.match(view, /admin\/recommendations\/supplement/)
})

test('discovery UI lists manual subscriptions without sitemap gap fill', () => {
  assert.match(view, /仅显示管理员主动设置的第一优先级来源/)
  assert.match(view, /title="发现"/)
  assert.match(view, /SiteInsightPanel v-if="selectedSource"/)
  assert.doesNotMatch(view, /\/sitemap-backfill/)
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
  assert.match(metricsPanel, /token\/s/)
  assert.match(metricsPanel, /预计完成/)
  assert.match(metricsPanel, /queue\.counts\.succeeded24h/)
  assert.match(metricsPanel, /queue\.counts\.failed24h/)
  assert.doesNotMatch(metricsPanel, /等待执行/)
  assert.doesNotMatch(metricsPanel, /正在运行/)
  assert.equal((metricsPanel.match(/近 24 小时成功/g) || []).length, 1)
  assert.equal((metricsPanel.match(/近 24 小时失败/g) || []).length, 1)
  assert.match(metricsPanel, /class="metrics-spin"/)
  assert.match(metricsPanel, /grid-template-columns: repeat\(4, minmax\(0, 1fr\)\)/)
  assert.match(metricsPanel, /入队后仍需点击/)
})

test('candidate article titles open the source in a safe new tab', () => {
  assert.match(view + recommendationCard, /class="candidate-title-link"/)
  assert.match(view + recommendationCard, /:href="(?:candidate|item\.candidate)\.url"/)
  assert.match(view + recommendationCard, /target="_blank"/)
  assert.match(view + recommendationCard, /rel="noopener noreferrer"/)
  assert.match(view, /@mark-read="markCandidateRead"/)
})

test('daily recommendation tab navigates history with date arrows', () => {
  assert.match(view, /title="每日推荐"/)
  assert.doesNotMatch(view, /title="今日推荐"/)
  assert.doesNotMatch(view, /title="历史日报"/)
  assert.doesNotMatch(view, /key="history"/)
  assert.match(view, /shiftDigestDate\(-1\)/)
  assert.match(view, /shiftDigestDate\(1\)/)
  assert.match(view, /aria-label="前一天"/)
  assert.match(view, /aria-label="后一天"/)
  assert.match(view, /isViewingToday/)
  assert.match(view, /\/api\/recommendations\/days\/\$\{date\}/)
  assert.match(view, /\/api\/recommendations\/days\/\$\{date\}\/summary/)
  assert.match(view, /addCalendarDays/)
  assert.match(view, /暂无每日推荐/)
  assert.match(view, /<a-date-picker/)
  assert.match(view, /disableDigestCalendarDate/)
  assert.match(view, /\/api\/recommendations\/history\?page=/)
  assert.match(view, /aria-label="选择日报日期"/)
  assert.match(view, /day\.status === 'published' \|\| day\.status === 'supplemented'/)
  assert.match(view, /digest-calendar-popup/)
})

test('today recommendation titles open the source in a safe new tab', () => {
  assert.match(recommendationCard, /:href="item\.candidate\.url"/)
  assert.match(recommendationCard, /\$emit\('mark-read', item\.candidate\)/)
  assert.match(recommendationCard, /item\.candidate\.title \|\| `候选文章 \$\{item\.candidateId\}`/)
})

test('unread discovery candidates use a refreshable personalized feed', () => {
  assert.match(view, /<h2>猜你喜欢<\/h2>/)
  assert.match(view, /\/api\/recommendations\/discovery-feed/)
  assert.match(view, /\/api\/recommendations\/discovery-feed\/refresh/)
  assert.match(view, /换一换/)
  assert.match(view, /当前仅有.*篇符合推荐条件/s)
  assert.ok((view.match(/<RecommendationArticleCard/g) || []).length >= 2)
  for (const label of ['原文', '入库', '查看发现路径']) assert.match(recommendationCard, new RegExp(label))
})

test('owner UI exposes the automatic crawl queue in a dedicated tab', () => {
  assert.match(view, /爬取任务队列/)
  assert.doesNotMatch(view, /执行待处理任务/)
  assert.match(view, /文章爬取由后台自动执行/)
  assert.match(view, /\/api\/admin\/discovery\/crawl-queue\?limit=50/)
  assert.doesNotMatch(view, /\/api\/admin\/discovery\/crawl-queue\/run/)
  assert.match(view, /<a-tab-pane v-if="isOwner" key="queue" title="任务队列">/)
  assert.ok(view.indexOf('key="queue"') > view.indexOf('key="discovery"'))
  assert.match(view, /moduleTab\.value !== 'discover' \|\| discoveryTab\.value !== 'queue'/)
  assert.match(view, /watch\(\[moduleTab, discoveryTab\]/)
  assert.match(view, /crawlQueue\.value\.state === 'running' \? 2000 : 10000/)
  assert.match(view, /displayedCrawlQueueTasks/)
  assert.match(view, /另有.*条相同任务已合并/)
  assert.match(view, /失败原因：.*未提供失败原因/)
})

test('owner queue tab manages the discovery domain blacklist', () => {
  assert.match(view, /域名黑名单/)
  assert.match(view, /同时匹配所有子域/)
  assert.match(view, /\/api\/admin\/discovery\/domain-blacklist/)
  assert.match(view, /method: 'POST'.*domain-blacklist/s)
  assert.match(view, /method: 'DELETE'.*domain-blacklist/s)
  assert.match(view, /affectedCandidates/)
  assert.match(view, /已爬取文章也会从候选列表和后续推荐中剔除/)
  assert.match(view, /剔除.*篇候选文章/)
  assert.ok(view.indexOf('domain-blacklist-panel') > view.indexOf('爬取任务队列'))
})
