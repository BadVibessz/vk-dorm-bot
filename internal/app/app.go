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
	"io"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"vk-bot/internal/config"
	stringutils "vk-bot/pkg/utils/string"
)

type App interface {
	NotifyAboutDuty(ctx context.Context, chatId int, room *config.Room) (*http.Response, error)
	NotifyAboutCleaning(ctx context.Context, chatId int) (*http.Response, error)

	SendMessageToChat(ctx context.Context, msg string, chatID, randID int) (*http.Response, error)
	SwapRooms(ctx context.Context, roomName1 string, roomName2 string) error

	StartAsync(ctx context.Context, logger *log.Logger, sendLogs bool)
	HandleMessage(ctx context.Context, obj events.MessageNewObject, logger *log.Logger)

	// todo: general function for starting any task with any schedule

}

type BotService struct {
	ConfigPath string
	Conf       *config.Config

	wg *sync.WaitGroup

	VK *vkapi.VkAPI

	dutyScheduler  *gocron.Scheduler
	cleanScheduler *gocron.Scheduler

	context context.Context // TODO: Contexts should not be stored inside a struct type, but instead passed to each function that needs it.

	swapped bool

	// count of notified duties, todo: rename
	count int
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
		wg:         &sync.WaitGroup{},
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

		if v.SwapPending {
			res += "*"
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

	case "/echo":
		_, err := b.sendMessage(ctx, msg, from, 0)
		if err != nil {
			logger.Println(err)
		}
		break

	// sort queue ascending
	case "/reset": // todo: store admin's ids in config and then check if message from admin
		b.resetQueue()

		_, err := b.sendMessage(ctx, "Порядок комнат восстановлен по умолчанию", from, 0)
		if err != nil {
			logger.Println(err)
		}

		// overwrite existing config
		saveErr := b.saveConfig()
		if saveErr != nil {
			logger.Println(saveErr)
		}

		break

	// set current room
	case "/setcurrent":

		// validate input
		curr := spltd[1]
		if slices.IndexFunc(b.Conf.Rooms, func(r config.Room) bool { return r.Number == curr }) != -1 {
			b.Conf.Current = curr

			resp := "Номер текущей комнаты успешно изменен" + "\n" + "Новый порядок:\n" + b.getQueueString()
			_, err := b.sendMessage(b.context, resp, from, 0)
			if err != nil {
				logger.Println(err)
			}

			// overwrite existing config
			saveErr := b.saveConfig()
			if saveErr != nil {
				logger.Println(saveErr)
			}

		} else {
			_, err := b.sendMessage(b.context, "Некорректный номер комнаты", from, 0)
			if err != nil {
				logger.Println(err)
			}
		}
		break

	case "/queue":
		_, err := b.sendMessage(b.context, b.getQueueString(), from, 0)
		if err != nil {
			logger.Println(err)
		}
		break

	case "/swap":
		err := b.SwapRooms(ctx, spltd[1], spltd[2])
		if err != nil {
			_, sendErr := b.sendMessage(b.context, "Некорректный номер комнаты", from, 0)
			if sendErr != nil {
				logger.Println(sendErr)
			}
		} else {
			resp := "Сделано!" + "\n" + "Новый порядок:\n" + b.getQueueString()
			_, err := b.sendMessage(b.context, resp, from, 0)
			if err != nil {
				logger.Println(err)
			}

			// overwrite existing config
			saveErr := b.saveConfig()
			if saveErr != nil {
				logger.Println(saveErr)
			}
		}
		break

	case "/setfreq":

		if n, err := strconv.Atoi(spltd[1]); err == nil && n <= 7 && n > 0 {

			b.Conf.Frequency = n

			err = b.saveConfig()
			if err != nil {
				logger.Println(err)
			}
		}

		break

	case "/setcleanday":

		day := strings.ToLower(spltd[1])

		_, err := stringutils.GetWeekday(day)
		if err != nil {
			logger.Println(err)
			_, err = b.sendMessage(b.context, "Такого дня недели не существует", from, 0)
			return
		}

		// cancel scheduled task
		err = b.cleanScheduler.RemoveByTag("cleanday")
		if err != nil {
			logger.Println(err)
			_, err = b.sendMessage(b.context, "Что-то пошло не так...", from, 0)
			return
		}

		// set clean day
		b.Conf.CleanDay = day

		// restart task
		b.scheduleCleanDayTask(b.context, logger, true)

		_, err = b.sendMessage(b.context, "День уборки успешно изменен на "+day, from, 0)
		if err != nil {
			logger.Println(err)
		}

		// overwrite existing config
		saveErr := b.saveConfig()
		if saveErr != nil {
			logger.Println(saveErr)
		}

		break

	case "/setcleanhour":

		cleanTime := spltd[1]

		err := stringutils.ValidateTime(cleanTime)
		if err != nil {
			logger.Println(err)
			_, err = b.sendMessage(b.context, "Указано некорректное время", from, 0)
			return
		}

		// cancel scheduled task
		err = b.cleanScheduler.RemoveByTag("cleanday")
		if err != nil {
			logger.Println(err)
			_, err = b.sendMessage(b.context, "Что-то пошло не так...", from, 0)
			return
		}

		// set clean day
		b.Conf.CleanHour = cleanTime

		// restart task
		b.scheduleCleanDayTask(ctx, logger, true)

		_, err = b.sendMessage(b.context, "Время уборки успешно изменено на "+cleanTime, from, 0)
		if err != nil {
			logger.Println(err)
		}

		// overwrite existing config
		saveErr := b.saveConfig()
		if saveErr != nil {
			logger.Println(saveErr)
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

	spltd := strings.Split(b.Conf.CleanHour, ":")

	hour, err := strconv.Atoi(spltd[0])
	if err != nil {
		return nil, err
	}

	mscHour := strconv.Itoa(hour + 3) // todo:
	msg := "@all\n" + "Напоминаю, сегодня проверка в " + mscHour + ":" + spltd[1]

	// todo: why from HandleMessage im getting 'value ctx from vksdk?'
	resp, err := b.SendMessageToChat(b.context, msg, chatId, 0)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (b *BotService) NotifyAboutDuty(ctx context.Context, chatId int, room *config.Room) (*http.Response, error) {

	b.count++
	b.count %= len(b.Conf.Timings) // take mod of count to know if it's time to move to next room

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

	b.swapped = true

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

	for _, v := range b.Conf.Rooms {
		v.SwapPending = false
	}

	slices.SortFunc(b.Conf.Rooms, func(r1, r2 config.Room) int {
		return cmp.Compare(r1.Number, r2.Number)
	})
}

func (b *BotService) updateQueue(currentInd int) {

	currentRoom := &b.Conf.Rooms[currentInd]

	// swap not pending anymore
	if currentRoom.SwapPending {
		currentRoom.SwapPending = false
	}

	// if sawpped and no swaps pending anymore -> reset queue
	if b.swapped && !b.isSwapPending() {
		b.resetQueue()
	}

	// update current room if count == 0
	if b.count == 0 {
		nextInd := (currentInd + 1) % len(b.Conf.Rooms)
		b.Conf.Current = b.Conf.Rooms[nextInd].Number
	}

	// todo: distinct func
	println("\ncurrent schedule:")
	for _, v := range b.Conf.Rooms {
		if v.Number == b.Conf.Current {
			print("*")
		}
		print(v.Number + " ")
	}
}

// todo: maybe store context in bot struct? what context to pass here?
func (b *BotService) scheduleCleanDayTask(ctx context.Context, logger *log.Logger, sendLogs bool) {

	cleanTask := func() {
		resp, err := b.NotifyAboutCleaning(ctx, 1)
		if err != nil {
			logger.Println(err)

			if sendLogs {
				_, logErr := b.sendLog(ctx, "notify about clean: "+err.Error())
				if logErr != nil {
					logger.Println(logErr)
				}
			}
		}

		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			logger.Println(err)
		}
		fmt.Println(stringutils.PrettyString(string(buf)))
	}

	b.cleanScheduler = gocron.NewScheduler(time.UTC)

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()

		weekday, err := stringutils.GetWeekday(b.Conf.CleanDay)
		if err == nil {

			cleanHour, parseErr := stringutils.MoscowTimeToGMT(b.Conf.CleanHour)
			if parseErr != nil {
				logger.Println(parseErr)

				if sendLogs {
					_, sendErr := b.sendLog(ctx, "notify about clean: "+parseErr.Error())
					if sendErr != nil {
						logger.Println(sendErr)
					}
				}
			}

			_, doErr := b.cleanScheduler.Every(1).
				Week().
				Weekday(weekday).
				At(cleanHour).
				Tag("cleanday").
				Do(cleanTask)

			if doErr != nil {
				logger.Println(doErr)

				if sendLogs {
					_, sendErr := b.sendLog(ctx, "notify about clean: "+doErr.Error())
					if sendErr != nil {
						logger.Println(sendErr)
					}
				}
			} else {
				b.cleanScheduler.StartBlocking()
			}
		} else {
			logger.Println(err)
		}

	}()

}

func (b *BotService) scheduleDutyTask(ctx context.Context, logger *log.Logger, sendLogs bool) {
	dutyTask := func() {

		ind := slices.IndexFunc(b.Conf.Rooms, func(r config.Room) bool { return r.Number == b.Conf.Current })
		room := b.Conf.Rooms[ind]

		// TODO: dynamically retrieve chat id by name
		resp, err := b.NotifyAboutDuty(ctx, 1, &room)
		if err != nil {
			logger.Println(err)

			if sendLogs {
				_, logErr := b.sendLog(ctx, "notify about duty: "+err.Error())
				if logErr != nil {
					logger.Println(logErr) // todo: log to file
				}
			}
		}

		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			logger.Println(err)
		}
		fmt.Println(stringutils.PrettyString(string(buf)))

		// update current room and queue
		b.updateQueue(ind)

		// overwrite existing config
		saveErr := b.saveConfig()
		if saveErr != nil {
			logger.Println(saveErr)
		}
	}

	b.dutyScheduler = gocron.NewScheduler(time.UTC)

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()

		timings, err := stringutils.TimingsToGMTString(b.Conf.Timings)
		if err != nil {
			// todo: send me log messages in vk direct?
			logger.Println(err)

			// default timings if timings are not provided in conf file // todo: in .env? (12factor app)
			timings = "10:30;22:30"
		}

		_, err = b.dutyScheduler.Every(b.Conf.Frequency).Day().At(timings).Do(dutyTask)
		if err != nil {
			logger.Println(err)
		}

		b.dutyScheduler.StartBlocking()
	}()
}

// todo: call generic method StartTaskAsync(task func(), ctx ...)
func (b *BotService) StartAsync(ctx context.Context, logger *log.Logger, sendLogs bool) {

	b.context = ctx // todo: pass context to bot's 'constructor'?

	_, err := b.sendMessage(ctx, "Я включился!)", b.Conf.Dad, 0)
	if err != nil {
		logger.Println(err)
	}

	b.scheduleDutyTask(ctx, logger, sendLogs)
	b.scheduleCleanDayTask(ctx, logger, sendLogs)

	//ff := func() {
	//
	//	msg := "\nмбепнники: "
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
	//	// update current room and queue
	//	b.updateQueue(ind, logger)
	//}
	//
	//wg.Add(1)
	//go func() {
	//	defer wg.Done()
	//
	//	_, err := dutyScheduler.Every(3).Seconds().WaitForSchedule().Do(ff)
	//	if err != nil {
	//		logger.Println(err)
	//	}
	//	dutyScheduler.StartBlocking()
	//
	//}()

	logger.Println("Bot successfully started")
}
