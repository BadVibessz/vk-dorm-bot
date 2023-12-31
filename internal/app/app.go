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
	"log/slog"
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
	NotifyAboutDuty(ctx context.Context, room *config.Room) (*http.Response, error)
	NotifyAboutCleaning(ctx context.Context) (*http.Response, error)

	SendMessageToChat(ctx context.Context, msg string, randID int) (*http.Response, error)

	StartAsync(ctx context.Context, sendLogs bool, servChan chan any)
	HandleMessage(ctx context.Context, obj events.MessageNewObject)

	// general function for starting any task with any schedule?
}

const (
	groupPeerId = 2000000000
)

type BotService struct {
	ConfigPath string

	confMutex sync.RWMutex
	Conf      *config.Config

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

func (b *BotService) SendMessageToChat(ctx context.Context, msg string, randID int) (*http.Response, error) {
	return b.sendMessage(ctx, msg, groupPeerId+b.Conf.ChatID, randID)
}

func (b *BotService) getQueueString() string {

	b.confMutex.RLock()
	defer b.confMutex.RUnlock()

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

func (b *BotService) log(ctx context.Context, msg string, level int) {

	if level == 1 {
		slog.Info(msg)
	} else if level == 2 {
		slog.Error(msg)
	}

	if b.sendLogs {
		_, logErr := b.sendMessage(ctx, msg, b.Conf.Dad, 0)
		if logErr != nil {
			slog.Error(logErr.Error())
		}
	}
}

func (b *BotService) getUserGroup(id int) string {
	if slices.IndexFunc(b.Conf.Admins, func(n int) bool { return n == id }) != -1 {
		return "admin"
	}
	return "unknown"
}

func (b *BotService) HandleMessage(ctx context.Context, obj events.MessageNewObject) {

	msg := obj.Message.Text
	from := obj.Message.FromID

	isSaveNeeded := false

	// if message from chat -> do not handle
	if obj.Message.PeerID > groupPeerId {
		return
	}

	logMsg := "new message from @id" + strconv.Itoa(from) + " with content: " + msg + "."
	slog.Info(logMsg)

	if from != b.Conf.Dad {
		_, err := b.sendMessage(ctx, logMsg, b.Conf.Dad, 0)
		if err != nil {
			slog.Error(err.Error())
		}
	}

	group := b.getUserGroup(from)

	log.Println(msg)

	spltd := strings.Split(msg, " ")

	authenticate := func() bool {
		if group != "admin" {
			_, err := b.sendMessage(ctx, "У тебя нет прав, чтобы запрашивать данную команду.", from, 0)
			if err != nil {
				slog.Error(err.Error())
			}
			return false
		}
		return true
	}

	switch spltd[0] {

	case "/reset":
		if !authenticate() {
			return
		}

		b.resetQueue()
		_, err := b.sendMessage(ctx, "Порядок комнат восстановлен по умолчанию", from, 0)
		if err != nil {
			slog.Error(err.Error())
		}

		isSaveNeeded = true

		break

	// set current room
	case "/setcurrent":
		if !authenticate() {
			return
		}

		// validate input
		curr := spltd[1]
		if slices.IndexFunc(b.Conf.Rooms, func(r config.Room) bool { return r.Number == curr }) != -1 {

			b.confMutex.Lock()

			b.Conf.Current = curr
			resp := "Номер текущей комнаты успешно изменен" + "\n" + "Новый порядок:\n" + b.getQueueString()

			b.confMutex.Unlock()

			_, err := b.sendMessage(ctx, resp, from, 0)
			if err != nil {
				slog.Error(err.Error())
			}

			isSaveNeeded = true

		} else {
			_, err := b.sendMessage(ctx, "Некорректный номер комнаты", from, 0)
			if err != nil {
				slog.Error(err.Error())
			}
		}
		break

	case "/queue":
		// getQueueString() locks and unlocks mutex by itself
		_, err := b.sendMessage(ctx, b.getQueueString(), from, 0)
		if err != nil {
			slog.Error(err.Error())
		}
		break

	case "/swap":
		if !authenticate() {
			return
		}

		err := b.swapRooms(spltd[1], spltd[2])
		if err != nil {
			_, sendErr := b.sendMessage(ctx, "Некорректный номер комнаты", from, 0)
			if sendErr != nil {
				slog.Error(sendErr.Error())
			}
		} else {
			resp := "Сделано!" + "\n" + "Новый порядок:\n" + b.getQueueString()
			_, sendErr := b.sendMessage(ctx, resp, from, 0)
			if err != nil {
				slog.Error(sendErr.Error())
			}

			isSaveNeeded = true
		}
		break

	case "/setfreq":
		if !authenticate() {
			return
		}

		if n, err := strconv.Atoi(spltd[1]); err == nil && n <= 7 && n > 0 {

			b.confMutex.Lock()
			b.Conf.Frequency = n
			b.confMutex.Unlock()

			isSaveNeeded = true
		}

		break

	case "/getcleanday":

		b.confMutex.RLock()
		cleanDay := b.Conf.CleanDay
		b.confMutex.RUnlock()

		_, err := b.sendMessage(ctx, "День уборки: "+cleanDay, from, 0)
		if err != nil {
			slog.Error(err.Error())
		}

		break

	case "/setcleanday":
		if !authenticate() {
			return
		}

		day := strings.ToLower(spltd[1])

		_, err := stringutils.GetWeekday(day)
		if err != nil {
			slog.Error(err.Error())
			_, err = b.sendMessage(ctx, "Такого дня недели не существует", from, 0)
			if err != nil {
				slog.Error(err.Error())
			}
			return
		}

		// cancel scheduled task
		err = b.cleanScheduler.RemoveByTag("cleanday")
		if err != nil {
			slog.Error(err.Error())
			_, err = b.sendMessage(ctx, "Что-то пошло не так...", from, 0)
			if err != nil {
				slog.Error(err.Error())
			}
			return
		}

		// set clean day
		b.confMutex.Lock()
		b.Conf.CleanDay = day
		b.confMutex.Unlock()

		// restart task
		b.scheduleCleanDayTask(ctx)

		_, err = b.sendMessage(ctx, "День уборки успешно изменен на "+day, from, 0)
		if err != nil {
			slog.Error(err.Error())
		}

		isSaveNeeded = true

		break

	case "/getcleanhour":

		b.confMutex.RLock()
		cleanHour := b.Conf.CleanHour
		b.confMutex.RUnlock()

		_, err := b.sendMessage(ctx, "Время уборки: "+cleanHour, from, 0)
		if err != nil {
			slog.Error(err.Error())
		}

		break

	case "/setcleanhour":
		if !authenticate() {
			return
		}

		cleanTime := spltd[1]

		err := stringutils.ValidateTime(cleanTime)
		if err != nil {
			slog.Error(err.Error())
			_, err = b.sendMessage(ctx, "Указано некорректное время", from, 0)
			if err != nil {
				slog.Error(err.Error())
			}
		}

		// cancel scheduled task
		err = b.cleanScheduler.RemoveByTag("cleanday")
		if err != nil {
			slog.Error(err.Error())
			_, err = b.sendMessage(ctx, "Что-то пошло не так...", from, 0)
			if err != nil {
				slog.Error(err.Error())
			}
		}

		// set clean day
		b.confMutex.Lock()
		b.Conf.CleanHour = cleanTime
		b.confMutex.Unlock()

		// restart task
		b.scheduleCleanDayTask(ctx)

		_, err = b.sendMessage(ctx, "Время уборки успешно изменено на "+cleanTime, from, 0)
		if err != nil {
			slog.Error(err.Error())
		}

		isSaveNeeded = true
		break

	case "/accepted":
		if !authenticate() {
			return
		}

		b.confMutex.Lock()

		// if accepted -> current = current.next
		b.accepted = true
		roomNumber := b.Conf.Rooms[b.getCurrentRoomIndex()+1].Number

		b.confMutex.Unlock()

		// send response to user
		_, err := b.sendMessage(ctx, "Понял!\nСледующий раз напомню "+roomNumber+" комнате.", from, 0)
		if err != nil {
			slog.Error(err.Error())
		}

		break

	case "/skip":
		if !authenticate() {
			return
		}

		count, err := strconv.Atoi(spltd[0])
		if err != nil {
			_, sendErr := b.sendMessage(ctx, "Некорректный параметр", from, 0)
			if sendErr != nil {
				slog.Error(sendErr.Error())
			}
		}

		b.confMutex.Lock()

		b.skipped = true
		b.skippedCount = count

		b.confMutex.Unlock()

		_, err = b.sendMessage(ctx, "Понял!\nПропускаю напоминания "+strconv.Itoa(count)+" раз.", from, 0)
		if err != nil {
			slog.Error(err.Error())
		}

		break

	default: // echo
		_, err := b.sendMessage(ctx, msg, from, 0)
		if err != nil {
			slog.Error(err.Error())
		}
	}

	// overwrite existing config
	if isSaveNeeded {
		saveErr := b.saveConfig()
		if saveErr != nil {
			slog.Error(saveErr.Error())
		}
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

func (b *BotService) NotifyAboutCleaning(ctx context.Context) (*http.Response, error) {

	err := stringutils.ValidateTime(b.Conf.CleanHour)
	if err != nil {
		return nil, err
	}

	msg := "@all\n" + "Напоминаю, сегодня проверка в " + b.Conf.CleanHour
	resp, err := b.SendMessageToChat(ctx, msg, 0)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (b *BotService) NotifyAboutDuty(ctx context.Context, room *config.Room) (*http.Response, error) {

	msg := "Дежурные: " + room.Number + "\n"
	for _, member := range room.Members {
		msg += "*" + member.Id + "(" + member.Name + ")\n"
	}

	resp, err := b.SendMessageToChat(ctx, msg, 0)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (b *BotService) StartScheduledTaskAsync(task func()) { // todo: code generation?
}

func (b *BotService) swapRooms(roomName1 string, roomName2 string) error {

	b.confMutex.Lock()
	defer b.confMutex.Unlock()

	b.swapped = true

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

	b.confMutex.Lock()
	defer b.confMutex.Unlock()

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
	spltd := strings.Split(b.Conf.DutyTimings[len(b.Conf.DutyTimings)-1], ":")

	lastHour, err := strconv.Atoi(spltd[0])
	if err != nil {
		return err
	}

	lastMin, err := strconv.Atoi(spltd[1])
	if err != nil {
		return err
	}

	// move to the next room
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

func (b *BotService) scheduleCleanDayTask(ctx context.Context) {

	cleanTask := func() {
		resp, err := b.NotifyAboutCleaning(ctx)
		if err != nil {
			b.log(ctx, "schedule clean task: "+err.Error(), 2)
		}

		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error(err.Error())
		}
		fmt.Println(stringutils.PrettyString(string(buf)))
	}

	b.cleanScheduler = gocron.NewScheduler(time.UTC)

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()

		weekday, err := stringutils.GetWeekday(b.Conf.CleanDay)
		if err == nil {

			timings, parseErr := stringutils.TimingsToGMTString(b.Conf.CleanTimings)
			if parseErr != nil {
				b.log(ctx, "schedule duty task (TimingsToGMTString): "+parseErr.Error(), 2)

				// default timings if timings are not provided in conf file
				timings = "11:00;18:00"
			}

			_, doErr := b.cleanScheduler.Every(1).
				Week().
				Weekday(weekday).
				At(timings).
				Tag("cleanday").
				Do(cleanTask)

			if doErr != nil {
				b.log(ctx, "schedule clean task: "+doErr.Error(), 2)
			} else {
				b.cleanScheduler.StartAsync()

				// listen for ctx cancellation
				for {
					select {
					case <-ctx.Done():
						slog.Info("shutting down clean day scheduler")
						return
					}
				}
			}
		} else {
			b.log(ctx, "schedule clean task: "+err.Error(), 2)
		}

	}()

}

func (b *BotService) scheduleDutyTask(ctx context.Context) {
	dutyTask := func() {
		if !b.accepted && !b.skipped {
			ind := slices.IndexFunc(b.Conf.Rooms, func(r config.Room) bool { return r.Number == b.Conf.Current })
			room := b.Conf.Rooms[ind]

			// TODO: dynamically retrieve chat id by name
			resp, err := b.NotifyAboutDuty(ctx, &room)
			if err != nil {
				b.log(ctx, "schedule duty task (notifyAboutDuty): "+err.Error(), 2)
			}

			buf, err := io.ReadAll(resp.Body)
			if err != nil {
				slog.Error(err.Error())
			}
			fmt.Println(stringutils.PrettyString(string(buf)))
		}

		// update current room and queue
		updateErr := b.updateQueue()
		if updateErr != nil {
			b.log(ctx, "schedule duty task (updateQueue): "+updateErr.Error(), 2)
		}

		// overwrite existing config
		saveErr := b.saveConfig()
		if saveErr != nil {
			b.log(ctx, "schedule duty task (saveConfig): "+saveErr.Error(), 2)
		}
	}

	b.dutyScheduler = gocron.NewScheduler(time.UTC)

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()

		timings, err := stringutils.TimingsToGMTString(b.Conf.DutyTimings)
		if err != nil {
			b.log(ctx, "schedule duty task (TimingsToGMTString): "+err.Error(), 2)

			// default timings if timings are not provided in conf file
			timings = "10:30;21:00"
		}

		_, err = b.dutyScheduler.Every(b.Conf.Frequency).Day().At(timings).Tag("duty").Do(dutyTask)
		if err != nil {
			b.log(ctx, "schedule duty task (Do): "+err.Error(), 2)
		}

		b.dutyScheduler.StartAsync()

		// listen for ctx cancellation
		for {
			select {
			case <-ctx.Done():
				slog.Info("shutting down duty scheduler")
				return
			}
		}

	}()
}

func (b *BotService) StartAsync(ctx context.Context, sendLogs bool, servChan chan any) {

	b.sendLogs = sendLogs

	_, err := b.sendMessage(ctx, "Я включился!)", b.Conf.Dad, 0)
	if err != nil {
		slog.Error(err.Error())
	}

	if validErr := b.Conf.Validate(); validErr != nil {
		b.log(ctx, validErr.Error(), 2)
	}

	b.scheduleDutyTask(ctx)
	b.scheduleCleanDayTask(ctx)

	slog.Info("Bot successfully started")

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down schedulers")

			// there's no reason for graceful shutdown because it's meaningless to wait for all the jobs to stop.
			b.dutyScheduler.Stop()
			removeErr := b.dutyScheduler.RemoveByTag("duty")
			if removeErr != nil {
				slog.Error(removeErr.Error())
			}

			b.cleanScheduler.Stop()
			removeErr = b.cleanScheduler.RemoveByTag("cleanday")
			if removeErr != nil {
				slog.Error(removeErr.Error())
			}

			// wait for server.shutdown
			<-servChan
			os.Exit(1)
		}
	}
}
