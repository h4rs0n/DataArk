package common

import "flag"

func ParseFlag() {
	debugFlag := flag.Bool("debug", false, "Enable debug mode")
	ArchiveFileLocationFlag := flag.String("loc", "./api/static/archive/", "Assign HTML file path")
	MEILIHostFlag := flag.String("mhost", "http://127.0.0.1:7700", "Assign MeiliSearch host")
	MEILIKeyFlag := flag.String("mkey", "", "Assign MeiliSearch API key")
	flag.Parse()
	DEBUG = *debugFlag
	ARCHIVEFILELOACTION = *ArchiveFileLocationFlag
	MEILIHOST = *MEILIHostFlag
	MEILIAPIKey = *MEILIKeyFlag
}
