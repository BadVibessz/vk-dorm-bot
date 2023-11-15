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
	"os"
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

	// general function for starting any task with any schedule?
}

type BotService struct {
	ConfigPath string
	Conf       *config.Config

	wg *sync.WaitGroup

	VK *vkapi.VkAPI

	dutyScheduler  *gocron.Scheduler
	cleanScheduler *gocron.Scheduler

	sendLogs bool
	swapped  bool
	accepted bool
	skipped  bool

	// count int
	skippedCount int
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

func (b *BotService) log(ctx context.Context, msg string, logger *log.Logger) {
	logger.Println(msg)

	if b.sendLogs {
		_, logErr := b.sendMessage(ctx, msg, b.Conf.Dad, 0)
		if logErr != nil {
			logger.Println(logErr)
		}
	}
}

func (b *BotService) HandleMessage(ctx context.Context, obj events.MessageNewObject, logger *log.Logger) {

	msg := obj.Message.Text
	from := obj.Message.FromID

	// authenticate
	if slices.IndexFunc(append(b.Conf.Admins, b.Conf.Dad), func(n int) bool { return n == from }) == -1 {
		_, err := b.sendMessage(ctx, "У тебя нет прав, чтобы запрашивать данную команду.", from, 0)
		if err != nil {
			logger.Println(err)
		}
		return
	}

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
	case "/reset":
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
			_, err := b.sendMessage(ctx, resp, from, 0)
			if err != nil {
				logger.Println(err)
			}

			// overwrite existing config
			saveErr := b.saveConfig()
			if saveErr != nil {
				logger.Println(saveErr)
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
			_, sendErr := b.sendMessage(ctx, "Некорректный номер комнаты", from, 0)
			if sendErr != nil {
				logger.Println(sendErr)
			}
		} else {
			resp := "Сделано!" + "\n" + "Новый порядок:\n" + b.getQueueString()
			_, err := b.sendMessage(ctx, resp, from, 0)
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

	case "/getcleanday":

		_, err := b.sendMessage(ctx, "День уборки: "+b.Conf.CleanDay, from, 0)
		if err != nil {
			logger.Println(err)
		}

		break

	case "/setcleanday":

		day := strings.ToLower(spltd[1])

		_, err := stringutils.GetWeekday(day)
		if err != nil {
			logger.Println(err)
			_, err = b.sendMessage(ctx, "Такого дня недели не существует", from, 0)
			return
		}

		// cancel scheduled task
		err = b.cleanScheduler.RemoveByTag("cleanday")
		if err != nil {
			logger.Println(err)
			_, err = b.sendMessage(ctx, "Что-то пошло не так...", from, 0)
			return
		}

		// set clean day
		b.Conf.CleanDay = day

		// restart task
		b.scheduleCleanDayTask(ctx, logger)

		_, err = b.sendMessage(ctx, "День уборки успешно изменен на "+day, from, 0)
		if err != nil {
			logger.Println(err)
		}

		// overwrite existing config
		saveErr := b.saveConfig()
		if saveErr != nil {
			logger.Println(saveErr)
		}

		break

	case "/getcleanhour":

		_, err := b.sendMessage(ctx, "Время уборки: "+b.Conf.CleanHour, from, 0)
		if err != nil {
			logger.Println(err)
		}

		break

	case "/setcleanhour":

		cleanTime := spltd[1]

		err := stringutils.ValidateTime(cleanTime)
		if err != nil {
			logger.Println(err)
			_, err = b.sendMessage(ctx, "Указано некорректное время", from, 0)
			return
		}

		// cancel scheduled task
		err = b.cleanScheduler.RemoveByTag("cleanday")
		if err != nil {
			logger.Println(err)
			_, err = b.sendMessage(ctx, "Что-то пошло не так...", from, 0)
			return
		}

		// set clean day
		b.Conf.CleanHour = cleanTime

		// restart task
		b.scheduleCleanDayTask(ctx, logger)

		_, err = b.sendMessage(ctx, "Время уборки успешно изменено на "+cleanTime, from, 0)
		if err != nil {
			logger.Println(err)
		}

		// overwrite existing config
		saveErr := b.saveConfig()
		if saveErr != nil {
			logger.Println(saveErr)
		}

		break

	case "/accepted":

		// if accepted -> current = current.next
		b.accepted = true

		// send response to user
		_, err := b.sendMessage(ctx, "Понял!\nСледующий раз напомню "+b.Conf.Rooms[b.getCurrentRoomIndex()+1].Number+" комнате.", from, 0)
		if err != nil {
			logger.Println(err)
		}

		break

	case "/skip":

		// todo:

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

	err := stringutils.ValidateTime(b.Conf.CleanHour)
	if err != nil {
		return nil, err
	}

	msg := "@all\n" + "Напоминаю, сегодня проверка в " + b.Conf.CleanHour
	resp, err := b.SendMessageToChat(ctx, msg, chatId, 0)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (b *BotService) NotifyAboutDuty(ctx context.Context, chatId int, room *config.Room) (*http.Response, error) {

	// b.count++
	// b.count %= len(b.Conf.Timings) // take mod of count to know if it's time to move to next room

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

	m := sync.Mutex{} // todo: where to define mutex? is mutex necessarily here?

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

func (b *BotService) getCurrentRoomIndex() int {
	return slices.IndexFunc(b.Conf.Rooms, func(r config.Room) bool { return r.Number == b.Conf.Current })
}

func (b *BotService) updateQueue() error {

	currentInd := b.getCurrentRoomIndex()
	currentRoom := &b.Conf.Rooms[currentInd]

	// swap not pending anymore
	if currentRoom.SwapPending {
		currentRoom.SwapPending = false
	}

	// if sawpped and no swaps pending anymore -> reset queue
	if b.swapped && !b.isSwapPending() {
		b.resetQueue()
	}

	nowHour, nowMin := time.Now().Hour(), time.Now().Minute()
	spltd := strings.Split(b.Conf.Timings[len(b.Conf.Timings)-1], ":")

	lastHour, err := strconv.Atoi(spltd[0])
	if err != nil {
		return err
	}

	lastMin, err := strconv.Atoi(spltd[1])
	if err != nil {
		return err
	}

	// move to the next room // todo: test
	if (nowHour > lastHour) || (nowHour == lastHour && nowMin >= lastMin) {
		nextInd := (currentInd + 1) % len(b.Conf.Rooms)
		b.Conf.Current = b.Conf.Rooms[nextInd].Number

		// reset accepted flag
		b.accepted = false

		// recalc skippedCount
		b.skippedCount--

		if b.skippedCount == 0 {
			b.skipped = false
		}

	}

	b.printDutySchedule()
	return nil
}

func (b *BotService) printDutySchedule() {
	println("\ncurrent schedule:")
	for _, v := range b.Conf.Rooms {
		if v.Number == b.Conf.Current {
			print("*")
		}
		print(v.Number + " ")
	}
}

func (b *BotService) scheduleCleanDayTask(ctx context.Context, logger *log.Logger) {

	cleanTask := func() {
		resp, err := b.NotifyAboutCleaning(ctx, 1)
		if err != nil {
			b.log(ctx, "schedule clean task: "+err.Error(), logger)
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
				b.log(ctx, "schedule clean task: "+parseErr.Error(), logger)
			}

			// todo: обработать случаи ведущих нулей (только зачем?, для красоты!)
			spltd := strings.Split(cleanHour, ":")

			h, strvErr := strconv.Atoi(spltd[0])
			if strvErr != nil {
				logger.Println(strvErr)
			}

			notifyTime := strconv.Itoa(h-3) + ":" + spltd[1]

			_, doErr := b.cleanScheduler.Every(1).
				Week().
				Weekday(weekday).
				At(notifyTime).
				Tag("cleanday").
				Do(cleanTask)

			if doErr != nil {
				b.log(ctx, "schedule clean task: "+doErr.Error(), logger)
			} else {
				b.cleanScheduler.StartAsync()

				// listen for ctx cancellation
				for {
					select {
					case <-ctx.Done():
						logger.Println("\nshuttding down clean day scheduler")
						return
					}
				}
			}
		} else {
			b.log(ctx, "schedule clean task: "+err.Error(), logger)
		}

	}()

}

func (b *BotService) scheduleDutyTask(ctx context.Context, logger *log.Logger) {
	dutyTask := func() {

		if !b.accepted && !b.skipped {
			ind := slices.IndexFunc(b.Conf.Rooms, func(r config.Room) bool { return r.Number == b.Conf.Current })
			room := b.Conf.Rooms[ind]

			// TODO: dynamically retrieve chat id by name
			resp, err := b.NotifyAboutDuty(ctx, 1, &room)
			if err != nil {
				b.log(ctx, "schedule duty task (notifyAboutDuty): "+err.Error(), logger)
			}

			buf, err := io.ReadAll(resp.Body)
			if err != nil {
				logger.Println(err)
			}
			fmt.Println(stringutils.PrettyString(string(buf)))
		}

		// update current room and queue
		updateErr := b.updateQueue()
		if updateErr != nil {
			b.log(ctx, "schedule duty task (updateQueue): "+updateErr.Error(), logger)
		}

		// overwrite existing config
		saveErr := b.saveConfig()
		if saveErr != nil {
			b.log(ctx, "schedule duty task (saveConfig): "+saveErr.Error(), logger)
		}
	}

	b.dutyScheduler = gocron.NewScheduler(time.UTC)

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()

		timings, err := stringutils.TimingsToGMTString(b.Conf.Timings)
		if err != nil {
			b.log(ctx, "schedule duty task (TimingsToGMTString): "+err.Error(), logger)

			// default timings if timings are not provided in conf file // todo: in .env? (12factor app)
			timings = "10:30;21:00"
		}

		_, err = b.dutyScheduler.Every(b.Conf.Frequency).Day().At(timings).Tag("duty").Do(dutyTask)
		if err != nil {
			b.log(ctx, "schedule duty task (Do): "+err.Error(), logger)
		}

		b.dutyScheduler.StartAsync()

		// listen for ctx cancellation
		for {
			select {
			case <-ctx.Done():
				logger.Println("shuttding down duty scheduler")
				return
			}
		}

	}()
}

func (b *BotService) StartAsync(ctx context.Context, logger *log.Logger, sendLogs bool) {

	b.sendLogs = sendLogs

	_, err := b.sendMessage(ctx, "Я включился!)", b.Conf.Dad, 0)
	if err != nil {
		logger.Println(err)
	}

	b.scheduleDutyTask(ctx, logger)
	b.scheduleCleanDayTask(ctx, logger)

	logger.Println("Bot successfully started")

	for {
		select {
		case <-ctx.Done():
			logger.Println("shuttding down schedulers")

			// there's no reason for graceful shutdown because it's meaningless to wait for all the jobs to stop.

			b.dutyScheduler.Stop()
			removeErr := b.dutyScheduler.RemoveByTag("duty")
			if removeErr != nil {
				return
			}

			b.cleanScheduler.Stop()
			removeErr = b.cleanScheduler.RemoveByTag("cleanday")
			if removeErr != nil {
				return // todo:
			}

			// todo: remove and understand why not all goroutines returns by ctx.cancel capturing
			time.Sleep(1 * time.Second) // wait for server.shutdown
			os.Exit(1)
			return
		}
	}
}
