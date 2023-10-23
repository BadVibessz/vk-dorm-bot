package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	vkapi "github.com/BadVibessz/vk-api"
	"github.com/go-co-op/gocron"
	"github.com/joho/godotenv"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
	"vk-bot/config"
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

	logger := log.New(os.Stderr, "", 3)

	config, err := config.Load("../config/bot-config.yml")
	if err != nil {
		logger.Println("VK_API_TOKEN not specified in env")
		os.Exit(1)
	}

	println(config.Rooms)

	loadEnv()

	token, exists := os.LookupEnv("VK_API_TOKEN")
	if !exists {
		logger.Println("VK_API_TOKEN not specified in env")
		os.Exit(1)
	}

	endpoint, exists := os.LookupEnv("VK_ENDPOINT")
	if !exists {
		logger.Println("VK_ENDPOINT not specified in env")
		os.Exit(1)
	}

	version, exists := os.LookupEnv("VK_API_VERSION")
	if !exists {
		logger.Println("VK_API_VERSION not specified in env")
		os.Exit(1)
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

	s := gocron.NewScheduler(time.UTC)

	rooms := make(map[string][2]string)
	rooms["393"] = [2]string{"*rooccat(Данил)", "*maxim.zaytsev2013(Максим)"}

	f := func() {

		msg := "мбепнники: "

		for _, v := range rooms["393"] {
			msg += v + " "
		}

		resp, err := vk.SendMessage(ctx, vkapi.Params{
			"message":   msg,
			"random_id": "0",
			"peer_id":   strconv.Itoa(2000000000 + 1),
		})
		if err != nil {
			logger.Println(err)
		}

		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			logger.Println(err)
		}
		fmt.Println(PrettyString(string(buf)))
	}

	day := "7:30"
	evening := "19:30"

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()

		//job, err := s.Every(5).Seconds().WaitForSchedule().Do(f)
		job, err := s.Every(1).Day().At(day).At(evening).At("13:14").Do(f)
		if err != nil {
			logger.Println(err)
		}
		//s.WaitForScheduleAll()
		fmt.Printf("Job: %v, Error: %v", job, err)
		s.StartBlocking()
	}()
	wg.Wait()

}
