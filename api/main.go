package main

import (
	"EchoArk/api"
	"EchoArk/common"
)

func main() {
	common.ParseFlag()
	api.WebStarter(common.DEBUG)
}
