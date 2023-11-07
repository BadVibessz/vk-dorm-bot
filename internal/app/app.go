package app

import (
	"context"
	"fmt"
	vkapi "github.com/BadVibessz/vk-api"
	"github.com/go-co-op/gocron"
	"io"
	"log"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"
	"vk-bot/internal/config"
	stringutils "vk-bot/pkg/utils/string"
)

type App interface {
	SendMessageToChat(ctx context.Context, msg string, chatID, randID int) (*http.Response, error)

	HandleMessage(ctx context.Context, msg string)

	NotifyAboutDuty(ctx context.Context, chatId int, room config.Room) (*http.Response, error)

	StartAsync(ctx context.Context, wg *sync.WaitGroup, logger *log.Logger, sendLogs bool)
	// todo: general function for starting any task with any schedule
}

type BotService struct {
	ConfigPath string
	Conf       *config.Config

	VK *vkapi.VkAPI
}

func NewBot(vk *vkapi.VkAPI, configPath string) (App, error) {

	conf, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	return &BotService{
		ConfigPath: configPath,
		Conf:       conf,
		VK:         vk,
	}, nil
}

func (b *BotService) sendLog(ctx context.Context, log string) (*http.Response, error) {
	return b.sendMessage(ctx, log, b.Conf.Dad, 0)
}

func (b *BotService) sendMessage(ctx context.Context, msg string, peerId, randID int) (*http.Response, error) {

	var i int8
	var resp *http.Response
	var err error

	for i = 0; i < b.Conf.Retries+1; i++ {

		resp, err = b.VK.SendMessage(ctx, vkapi.Params{
			"message":   msg,
			"random_id": strconv.Itoa(randID),
			"peer_id":   strconv.Itoa(peerId),
		})
		if err != nil {

			// context done
			if ctx.Err() != nil {
				return nil, err
			}
			// else wait and then retry
			time.Sleep(time.Duration(b.Conf.RetryInterval) * time.Second)
		} else {
			break
		}

	}
	return resp, nil
}

func (b *BotService) SendMessageToChat(ctx context.Context, msg string, chatID, randID int) (*http.Response, error) {
	return b.sendMessage(ctx, msg, 2000000000+chatID, randID)
}

func (b *BotService) HandleMessage(ctx context.Context, msg string) {
	//TODO implement me
	panic("implement me")
}

func (b *BotService) NotifyAboutDuty(ctx context.Context, chatId int, room config.Room) (*http.Response, error) {

	msg := "Дежурные: " + room.Number + "\n"
	for _, member := range room.Members {
		msg += "*" + member.Id + "(" + member.Name + ")\n"
	}

	// TODO: dynamically retrieve chat id by name
	resp, err := b.SendMessageToChat(ctx, msg, chatId, 0)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (b *BotService) StartScheduledTaskAsync(task func()) { // todo: code generation?
}

// todo: call generic method StartTaskAsync(task func(), ctx ...)
func (b *BotService) StartAsync(ctx context.Context, wg *sync.WaitGroup, logger *log.Logger, sendLogs bool) {

	s := gocron.NewScheduler(time.UTC)

	task := func() {

		ind := slices.IndexFunc(b.Conf.Rooms, func(r config.Room) bool { return r.Number == b.Conf.Current })
		room := b.Conf.Rooms[ind]

		// TODO: dynamically retrieve chat id by name
		resp, err := b.NotifyAboutDuty(ctx, 1, room)
		if err != nil {
			logger.Println(err)

			if sendLogs {
				_, logErr := b.sendLog(ctx, err.Error())
				if logErr != nil {
					// todo: handle
				}
			}

		}

		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			logger.Println(err)
		}
		fmt.Println(stringutils.PrettyString(string(buf)))

		// update current room
		nextInd := (ind + 1) % len(b.Conf.Rooms)
		b.Conf.Current = b.Conf.Rooms[nextInd].Number

		// overwrite existing config
		err = b.Conf.Save(b.ConfigPath)
		if err != nil {
			logger.Println(err)
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		timings, err := stringutils.TimingsToString(b.Conf.Timings)
		if err != nil {
			// todo: send me log messages in vk direct?
			fmt.Println(err)

			// default timings if timings are not provided in conf file // todo: in .env?
			timings = "7:30;19:30"
		}

		_, err = s.Every(b.Conf.Frequency).Day().At(timings).Do(task)
		if err != nil {
			logger.Println(err)
		}

		s.StartBlocking()
	}()

	//ff := func() {
	//
	//	msg := "мбепнники: "
	//
	//	ind := slices.IndexFunc(b.Conf.Rooms, func(r config.Room) bool { return r.Number == b.Conf.Current })
	//	room := b.Conf.Rooms[ind]
	//
	//	msg += room.Number + "\n"
	//	for _, member := range room.Members {
	//		msg += "*" + member.Id + "(" + member.Name + ")\n"
	//	}
	//
	//	fmt.Println(msg)
	//
	//	// update current room
	//	nextInd := (ind + 1) % len(b.Conf.Rooms)
	//	b.Conf.Current = b.Conf.Rooms[nextInd].Number
	//
	//	// overwrite existing config
	//	err := b.Conf.Save(b.ConfigPath)
	//	if err != nil {
	//		logger.Println(err)
	//	}
	//
	//}
	//
	//wg.Add(1)
	//go func() {
	//	defer wg.Done()
	//
	//	_, err := s.Every(b.Conf.Frequency).Seconds().WaitForSchedule().Do(ff)
	//	if err != nil {
	//		logger.Println(err)
	//	}
	//	s.StartBlocking()
	//
	//}()

}
