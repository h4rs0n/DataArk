package common

import "flag"

func ParseFlag() {
	debugFlag := flag.Bool("debug", false, "Enable debug mode")
	ArchiveFileLocationFlag := flag.String("loc", "./api/static/archive/", "Assign HTML file path")
	MEILIHostFlag := flag.String("mhost", "http://127.0.0.1:7700", "Assign MeiliSearch host")
	MEILIKeyFlag := flag.String("mkey", "", "Assign MeiliSearch API key")
	DBHostFlag := flag.String("dbhost", "localhost", "Assign DB host")
	DBPortFlag := flag.String("dbport", "5432", "Assign DB port")
	DBNameFlag := flag.String("dbname", "echoark", "Assign DB name")
	DBUserFlag := flag.String("dbuser", "postgres", "Assign DB user")
	DBPasswordFlag := flag.String("dbpasswd", "postgres", "Assign DB password")
	flag.Parse()
	DEBUG = *debugFlag
	ARCHIVEFILELOACTION = *ArchiveFileLocationFlag
	MEILIHOST = *MEILIHostFlag
	MEILIAPIKey = *MEILIKeyFlag
	DBHost = *DBHostFlag
	DBPort = *DBPortFlag
	DBName = *DBNameFlag
	DBUser = *DBUserFlag
	DBPassword = *DBPasswordFlag
}
