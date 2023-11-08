package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	vkapi "github.com/BadVibessz/vk-api"
	"github.com/SevereCloud/vksdk/v2/events"
	_ "github.com/SevereCloud/vksdk/v2/events"
	"github.com/go-co-op/gocron"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"vk-bot/internal/config"
)

type App interface {
	NotifyAboutDuty(ctx context.Context, chatId int, room *config.Room) (*http.Response, error)
	NotifyAboutCleaning(ctx context.Context, chatId int) (*http.Response, error)

	SendMessageToChat(ctx context.Context, msg string, chatID, randID int) (*http.Response, error)
	SwapRooms(ctx context.Context, roomName1 string, roomName2 string) error

	StartAsync(ctx context.Context, wg *sync.WaitGroup, logger *log.Logger, sendLogs bool)
	HandleMessage(ctx context.Context, obj events.MessageNewObject, logger *log.Logger)

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

func (b *BotService) getQueueString() string {

	var res string
	for i, v := range b.Conf.Rooms {

		res += " "

		if v.Number == b.Conf.Current {
			res += "[" + v.Number + "]"
		} else {
			res += v.Number
		}

		if i != len(b.Conf.Rooms)-1 {
			res += " ->"
		}
	}
	return res
}

func (b *BotService) HandleMessage(ctx context.Context, obj events.MessageNewObject, logger *log.Logger) {

	msg := obj.Message.Text
	from := obj.Message.FromID

	log.Println(msg)

	spltd := strings.Split(msg, " ")

	switch spltd[0] {

	case "/echo": // todo:
		_, err := b.sendMessage(ctx, msg, from, 0)
		if err != nil {
			logger.Println(err)
		}
		break

	// sort queue ascending
	case "/reset":
		b.resetQueue()

		_, err := b.sendMessage(ctx, "Порядок комнат восстановлен по умолчанию", from, 0)
		if err != nil {
			logger.Println(err)
		}

		break

	// set current room
	case "/setcurrent":

		// validate input
		curr := spltd[1]
		if slices.IndexFunc(b.Conf.Rooms, func(r config.Room) bool { return r.Number == curr }) != -1 {
			b.Conf.Current = curr

			resp := "Номер текущей комнаты успешно изменен" + "\n" + "Новый порядок:\n" + b.getQueueString()
			_, err := b.sendMessage(ctx, resp, from, 0)
			if err != nil {
				logger.Println(err)
			}

		} else {
			_, err := b.sendMessage(ctx, "Некорректный номер комнаты", from, 0)
			if err != nil {
				logger.Println(err)
			}
		}
		break

	case "/queue":
		_, err := b.sendMessage(ctx, b.getQueueString(), from, 0)
		if err != nil {
			logger.Println(err)
		}
		break

	case "/swap":
		err := b.SwapRooms(ctx, spltd[1], spltd[2])
		if err != nil {
			_, err := b.sendMessage(ctx, "Некорректный номер комнаты", from, 0)
			if err != nil {
				logger.Println(err)
			}
		} else {
			resp := "Сделано!" + "\n" + "Новый порядок:\n" + b.getQueueString()
			_, err := b.sendMessage(ctx, resp, from, 0)
			if err != nil {
				logger.Println(err)
			}

		}
		break

	case "/freq":

		if n, err := strconv.Atoi(spltd[1]); err == nil && n <= 7 && n > 0 {

			b.Conf.Frequency = n

			err = b.saveConfig()
			if err != nil {
				logger.Println(err)
			}
		}

		break
	}

}

func (b *BotService) isSwapPending() bool {

	for _, v := range b.Conf.Rooms {
		if v.SwapPending {
			return true
		}
	}
	return false
}

func (b *BotService) NotifyAboutCleaning(ctx context.Context, chatId int) (*http.Response, error) {
	//TODO implement me
	panic("implement me")
}

func (b *BotService) NotifyAboutDuty(ctx context.Context, chatId int, room *config.Room) (*http.Response, error) {

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

func (b *BotService) SwapRooms(ctx context.Context, roomName1 string, roomName2 string) error {

	m := sync.Mutex{} // todo: where to define mutex?

	m.Lock()

	ind1 := slices.IndexFunc(b.Conf.Rooms, func(r config.Room) bool { return r.Number == roomName1 })
	ind2 := slices.IndexFunc(b.Conf.Rooms, func(r config.Room) bool { return r.Number == roomName2 })

	if ind1 == -1 || ind2 == -1 {
		return errors.New("swap: no such room")
	}
	currInd := slices.IndexFunc(b.Conf.Rooms, func(r config.Room) bool { return r.Number == b.Conf.Current })

	newQueue := slices.Clone(b.Conf.Rooms)

	newQueue[ind1] = b.Conf.Rooms[ind2]
	newQueue[ind2] = b.Conf.Rooms[ind1]

	newQueue[ind1].SwapPending = true
	newQueue[ind2].SwapPending = true

	b.Conf.Rooms = newQueue
	m.Unlock()

	// if we swapped current room -> necessarily to overwrite schedule
	if ind1 == currInd {
		b.Conf.Current = b.Conf.Rooms[currInd].Number
		// b.saveConfig(logger)
	}

	println("\nsuccessfully swapped room " + roomName1 + " with " + roomName2)
	println("\ncurrent schedule:")
	for _, v := range b.Conf.Rooms {
		if v.Number == b.Conf.Current {
			print("*")
		}
		print(v.Number + " ")
	}
	return nil
}

func (b *BotService) saveConfig() error {
	err := b.Conf.Save(b.ConfigPath)
	if err != nil {
		return err
	}
	return nil
}

func (b *BotService) resetQueue() {
	slices.SortFunc(b.Conf.Rooms, func(r1, r2 config.Room) int {
		return cmp.Compare(r1.Number, r2.Number)
	})
}

func (b *BotService) updateQueue(currentInd int, logger *log.Logger) {

	currentRoom := &b.Conf.Rooms[currentInd]

	// swap not pending anymore
	if currentRoom.SwapPending {
		currentRoom.SwapPending = false

		// if room done its swapped duty -> it can be placed in its native order // todo:?
		//b.Conf.
	}

	// TODO:
	// if no swaps pending -> reset queue
	if !b.isSwapPending() {
		b.resetQueue()
	}

	// update current room
	nextInd := (currentInd + 1) % len(b.Conf.Rooms)
	b.Conf.Current = b.Conf.Rooms[nextInd].Number

	println("\ncurrent schedule:")
	for _, v := range b.Conf.Rooms {
		if v.Number == b.Conf.Current {
			print("*")
		}
		print(v.Number + " ")
	}

	// overwrite existing config
	err := b.saveConfig()
	if err != nil {
		logger.Println(err)
	}
}

// todo: call generic method StartTaskAsync(task func(), ctx ...)
func (b *BotService) StartAsync(ctx context.Context, wg *sync.WaitGroup, logger *log.Logger, sendLogs bool) {

	s := gocron.NewScheduler(time.UTC)

	//task := func() {
	//
	//	ind := slices.IndexFunc(b.Conf.Rooms, func(r config.Room) bool { return r.Number == b.Conf.Current })
	//	room := b.Conf.Rooms[ind]
	//
	//	// TODO: dynamically retrieve chat id by name
	//	resp, err := b.NotifyAboutDuty(ctx, 1, &room)
	//	if err != nil {
	//		logger.Println(err)
	//
	//		if sendLogs {
	//			_, logErr := b.sendLog(ctx, "notify about duty: "+err.Error())
	//			if logErr != nil {
	//				logger.Println(logErr) // todo: log to file
	//			}
	//		}
	//	}
	//
	//	buf, err := io.ReadAll(resp.Body)
	//	if err != nil {
	//		logger.Println(err)
	//	}
	//	fmt.Println(stringutils.PrettyString(string(buf)))
	//
	//	// update current room and queue
	//	b.updateQueue(ind, logger)
	//}
	//
	//wg.Add(1)
	//go func() {
	//	defer wg.Done()
	//
	//	timings, err := stringutils.TimingsToString(b.Conf.Timings)
	//	if err != nil {
	//		// todo: send me log messages in vk direct?
	//		fmt.Println(err)
	//
	//		// default timings if timings are not provided in conf file // todo: in .env?
	//		timings = "7:30;19:30"
	//	}
	//
	//	_, err = s.Every(b.Conf.Frequency).Day().At(timings).Do(task)
	//	if err != nil {
	//		logger.Println(err)
	//	}
	//
	//	s.StartBlocking()
	//}()

	// todo: clean day scheduler
	task2 := func() {

	}

	wg.Add(1)
	go func() {

		defer wg.Done()

		// todo: https://github.com/algorythma/go-scheduler/blob/14b57fd34811/scheduler.go#L40
		_, err := s.Every(1).Week().Weekday(time.Monday).At(b.Conf.CleanHour).Do(task2)
		if sendLogs {
			_, logErr := b.sendLog(ctx, "notify about clean: "+err.Error())
			if logErr != nil {
				logger.Println(logErr) // todo: log to file
			}
		}

	}()

	ff := func() {

		msg := "\nмбепнники: "

		ind := slices.IndexFunc(b.Conf.Rooms, func(r config.Room) bool { return r.Number == b.Conf.Current })
		room := b.Conf.Rooms[ind]

		msg += room.Number + "\n"
		for _, member := range room.Members {
			msg += "*" + member.Id + "(" + member.Name + ")\n"
		}

		fmt.Println(msg)

		// update current room and queue
		b.updateQueue(ind, logger)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		_, err := s.Every(3).Seconds().WaitForSchedule().Do(ff)
		if err != nil {
			logger.Println(err)
		}
		s.StartBlocking()

	}()

	logger.Println("Bot successfully started")
}
