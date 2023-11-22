package main

import (
	"context"
	"errors"
	vkapi "github.com/BadVibessz/vk-api"
	"github.com/SevereCloud/vksdk/v2/callback"
	"github.com/SevereCloud/vksdk/v2/events"
	"github.com/joho/godotenv"
	"golang.org/x/sync/errgroup"
	"log"
	"log/slog"
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

func initVars() {

	var exists bool

	token, exists = os.LookupEnv("VK_API_TOKEN")
	if !exists {
		apputils.HandleFatalError(errors.New("VK_API_TOKEN not specified in env"))
	}

	endpoint, exists = os.LookupEnv("VK_ENDPOINT")
	if !exists {
		apputils.HandleFatalError(errors.New("VK_ENDPOINT not specified in env"))
	}

	version, exists = os.LookupEnv("VK_API_VERSION")
	if !exists {
		apputils.HandleFatalError(errors.New("VK_API_VERSION not specified in env"))
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

func startServerAsync(mainCtx context.Context, bot *app.App) {

	httpServer := &http.Server{
		Addr: ":" + strconv.Itoa(port),
	}

	cb := callback.NewCallback()

	cb.ConfirmationKey = "ccfdcf70"
	cb.MessageNew(func(ctx context.Context, obj events.MessageNewObject) {
		(*bot).HandleMessage(mainCtx, obj)
	})

	http.HandleFunc("/callback", cb.HandleFunc)

	slog.Info("Server started at port:" + strconv.Itoa(port))

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
		slog.Info("exit reason: %s \n", err)
	}
}

func main() {

	loadEnv()
	initVars()

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

		slog.Info("TERMINATING")
		cancel()
		return
	}()

	bot, err := app.NewBot(&vk, configPath)
	if err != nil {
		apputils.HandleFatalError(err)
	}

	// start bot schedule
	go bot.StartAsync(ctx, true)

	// start server for events handling
	wg.Add(1)
	startServerAsync(ctx, &bot)

	wg.Wait()
	cancel()

	slog.Info("App finished")
}
