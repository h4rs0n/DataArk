package common

import (
	"flag"
	"os"
	"testing"
)

func TestParseFlagAppliesConfiguration(t *testing.T) {
	oldArgs := os.Args
	oldCommandLine := flag.CommandLine
	oldConfig := []interface{}{
		DEBUG, ARCHIVEFILELOACTION, MEILIHOST, MEILIAPIKey, MEILIDumpDir,
		SINGLEFILEWEBSERVICEURL, DBHost, DBPort, DBName, DBUser, DBPassword,
		RECOMMENDATIONENABLED, RECOMMENDATIONDAILYLIMIT, RECOMMENDATIONTIMEZONE,
		RECOMMENDATIONGENERATIONTIME, RECOMMENDATIONCANDIDATEWINDOWDAYS,
		RECOMMENDATIONCANDIDATEPOOLSIZE, RECOMMENDATIONRERANKLIMIT,
		RECOMMENDATIONEXPLORATIONRATE, LLMBASEURL, LLMAPIKEY, LLMCHATMODEL,
		LLMEMBEDDINGMODEL, LLMTIMEOUT, EMBEDDINGDIMENSION, RSSHUBBASEURL,
	}
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
		DEBUG = oldConfig[0].(bool)
		ARCHIVEFILELOACTION = oldConfig[1].(string)
		MEILIHOST = oldConfig[2].(string)
		MEILIAPIKey = oldConfig[3].(string)
		MEILIDumpDir = oldConfig[4].(string)
		SINGLEFILEWEBSERVICEURL = oldConfig[5].(string)
		DBHost = oldConfig[6].(string)
		DBPort = oldConfig[7].(string)
		DBName = oldConfig[8].(string)
		DBUser = oldConfig[9].(string)
		DBPassword = oldConfig[10].(string)
		RECOMMENDATIONENABLED = oldConfig[11].(bool)
		RECOMMENDATIONDAILYLIMIT = oldConfig[12].(int)
		RECOMMENDATIONTIMEZONE = oldConfig[13].(string)
		RECOMMENDATIONGENERATIONTIME = oldConfig[14].(string)
		RECOMMENDATIONCANDIDATEWINDOWDAYS = oldConfig[15].(int)
		RECOMMENDATIONCANDIDATEPOOLSIZE = oldConfig[16].(int)
		RECOMMENDATIONRERANKLIMIT = oldConfig[17].(int)
		RECOMMENDATIONEXPLORATIONRATE = oldConfig[18].(float64)
		LLMBASEURL = oldConfig[19].(string)
		LLMAPIKEY = oldConfig[20].(string)
		LLMCHATMODEL = oldConfig[21].(string)
		LLMEMBEDDINGMODEL = oldConfig[22].(string)
		LLMTIMEOUT = oldConfig[23].(string)
		EMBEDDINGDIMENSION = oldConfig[24].(int)
		RSSHUBBASEURL = oldConfig[25].(string)
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

	if !DEBUG || ARCHIVEFILELOACTION != "/tmp/archive" || MEILIHOST != "http://meili:7700" || MEILIAPIKey != "key" {
		t.Fatalf("unexpected parsed config: debug=%v loc=%q mhost=%q key=%q", DEBUG, ARCHIVEFILELOACTION, MEILIHOST, MEILIAPIKey)
	}
	if MEILIDumpDir != "/tmp/dumps" || SINGLEFILEWEBSERVICEURL != "http://singlefile" {
		t.Fatalf("unexpected parsed service config: dump=%q singlefile=%q", MEILIDumpDir, SINGLEFILEWEBSERVICEURL)
	}
	if DBHost != "db" || DBPort != "5433" || DBName != "dataark" || DBUser != "user" || DBPassword != "pass" {
		t.Fatalf("unexpected parsed db config: host=%q port=%q name=%q user=%q pass=%q", DBHost, DBPort, DBName, DBUser, DBPassword)
	}
	if !RECOMMENDATIONENABLED || RECOMMENDATIONDAILYLIMIT != 12 || RECOMMENDATIONTIMEZONE != "UTC" || RECOMMENDATIONGENERATIONTIME != "06:30" {
		t.Fatalf("unexpected recommendation config: enabled=%v limit=%d timezone=%q time=%q", RECOMMENDATIONENABLED, RECOMMENDATIONDAILYLIMIT, RECOMMENDATIONTIMEZONE, RECOMMENDATIONGENERATIONTIME)
	}
	if RECOMMENDATIONCANDIDATEWINDOWDAYS != 14 || RECOMMENDATIONCANDIDATEPOOLSIZE != 90 || RECOMMENDATIONRERANKLIMIT != 25 || RECOMMENDATIONEXPLORATIONRATE != 0.2 {
		t.Fatalf("unexpected recommendation tuning config")
	}
	if LLMBASEURL != "http://ollama:11434" || LLMAPIKEY != "llm-key" || LLMCHATMODEL != "qwen" || LLMEMBEDDINGMODEL != "embed" || LLMTIMEOUT != "20s" || EMBEDDINGDIMENSION != 768 || RSSHUBBASEURL != "http://rsshub:1200" {
		t.Fatalf("unexpected llm config")
	}
}
