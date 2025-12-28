package main

import (
	"log"

	"github.com/nerdneilsfield/telegram-upload-watcher/go/cmd"
)

var (
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	log.SetFlags(log.LstdFlags)
	if err := cmd.Execute(version, buildTime, gitCommit); err != nil {
		log.Fatal(err)
	}
}
