package main

import (
	"context"
	"errors"
	"fmt"
	vkapi "github.com/BadVibessz/vk-api"
	"github.com/SevereCloud/vksdk/v2/callback"
	"github.com/SevereCloud/vksdk/v2/events"
	"github.com/joho/godotenv"
	"golang.org/x/sync/errgroup"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"vk-bot/internal/app"
	apputils "vk-bot/pkg/utils/app"
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

func startServerAsync(mainCtx context.Context, bot *app.App, logger *log.Logger) {

	httpServer := &http.Server{
		Addr: ":" + strconv.Itoa(port),
	}

	cb := callback.NewCallback()

	cb.ConfirmationKey = "538d2804"
	cb.MessageNew(func(ctx context.Context, obj events.MessageNewObject) {
		(*bot).HandleMessage(mainCtx, obj, logger)
	})

	http.HandleFunc("/callback", cb.HandleFunc)

	logger.Println("Server started at port:" + strconv.Itoa(port))

	eg, gCtx := errgroup.WithContext(mainCtx)

	// start server
	eg.Go(func() error {
		return httpServer.ListenAndServe()
	})

	// listen for ctx cancellation
	eg.Go(func() error {
		<-gCtx.Done()
		return httpServer.Shutdown(context.Background())
	})

	if err := eg.Wait(); err != nil {
		fmt.Printf("exit reason: %s \n", err)
	}
}

func main() {

	logger := log.New(os.Stderr, "", 3)

	loadEnv()
	initVars(logger)

	wg := sync.WaitGroup{}
	ctx, cancel := context.WithCancel(context.Background())

	// todo: graceful shutdown
	// listen to termination signal and exit gracefully
	go func() {
		exit := make(chan os.Signal, 1)
		signal.Notify(exit, os.Interrupt, syscall.SIGTERM) // todo: read more about signal.notify! https://pkg.go.dev/os/signal
		<-exit

		//ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		//defer cancel()
		//

		print("\nTERMINATING")
		cancel()
		return
	}()

	bot, err := app.NewBot(&vk, configPath)
	if err != nil {
		apputils.HandleFatalError(err, logger)
	}

	// start bot schedule
	go bot.StartAsync(ctx, logger, true)

	// start server for events handling
	wg.Add(1)
	startServerAsync(ctx, &bot, logger)

	wg.Wait()
	cancel()
	println("App finished")
}
