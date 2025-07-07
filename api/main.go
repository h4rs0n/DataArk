package main

import (
	"DataArk/api"
	"DataArk/common"
	"fmt"
)

func main() {
	display_banner()
	common.ParseFlag()
	api.WebStarter(common.DEBUG)
}

func display_banner() {
	fmt.Println("  _____     _             _    ____  _  __")
	fmt.Println(" | ____|___| |__   ___   / \\  |  _ \\| |/ /")
	fmt.Println(" |  _| / __| '_ \\ / _ \\ / _ \\ | |_) | ' / ")
	fmt.Println(" | |__| (__| | | | (_) / ___ \\|  _ <| . \\ ")
	fmt.Println(" |_____\\___|_| |_|\\___/_/   \\_\\_| \\_\\_|\\_\\")
	fmt.Println("                                          ")
}
