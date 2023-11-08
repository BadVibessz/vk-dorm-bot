package main

import (
	"context"
	"errors"
	vkapi "github.com/BadVibessz/vk-api"
	"github.com/joho/godotenv"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"vk-bot/internal/app"
	apputils "vk-bot/pkg/utils/app"

	"github.com/SevereCloud/vksdk/v2/callback"
	"github.com/SevereCloud/vksdk/v2/events"
)

const (
	configPath = "config/bot-config.yml"
	port       = 8080
)

var (
	token    string
	endpoint string
	version  string

	vk vkapi.VkAPI
)

func loadEnv() {
	if err := godotenv.Load(); err != nil {
		log.Print("no .env file found")
		os.Exit(1)
	}
}

func initVars(logger *log.Logger) {

	var exists bool

	token, exists = os.LookupEnv("VK_API_TOKEN")
	if !exists {
		apputils.HandleFatalError(errors.New("VK_API_TOKEN not specified in env"), logger)
	}

	endpoint, exists = os.LookupEnv("VK_ENDPOINT")
	if !exists {
		apputils.HandleFatalError(errors.New("VK_ENDPOINT not specified in env"), logger)
	}

	version, exists = os.LookupEnv("VK_API_VERSION")
	if !exists {
		apputils.HandleFatalError(errors.New("VK_API_VERSION not specified in env"), logger)
	}

	h := http.Client{}
	client := vkapi.Client{
		Http:       &h,
		BaseURL:    endpoint,
		Retry:      false,
		RetryCount: 0}

	vk = vkapi.VkAPI{
		Token:   token,
		Version: version,
		Client:  &client,
	}

}

func startBot(logger *log.Logger) {

}

func startServer(bot *app.App, logger *log.Logger) {

	cb := callback.NewCallback()

	cb.ConfirmationKey = "cdaf39ca"

	cb.MessageNew(func(ctx context.Context, obj events.MessageNewObject) {
		(*bot).HandleMessage(ctx, obj, logger)
	})

	http.HandleFunc("/callback", cb.HandleFunc)

	logger.Println("Server started at port:" + strconv.Itoa(port))

	err := http.ListenAndServe(":"+strconv.Itoa(port), nil)
	if err != nil {
		apputils.HandleFatalError(err, logger)
	}
}

func main() {

	logger := log.New(os.Stderr, "", 3)

	loadEnv()
	initVars(logger)

	bot, err := app.NewBot(&vk, configPath)
	if err != nil {
		apputils.HandleFatalError(err, logger)
	}

	wg := sync.WaitGroup{}
	ctx := context.Background()

	// start bot schedule
	bot.StartAsync(ctx, &wg, logger, true)

	// start server for events handling
	wg.Add(1)
	go startServer(&bot, logger)

	wg.Wait()
	println("App finished")
}
