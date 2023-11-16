package app

import (
	"log/slog"
	"os"
)

func HandleFatalError(err error) {
	slog.Error(err.Error())
	os.Exit(1)
}
