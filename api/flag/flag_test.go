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
	oldConfig := []interface{}{
		config.DEBUG, config.ARCHIVEFILELOACTION, config.MEILIHOST, config.MEILIAPIKey, config.MEILIDumpDir,
		config.SINGLEFILEWEBSERVICEURL, config.DBHost, config.DBPort, config.DBName, config.DBUser, config.DBPassword,
		config.RECOMMENDATIONENABLED, config.RECOMMENDATIONDAILYLIMIT, config.RECOMMENDATIONTIMEZONE,
		config.RECOMMENDATIONGENERATIONTIME, config.RECOMMENDATIONCANDIDATEWINDOWDAYS,
		config.RECOMMENDATIONCANDIDATEPOOLSIZE, config.RECOMMENDATIONRERANKLIMIT,
		config.RECOMMENDATIONEXPLORATIONRATE, config.LLMBASEURL, config.LLMAPIKEY, config.LLMCHATMODEL,
		config.LLMEMBEDDINGMODEL, config.LLMTIMEOUT, config.EMBEDDINGDIMENSION, config.RSSHUBBASEURL,
	}
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
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
		config.RECOMMENDATIONENABLED = oldConfig[11].(bool)
		config.RECOMMENDATIONDAILYLIMIT = oldConfig[12].(int)
		config.RECOMMENDATIONTIMEZONE = oldConfig[13].(string)
		config.RECOMMENDATIONGENERATIONTIME = oldConfig[14].(string)
		config.RECOMMENDATIONCANDIDATEWINDOWDAYS = oldConfig[15].(int)
		config.RECOMMENDATIONCANDIDATEPOOLSIZE = oldConfig[16].(int)
		config.RECOMMENDATIONRERANKLIMIT = oldConfig[17].(int)
		config.RECOMMENDATIONEXPLORATIONRATE = oldConfig[18].(float64)
		config.LLMBASEURL = oldConfig[19].(string)
		config.LLMAPIKEY = oldConfig[20].(string)
		config.LLMCHATMODEL = oldConfig[21].(string)
		config.LLMEMBEDDINGMODEL = oldConfig[22].(string)
		config.LLMTIMEOUT = oldConfig[23].(string)
		config.EMBEDDINGDIMENSION = oldConfig[24].(int)
		config.RSSHUBBASEURL = oldConfig[25].(string)
	})

	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
	os.Args = []string{
		"dataark",
		"-debug",
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
		"-embedding-dimension", "768",
		"-rsshub-base-url", "http://rsshub:1200/",
	}

	ParseFlag()

	if !config.DEBUG || config.ARCHIVEFILELOACTION != "/tmp/archive" || config.MEILIHOST != "http://meili:7700" || config.MEILIAPIKey != "key" {
		t.Fatalf("unexpected parsed config: debug=%v loc=%q mhost=%q key=%q", config.DEBUG, config.ARCHIVEFILELOACTION, config.MEILIHOST, config.MEILIAPIKey)
	}
	if config.MEILIDumpDir != "/tmp/dumps" || config.SINGLEFILEWEBSERVICEURL != "http://singlefile" {
		t.Fatalf("unexpected parsed service config: dump=%q singlefile=%q", config.MEILIDumpDir, config.SINGLEFILEWEBSERVICEURL)
	}
	if config.DBHost != "db" || config.DBPort != "5433" || config.DBName != "dataark" || config.DBUser != "user" || config.DBPassword != "pass" {
		t.Fatalf("unexpected parsed db config: host=%q port=%q name=%q user=%q pass=%q", config.DBHost, config.DBPort, config.DBName, config.DBUser, config.DBPassword)
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
}
