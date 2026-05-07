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
}
