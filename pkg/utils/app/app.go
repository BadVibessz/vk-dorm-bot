package app

import (
	"log"
	"os"
)

func HandleFatalError(err error, logger *log.Logger) {
	logger.Println(err)
	os.Exit(1)
}
