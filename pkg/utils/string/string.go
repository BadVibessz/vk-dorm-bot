package string

import (
	"bytes"
	"encoding/json"
	"errors"
)

func PrettyString(str string) (string, error) {
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, []byte(str), "", "    "); err != nil {
		return "", err
	}
	return prettyJSON.String(), nil
}

func TimingsToString(timings []string) (string, error) {

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
