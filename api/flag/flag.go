package flag

import (
	"DataArk/config"
	stdflag "flag"
	"strings"
)

func ParseFlag() {
	debugFlag := stdflag.Bool("debug", false, "Enable debug mode")
	ArchiveFileLocationFlag := stdflag.String("loc", "./api/static/archive/", "Assign HTML file path")
	MEILIHostFlag := stdflag.String("mhost", "http://127.0.0.1:7700", "Assign MeiliSearch host")
	MEILIKeyFlag := stdflag.String("mkey", "", "Assign MeiliSearch API key")
	MEILIDumpDirFlag := stdflag.String("mdump", "./dumps", "Assign shared MeiliSearch dump directory")
	SingleFileWebServiceURLFlag := stdflag.String("sfhost", "http://singlefile-webservice:8080", "Assign SingleFile WEBService host")
	DBHostFlag := stdflag.String("dbhost", "localhost", "Assign DB host")
	DBPortFlag := stdflag.String("dbport", "5432", "Assign DB port")
	DBNameFlag := stdflag.String("dbname", "echoark", "Assign DB name")
	DBUserFlag := stdflag.String("dbuser", "postgres", "Assign DB user")
	DBPasswordFlag := stdflag.String("dbpasswd", "postgres", "Assign DB password")
	DiscoveryFetchIntervalFlag := stdflag.String("discover-interval", "6h", "Assign discovery source fetch interval, set 0 to disable")
	DiscoveryRequestTimeoutFlag := stdflag.String("discover-timeout", "12s", "Assign discovery HTTP request timeout")
	DiscoveryMaxCandidatesFlag := stdflag.Int("discover-max", 50, "Assign max candidates collected per source fetch")
	DiscoveryUserAgentFlag := stdflag.String("discover-ua", "DataArkDiscovery/1.0", "Assign discovery HTTP User-Agent")
	DiscoveryHostConcurrencyFlag := stdflag.Int("discover-host-concurrency", 2, "Assign maximum concurrent discovery requests per host")
	DiscoveryMinRequestIntervalFlag := stdflag.String("discover-min-request-interval", "1s", "Assign minimum interval between discovery requests to one host")
	DiscoveryRobotsCacheTTLFlag := stdflag.String("discover-robots-ttl", "6h", "Assign robots.txt cache lifetime")
	DiscoveryMaxRedirectsFlag := stdflag.Int("discover-max-redirects", 5, "Assign maximum discovery HTTP redirects")
	DiscoveryActiveFeedIntervalFlag := stdflag.String("discover-active-feed-interval", "24h", "Assign maximum interval for active feed checks")
	DiscoveryObservingIntervalFlag := stdflag.String("discover-observing-interval", "168h", "Assign maximum interval for observing-site checks")
	DiscoveryDormantIntervalFlag := stdflag.String("discover-dormant-interval", "720h", "Assign maximum interval for dormant-site checks")
	DiscoveryBackoffBaseFlag := stdflag.String("discover-backoff-base", "5m", "Assign discovery failure backoff base")
	DiscoveryBackoffMaxFlag := stdflag.String("discover-backoff-max", "24h", "Assign discovery failure backoff maximum")
	DiscoveryMaxGraphDepthFlag := stdflag.Int("discover-max-graph-depth", 3, "Assign maximum automatic blog graph depth")
	DiscoveryMaxBlogrollTargetsFlag := stdflag.Int("discover-max-blogroll-targets", 50, "Assign maximum active blogroll targets per site scan")
	DiscoveryDailyObservingLimitFlag := stdflag.Int("discover-daily-observing-limit", 100, "Assign maximum new observing sites activated per day")
	DiscoveryBackfillBatchSizeFlag := stdflag.Int("discover-backfill-batch-size", 1, "Assign bounded pages processed per historical backfill job")
	DiscoveryBackfillMaxIntervalFlag := stdflag.String("discover-backfill-max-interval", "168h", "Assign maximum wait between unfinished historical backfill batches")
	DiscoveryArticleMinCharsFlag := stdflag.Int("discover-article-min-chars", 120, "Assign minimum extracted article text length")
	DiscoveryProcessingMaxAttemptsFlag := stdflag.Int("discover-processing-max-attempts", 5, "Assign maximum transient article processing attempts")
	DiscoveryArticleQualityThresholdFlag := stdflag.Float64("discover-article-quality-threshold", 0.45, "Assign minimum article-level quality required for eligibility")
	DiscoveryScheduleMinIntervalFlag := stdflag.String("discover-schedule-min-interval", "1h", "Assign minimum interval after applying extra crawl budget")
	DiscoveryInventoryFreshDaysFlag := stdflag.Int("discover-inventory-fresh-days", 30, "Assign recent article window used by candidate inventory")
	DiscoveryInventoryWarningDaysFlag := stdflag.Float64("discover-inventory-warning-days", 7, "Assign candidate inventory warning threshold in days")
	DiscoveryInventoryCriticalDaysFlag := stdflag.Float64("discover-inventory-critical-days", 3, "Assign candidate inventory critical threshold in days")
	RecommendationEnabledFlag := stdflag.Bool("recommend-enabled", false, "Enable LLM daily recommendations")
	RecommendationDailyLimitFlag := stdflag.Int("recommend-daily-limit", 10, "Assign default daily recommendation count")
	RecommendationTimezoneFlag := stdflag.String("recommend-timezone", "Asia/Shanghai", "Assign recommendation timezone")
	RecommendationGenerationTimeFlag := stdflag.String("recommend-time", "07:00", "Assign daily recommendation generation time")
	RecommendationCandidateWindowDaysFlag := stdflag.Int("recommend-window-days", 30, "Assign recommendation candidate window in days")
	RecommendationCandidatePoolSizeFlag := stdflag.Int("recommend-pool-size", 100, "Assign recommendation candidate pool size")
	RecommendationRerankLimitFlag := stdflag.Int("recommend-rerank-limit", 30, "Assign max candidates sent to LLM reranker")
	RecommendationExplorationRateFlag := stdflag.Float64("recommend-exploration-rate", 0.15, "Assign recommendation exploration rate")
	LLMBaseURLFlag := stdflag.String("llm-base-url", "", "Assign OpenAI-compatible LLM base URL")
	LLMAPIKeyFlag := stdflag.String("llm-api-key", "", "Assign LLM API key")
	LLMChatModelFlag := stdflag.String("llm-chat-model", "", "Assign LLM chat model")
	LLMEmbeddingModelFlag := stdflag.String("llm-embedding-model", "", "Assign LLM embedding model")
	LLMTimeoutFlag := stdflag.String("llm-timeout", "30s", "Assign LLM request timeout")
	EmbeddingDimensionFlag := stdflag.Int("embedding-dimension", 0, "Assign embedding vector dimension")
	RSSHubBaseURLFlag := stdflag.String("rsshub-base-url", "", "Assign optional RSSHub base URL")
	stdflag.Parse()
	config.DEBUG = *debugFlag
	config.ARCHIVEFILELOACTION = *ArchiveFileLocationFlag
	config.MEILIHOST = *MEILIHostFlag
	config.MEILIAPIKey = *MEILIKeyFlag
	config.MEILIDumpDir = *MEILIDumpDirFlag
	config.SINGLEFILEWEBSERVICEURL = strings.TrimRight(*SingleFileWebServiceURLFlag, "/")
	config.DBHost = *DBHostFlag
	config.DBPort = *DBPortFlag
	config.DBName = *DBNameFlag
	config.DBUser = *DBUserFlag
	config.DBPassword = *DBPasswordFlag
	config.DISCOVERYFETCHINTERVAL = *DiscoveryFetchIntervalFlag
	config.DISCOVERYREQUESTTIMEOUT = *DiscoveryRequestTimeoutFlag
	config.DISCOVERYMAXCANDIDATES = *DiscoveryMaxCandidatesFlag
	config.DISCOVERYUSERAGENT = strings.TrimSpace(*DiscoveryUserAgentFlag)
	config.DISCOVERYHOSTCONCURRENCY = *DiscoveryHostConcurrencyFlag
	config.DISCOVERYMINREQUESTINTERVAL = strings.TrimSpace(*DiscoveryMinRequestIntervalFlag)
	config.DISCOVERYROBOTSCACHETTL = strings.TrimSpace(*DiscoveryRobotsCacheTTLFlag)
	config.DISCOVERYMAXREDIRECTS = *DiscoveryMaxRedirectsFlag
	config.DISCOVERYACTIVEFEEDINTERVAL = strings.TrimSpace(*DiscoveryActiveFeedIntervalFlag)
	config.DISCOVERYOBSERVINGINTERVAL = strings.TrimSpace(*DiscoveryObservingIntervalFlag)
	config.DISCOVERYDORMANTINTERVAL = strings.TrimSpace(*DiscoveryDormantIntervalFlag)
	config.DISCOVERYBACKOFFBASE = strings.TrimSpace(*DiscoveryBackoffBaseFlag)
	config.DISCOVERYBACKOFFMAX = strings.TrimSpace(*DiscoveryBackoffMaxFlag)
	config.DISCOVERYMAXGRAPHDEPTH = *DiscoveryMaxGraphDepthFlag
	config.DISCOVERYMAXBLOGROLLTARGETS = *DiscoveryMaxBlogrollTargetsFlag
	config.DISCOVERYDAILYOBSERVINGLIMIT = *DiscoveryDailyObservingLimitFlag
	config.DISCOVERYBACKFILLBATCHSIZE = *DiscoveryBackfillBatchSizeFlag
	config.DISCOVERYBACKFILLMAXINTERVAL = strings.TrimSpace(*DiscoveryBackfillMaxIntervalFlag)
	config.DISCOVERYARTICLEMINCHARS = *DiscoveryArticleMinCharsFlag
	config.DISCOVERYPROCESSINGMAXATTEMPTS = *DiscoveryProcessingMaxAttemptsFlag
	config.DISCOVERYARTICLEQUALITYTHRESHOLD = *DiscoveryArticleQualityThresholdFlag
	config.DISCOVERYSCHEDULEMININTERVAL = strings.TrimSpace(*DiscoveryScheduleMinIntervalFlag)
	config.DISCOVERYINVENTORYFRESHDAYS = *DiscoveryInventoryFreshDaysFlag
	config.DISCOVERYINVENTORYWARNINGDAYS = *DiscoveryInventoryWarningDaysFlag
	config.DISCOVERYINVENTORYCRITICALDAYS = *DiscoveryInventoryCriticalDaysFlag
	config.RECOMMENDATIONENABLED = *RecommendationEnabledFlag
	config.RECOMMENDATIONDAILYLIMIT = *RecommendationDailyLimitFlag
	config.RECOMMENDATIONTIMEZONE = strings.TrimSpace(*RecommendationTimezoneFlag)
	config.RECOMMENDATIONGENERATIONTIME = strings.TrimSpace(*RecommendationGenerationTimeFlag)
	config.RECOMMENDATIONCANDIDATEWINDOWDAYS = *RecommendationCandidateWindowDaysFlag
	config.RECOMMENDATIONCANDIDATEPOOLSIZE = *RecommendationCandidatePoolSizeFlag
	config.RECOMMENDATIONRERANKLIMIT = *RecommendationRerankLimitFlag
	config.RECOMMENDATIONEXPLORATIONRATE = *RecommendationExplorationRateFlag
	config.LLMBASEURL = strings.TrimRight(strings.TrimSpace(*LLMBaseURLFlag), "/")
	config.LLMAPIKEY = strings.TrimSpace(*LLMAPIKeyFlag)
	config.LLMCHATMODEL = strings.TrimSpace(*LLMChatModelFlag)
	config.LLMEMBEDDINGMODEL = strings.TrimSpace(*LLMEmbeddingModelFlag)
	config.LLMTIMEOUT = strings.TrimSpace(*LLMTimeoutFlag)
	config.EMBEDDINGDIMENSION = *EmbeddingDimensionFlag
	config.RSSHUBBASEURL = strings.TrimRight(strings.TrimSpace(*RSSHubBaseURLFlag), "/")
}
