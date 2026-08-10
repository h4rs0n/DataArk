package flag

import (
	"DataArk/config"
	"flag"
	"os"
	"testing"
)

func TestParseFlagAppliesConfiguration(t *testing.T) {
	oldArgs := os.Args
	oldCommandLine := flag.CommandLine
	oldLogDir := config.LOGDIR
	oldLogRetentionDays := config.LOGRETENTIONDAYS
	oldConfig := []interface{}{
		config.DEBUG, config.ARCHIVEFILELOACTION, config.MEILIHOST, config.MEILIAPIKey, config.MEILIDumpDir,
		config.SINGLEFILEWEBSERVICEURL, config.DBHost, config.DBPort, config.DBName, config.DBUser, config.DBPassword,
		config.DISCOVERYSOCKS5PROXY,
		config.RECOMMENDATIONENABLED, config.RECOMMENDATIONDAILYLIMIT, config.RECOMMENDATIONTIMEZONE,
		config.RECOMMENDATIONGENERATIONTIME, config.RECOMMENDATIONCANDIDATEWINDOWDAYS,
		config.RECOMMENDATIONCANDIDATEPOOLSIZE, config.RECOMMENDATIONRERANKLIMIT,
		config.RECOMMENDATIONEXPLORATIONRATE, config.LLMBASEURL, config.LLMAPIKEY, config.LLMCHATMODEL,
		config.LLMEMBEDDINGMODEL, config.LLMTIMEOUT, config.EMBEDDINGDIMENSION, config.RSSHUBBASEURL,
		config.ARTICLEASSESSMENTMODE, config.ARTICLEASSESSMENTCONCURRENCY,
	}
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
		config.LOGDIR = oldLogDir
		config.LOGRETENTIONDAYS = oldLogRetentionDays
		config.DEBUG = oldConfig[0].(bool)
		config.ARCHIVEFILELOACTION = oldConfig[1].(string)
		config.MEILIHOST = oldConfig[2].(string)
		config.MEILIAPIKey = oldConfig[3].(string)
		config.MEILIDumpDir = oldConfig[4].(string)
		config.SINGLEFILEWEBSERVICEURL = oldConfig[5].(string)
		config.DBHost = oldConfig[6].(string)
		config.DBPort = oldConfig[7].(string)
		config.DBName = oldConfig[8].(string)
		config.DBUser = oldConfig[9].(string)
		config.DBPassword = oldConfig[10].(string)
		config.DISCOVERYSOCKS5PROXY = oldConfig[11].(string)
		config.RECOMMENDATIONENABLED = oldConfig[12].(bool)
		config.RECOMMENDATIONDAILYLIMIT = oldConfig[13].(int)
		config.RECOMMENDATIONTIMEZONE = oldConfig[14].(string)
		config.RECOMMENDATIONGENERATIONTIME = oldConfig[15].(string)
		config.RECOMMENDATIONCANDIDATEWINDOWDAYS = oldConfig[16].(int)
		config.RECOMMENDATIONCANDIDATEPOOLSIZE = oldConfig[17].(int)
		config.RECOMMENDATIONRERANKLIMIT = oldConfig[18].(int)
		config.RECOMMENDATIONEXPLORATIONRATE = oldConfig[19].(float64)
		config.LLMBASEURL = oldConfig[20].(string)
		config.LLMAPIKEY = oldConfig[21].(string)
		config.LLMCHATMODEL = oldConfig[22].(string)
		config.LLMEMBEDDINGMODEL = oldConfig[23].(string)
		config.LLMTIMEOUT = oldConfig[24].(string)
		config.EMBEDDINGDIMENSION = oldConfig[25].(int)
		config.RSSHUBBASEURL = oldConfig[26].(string)
		config.ARTICLEASSESSMENTMODE = oldConfig[27].(string)
		config.ARTICLEASSESSMENTCONCURRENCY = oldConfig[28].(int)
	})

	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	os.Args = []string{
		"dataark",
		"-debug",
		"-log-dir", " /tmp/dataark-logs ",
		"-log-retention-days", "14",
		"-loc", "/tmp/archive",
		"-mhost", "http://meili:7700",
		"-mkey", "key",
		"-mdump", "/tmp/dumps",
		"-sfhost", "http://singlefile/",
		"-dbhost", "db",
		"-dbport", "5433",
		"-dbname", "dataark",
		"-dbuser", "user",
		"-dbpasswd", "pass",
		"-discover-socks5-proxy", "socks5://proxy-user:proxy-pass@127.0.0.1:1080",
		"-recommend-enabled",
		"-recommend-daily-limit", "12",
		"-recommend-timezone", "UTC",
		"-recommend-time", "06:30",
		"-recommend-window-days", "14",
		"-recommend-pool-size", "90",
		"-recommend-rerank-limit", "25",
		"-recommend-exploration-rate", "0.2",
		"-llm-base-url", "http://ollama:11434/",
		"-llm-api-key", "llm-key",
		"-llm-chat-model", "qwen",
		"-llm-embedding-model", "embed",
		"-llm-timeout", "20s",
		"-article-assessment-mode", "active",
		"-article-assessment-concurrency", "3",
		"-embedding-dimension", "768",
		"-rsshub-base-url", "http://rsshub:1200/",
	}

	ParseFlag()

	if !config.DEBUG || config.ARCHIVEFILELOACTION != "/tmp/archive" || config.MEILIHOST != "http://meili:7700" || config.MEILIAPIKey != "key" {
		t.Fatalf("unexpected parsed config: debug=%v loc=%q mhost=%q key=%q", config.DEBUG, config.ARCHIVEFILELOACTION, config.MEILIHOST, config.MEILIAPIKey)
	}
	if config.LOGDIR != "/tmp/dataark-logs" || config.LOGRETENTIONDAYS != 14 {
		t.Fatalf("unexpected log config: dir=%q retention=%d", config.LOGDIR, config.LOGRETENTIONDAYS)
	}
	if config.MEILIDumpDir != "/tmp/dumps" || config.SINGLEFILEWEBSERVICEURL != "http://singlefile" {
		t.Fatalf("unexpected parsed service config: dump=%q singlefile=%q", config.MEILIDumpDir, config.SINGLEFILEWEBSERVICEURL)
	}
	if config.DBHost != "db" || config.DBPort != "5433" || config.DBName != "dataark" || config.DBUser != "user" || config.DBPassword != "pass" {
		t.Fatalf("unexpected parsed db config: host=%q port=%q name=%q user=%q pass=%q", config.DBHost, config.DBPort, config.DBName, config.DBUser, config.DBPassword)
	}
	if config.DISCOVERYSOCKS5PROXY != "socks5://proxy-user:proxy-pass@127.0.0.1:1080" {
		t.Fatalf("unexpected discovery SOCKS5 proxy: %q", config.DISCOVERYSOCKS5PROXY)
	}
	if config.DISCOVERYUSERAGENT != config.DefaultDiscoveryUserAgent {
		t.Fatalf("unexpected discovery user-agent: %q", config.DISCOVERYUSERAGENT)
	}
	if !config.RECOMMENDATIONENABLED || config.RECOMMENDATIONDAILYLIMIT != 12 || config.RECOMMENDATIONTIMEZONE != "UTC" || config.RECOMMENDATIONGENERATIONTIME != "06:30" {
		t.Fatalf("unexpected recommendation config: enabled=%v limit=%d timezone=%q time=%q", config.RECOMMENDATIONENABLED, config.RECOMMENDATIONDAILYLIMIT, config.RECOMMENDATIONTIMEZONE, config.RECOMMENDATIONGENERATIONTIME)
	}
	if config.RECOMMENDATIONCANDIDATEWINDOWDAYS != 14 || config.RECOMMENDATIONCANDIDATEPOOLSIZE != 90 || config.RECOMMENDATIONRERANKLIMIT != 25 || config.RECOMMENDATIONEXPLORATIONRATE != 0.2 {
		t.Fatalf("unexpected recommendation tuning config")
	}
	if config.LLMBASEURL != "http://ollama:11434" || config.LLMAPIKEY != "llm-key" || config.LLMCHATMODEL != "qwen" || config.LLMEMBEDDINGMODEL != "embed" || config.LLMTIMEOUT != "20s" || config.EMBEDDINGDIMENSION != 768 || config.RSSHUBBASEURL != "http://rsshub:1200" {
		t.Fatalf("unexpected llm config")
	}
	if config.ARTICLEASSESSMENTMODE != "active" || config.ARTICLEASSESSMENTCONCURRENCY != 3 {
		t.Fatalf("unexpected assessment config: mode=%q concurrency=%d", config.ARTICLEASSESSMENTMODE, config.ARTICLEASSESSMENTCONCURRENCY)
	}
}
