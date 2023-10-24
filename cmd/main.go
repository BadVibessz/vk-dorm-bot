package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	vkapi "github.com/BadVibessz/vk-api"
	"github.com/go-co-op/gocron"
	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"
	"vk-bot/config"
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

func timingsToString(timings []string) (string, error) {

	if len(timings) == 0 {
		return "", errors.New("timings can not be empty")
	}

	res := ""
	for i, v := range timings {
		res += v
		if i != len(timings)-1 {
			res += ";"
		}
	}
	return res, nil
}

func handleFatalError(err error, logger *log.Logger) {
	logger.Println(err)
	os.Exit(1)
}

func main() {

	loadEnv()

	logger := log.New(os.Stderr, "", 3)

	conf, err := config.Load(configPath)
	if err != nil {
		handleFatalError(err, logger)
	}

	token, exists := os.LookupEnv("VK_API_TOKEN")
	if !exists {
		handleFatalError(errors.New("VK_API_TOKEN not specified in env"), logger)
	}

	endpoint, exists := os.LookupEnv("VK_ENDPOINT")
	if !exists {
		handleFatalError(errors.New("VK_ENDPOINT not specified in env"), logger)
	}

	version, exists := os.LookupEnv("VK_API_VERSION")
	if !exists {
		handleFatalError(errors.New("VK_API_VERSION not specified in env"), logger)
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

	f := func() {

		msg := "мбепнники: "

		for _, room := range conf.Rooms {

			msg += room.Number + "\n"
			for _, member := range room.Members {
				msg += "*" + member.Id + "(" + member.Name + ")\n"
			}

		}

		// todo: context with timeout?
		resp, err := vk.SendMessage(ctx, vkapi.Params{
			"message":   msg,
			"random_id": "0",
			"peer_id":   strconv.Itoa(2000000000 + 1), // TODO: dynamically retrieve chat id by name
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

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()

		timings, err := timingsToString(conf.Timings)
		if err != nil {
			// todo: send me log messages in vk direct?
			fmt.Println(err)

			// default timings if timings are not provided in conf file
			timings = "7:30;19:30"
		}

		//job, err := s.Every(5).Seconds().WaitForSchedule().Do(f)
		job, err := s.Every(conf.Frequency).Day().At(timings).At("19:21").Do(f)
		if err != nil {
			logger.Println(err)
		}

		//s.WaitForScheduleAll()
		fmt.Printf("Job: %v, Error: %v", job, err)
		s.StartBlocking()
	}()

	// todo: schedule testing
	wg.Add(1)
	go func() {
		defer wg.Done()

		fmt.Println("a")

		ff := func() {

			msg := "мбепнники: "

			ind := slices.IndexFunc(conf.Rooms, func(r config.Room) bool { return r.Number == conf.Current })

			room := conf.Rooms[ind]

			msg += room.Number + "\n"
			for _, member := range room.Members {
				msg += "*" + member.Id + "(" + member.Name + ")\n"
			}

			fmt.Println(msg)

			// update current room
			nextInd := (ind + 1) % len(conf.Rooms)

			conf.Current = conf.Rooms[nextInd].Number

			serialized, err := yaml.Marshal(&conf)
			if err != nil {
				// todo: handle somehow
				logger.Println(err)
			}

			// overwrite existing config
			file, err := os.Create(configPath)
			if err != nil {
				// todo: handle somehow
				logger.Println(err)
			}

			_, err = file.Write(serialized)
			if err != nil {
				// todo: handle somehow
				logger.Println(err)
			}

		}

		_, err := s.Every(2).Seconds().WaitForSchedule().Do(ff)
		if err != nil {
			logger.Println(err)
		}
		s.StartBlocking()

	}()

	wg.Wait()

}
