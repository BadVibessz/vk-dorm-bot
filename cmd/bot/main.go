package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	vkapi "github.com/BadVibessz/vk-api"
	"github.com/joho/godotenv"
	"log"
	"net/http"
	"os"
	"sync"
	"vk-bot/internal/app"
	apputils "vk-bot/pkg/utils/app"
)

const (
	configPath = "config/bot-config.yml"
)

func PrettyString(str string) (string, error) {
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, []byte(str), "", "    "); err != nil {
		return "", err
	}
	return prettyJSON.String(), nil
}

func loadEnv() {
	if err := godotenv.Load(); err != nil {
		log.Print("no .env file found")
		os.Exit(1)
	}
}

//func moscowToUtc(h, m int8) (string, error) {
//	// todo: return err if args is incorrect
//
//	// todo:
//	h -= 3
//	return "h"
//
//}

func main() {

	loadEnv()

	logger := log.New(os.Stderr, "", 3)

	// todo: encapsulate
	token, exists := os.LookupEnv("VK_API_TOKEN")
	if !exists {
		apputils.HandleFatalError(errors.New("VK_API_TOKEN not specified in env"), logger)
	}

	endpoint, exists := os.LookupEnv("VK_ENDPOINT")
	if !exists {
		apputils.HandleFatalError(errors.New("VK_ENDPOINT not specified in env"), logger)
	}

	version, exists := os.LookupEnv("VK_API_VERSION")
	if !exists {
		apputils.HandleFatalError(errors.New("VK_API_VERSION not specified in env"), logger)
	}

	h := http.Client{}
	client := vkapi.Client{
		Http:       &h,
		BaseURL:    endpoint,
		Retry:      false,
		RetryCount: 0}

	ctx := context.Background()

	vk := vkapi.VkAPI{
		Token:   token,
		Version: version,
		Client:  &client,
	}

	bot, err := app.NewBot(&vk, configPath)
	if err != nil {
		apputils.HandleFatalError(err, logger)
	}

	wg := sync.WaitGroup{}

	bot.StartAsync(ctx, &wg, logger)

	wg.Wait()

}
