package string

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

func PrettyString(str string) (string, error) {
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, []byte(str), "", "    "); err != nil {
		return "", err
	}
	return prettyJSON.String(), nil
}

func TimingsToGMTString(timings []string) (string, error) {

	if len(timings) == 0 {
		return "", errors.New("timings can not be empty")
	}

	res := ""
	for i, v := range timings {

		gmt, err := MoscowTimeToGMT(v)
		if err != nil {
			return "", err
		}

		res += gmt
		if i != len(timings)-1 {
			res += ";"
		}
	}
	return res, nil
}

func GetWeekday(weekday string) (time.Weekday, error) {

	switch strings.ToLower(weekday) {

	case "monday", "понедельник":
		return time.Monday, nil
	case "tuesday", "вторник":
		return time.Tuesday, nil
	case "wednesday", "среда":
		return time.Wednesday, nil
	case "thursday", "четверг":
		return time.Thursday, nil
	case "friday", "пятница":
		return time.Friday, nil
	case "saturday", "суббота":
		return time.Saturday, nil
	case "sunday", "воскресенье":
		return time.Sunday, nil
	}

	return -1, errors.New("GetWeekday: no such weekday")
}

func ValidateTime(time string) error {

	spltd := strings.Split(time, ":")

	hour, err := strconv.Atoi(spltd[0])
	if err != nil {
		return err
	}

	minute, err := strconv.Atoi(spltd[0])
	if err != nil {
		return err
	}

	if hour < 0 || hour > 23 {
		return errors.New("ValidateTime: incorrect hour format")
	}

	if minute < 0 || minute > 60 {
		return errors.New("ValidateTime: incorrect time format")
	}

	return nil
}

func MoscowTimeToGMT(time string) (string, error) {

	spltd := strings.Split(time, ":")

	var hour int
	var err error

	if spltd[0][0] == '0' && spltd[0][1] < '3' {

		diff, atoiErr := strconv.Atoi(string(spltd[0][1]))
		if err != nil {
			return "", atoiErr
		}

		hour = 24 - diff
	} else {
		hour, err = strconv.Atoi(spltd[0])
		if err != nil {
			return "", err
		}
	}

	var GMT string
	if hour < 10 {
		GMT = "0" + strconv.Itoa(hour-3)
	} else {
		GMT = strconv.Itoa(hour - 3)
	}

	return GMT + ":" + spltd[1], nil
}
