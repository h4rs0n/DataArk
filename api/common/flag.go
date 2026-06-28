package common

import (
	"flag"
	"strings"
)

func ParseFlag() {
	debugFlag := flag.Bool("debug", false, "Enable debug mode")
	ArchiveFileLocationFlag := flag.String("loc", "./api/static/archive/", "Assign HTML file path")
	MEILIHostFlag := flag.String("mhost", "http://127.0.0.1:7700", "Assign MeiliSearch host")
	MEILIKeyFlag := flag.String("mkey", "", "Assign MeiliSearch API key")
	MEILIDumpDirFlag := flag.String("mdump", "./dumps", "Assign shared MeiliSearch dump directory")
	SingleFileWebServiceURLFlag := flag.String("sfhost", "http://singlefile-webservice:8080", "Assign SingleFile WEBService host")
	DBHostFlag := flag.String("dbhost", "localhost", "Assign DB host")
	DBPortFlag := flag.String("dbport", "5432", "Assign DB port")
	DBNameFlag := flag.String("dbname", "echoark", "Assign DB name")
	DBUserFlag := flag.String("dbuser", "postgres", "Assign DB user")
	DBPasswordFlag := flag.String("dbpasswd", "postgres", "Assign DB password")
	DiscoveryFetchIntervalFlag := flag.String("discover-interval", "6h", "Assign discovery source fetch interval, set 0 to disable")
	DiscoveryRequestTimeoutFlag := flag.String("discover-timeout", "12s", "Assign discovery HTTP request timeout")
	DiscoveryMaxCandidatesFlag := flag.Int("discover-max", 50, "Assign max candidates collected per source fetch")
	DiscoveryUserAgentFlag := flag.String("discover-ua", "DataArkDiscovery/1.0", "Assign discovery HTTP User-Agent")
	RecommendationEnabledFlag := flag.Bool("recommend-enabled", false, "Enable LLM daily recommendations")
	RecommendationDailyLimitFlag := flag.Int("recommend-daily-limit", 10, "Assign default daily recommendation count")
	RecommendationTimezoneFlag := flag.String("recommend-timezone", "Asia/Shanghai", "Assign recommendation timezone")
	RecommendationGenerationTimeFlag := flag.String("recommend-time", "07:00", "Assign daily recommendation generation time")
	RecommendationCandidateWindowDaysFlag := flag.Int("recommend-window-days", 30, "Assign recommendation candidate window in days")
	RecommendationCandidatePoolSizeFlag := flag.Int("recommend-pool-size", 100, "Assign recommendation candidate pool size")
	RecommendationRerankLimitFlag := flag.Int("recommend-rerank-limit", 30, "Assign max candidates sent to LLM reranker")
	RecommendationExplorationRateFlag := flag.Float64("recommend-exploration-rate", 0.15, "Assign recommendation exploration rate")
	LLMBaseURLFlag := flag.String("llm-base-url", "", "Assign OpenAI-compatible LLM base URL")
	LLMAPIKeyFlag := flag.String("llm-api-key", "", "Assign LLM API key")
	LLMChatModelFlag := flag.String("llm-chat-model", "", "Assign LLM chat model")
	LLMEmbeddingModelFlag := flag.String("llm-embedding-model", "", "Assign LLM embedding model")
	LLMTimeoutFlag := flag.String("llm-timeout", "30s", "Assign LLM request timeout")
	EmbeddingDimensionFlag := flag.Int("embedding-dimension", 0, "Assign embedding vector dimension")
	RSSHubBaseURLFlag := flag.String("rsshub-base-url", "", "Assign optional RSSHub base URL")
	flag.Parse()
	DEBUG = *debugFlag
	ARCHIVEFILELOACTION = *ArchiveFileLocationFlag
	MEILIHOST = *MEILIHostFlag
	MEILIAPIKey = *MEILIKeyFlag
	MEILIDumpDir = *MEILIDumpDirFlag
	SINGLEFILEWEBSERVICEURL = strings.TrimRight(*SingleFileWebServiceURLFlag, "/")
	DBHost = *DBHostFlag
	DBPort = *DBPortFlag
	DBName = *DBNameFlag
	DBUser = *DBUserFlag
	DBPassword = *DBPasswordFlag
	DISCOVERYFETCHINTERVAL = *DiscoveryFetchIntervalFlag
	DISCOVERYREQUESTTIMEOUT = *DiscoveryRequestTimeoutFlag
	DISCOVERYMAXCANDIDATES = *DiscoveryMaxCandidatesFlag
	DISCOVERYUSERAGENT = strings.TrimSpace(*DiscoveryUserAgentFlag)
	RECOMMENDATIONENABLED = *RecommendationEnabledFlag
	RECOMMENDATIONDAILYLIMIT = *RecommendationDailyLimitFlag
	RECOMMENDATIONTIMEZONE = strings.TrimSpace(*RecommendationTimezoneFlag)
	RECOMMENDATIONGENERATIONTIME = strings.TrimSpace(*RecommendationGenerationTimeFlag)
	RECOMMENDATIONCANDIDATEWINDOWDAYS = *RecommendationCandidateWindowDaysFlag
	RECOMMENDATIONCANDIDATEPOOLSIZE = *RecommendationCandidatePoolSizeFlag
	RECOMMENDATIONRERANKLIMIT = *RecommendationRerankLimitFlag
	RECOMMENDATIONEXPLORATIONRATE = *RecommendationExplorationRateFlag
	LLMBASEURL = strings.TrimRight(strings.TrimSpace(*LLMBaseURLFlag), "/")
	LLMAPIKEY = strings.TrimSpace(*LLMAPIKeyFlag)
	LLMCHATMODEL = strings.TrimSpace(*LLMChatModelFlag)
	LLMEMBEDDINGMODEL = strings.TrimSpace(*LLMEmbeddingModelFlag)
	LLMTIMEOUT = strings.TrimSpace(*LLMTimeoutFlag)
	EMBEDDINGDIMENSION = *EmbeddingDimensionFlag
	RSSHUBBASEURL = strings.TrimRight(strings.TrimSpace(*RSSHubBaseURLFlag), "/")
}
