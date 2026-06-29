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
