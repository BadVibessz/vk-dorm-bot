package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/nikepan/govkbot/v2"
	"strconv"
)

// TODO: IMPORTS
func PrettyString(str string) (string, error) {
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, []byte(str), "", "    "); err != nil {
		return "", err
	}
	return prettyJSON.String(), nil
}

func main() {
	// create new http request

	//body := []byte(`{"user_id":"` + name + `","random_id":"` + 0 + `"}`)
	//
	//request, err := http.NewRequest("POST", apiUrl, bytes.NewBuffer(body))
	//request.Header.Set("Content-Type", "application/json; charset=utf-8")
	//
	//// send the request
	//client := &http.Client{}
	//response, err := client.Do(request)

	api := govkbot.VkAPI{
		Token: token,
		URL:   vkEndpoint,
		Ver:   vkApiVersion,
	}

	bytes, err := api.Call("messages.send", govkbot.H{
		//"user_id":   strconv.Itoa(myVkId),
		//"chat_id":   strconv.Itoa(2000000001),
		"message":   "ДА Я ЛЮБЛЮ СО..",
		"random_id": strconv.Itoa(0),
		"peer_id":   strconv.Itoa(2000000000 + 1),
	})
	if err != nil {
		fmt.Errorf(err.Error())
	}

	print(PrettyString(string(bytes)))

	bytes, err = api.Call("messages.getConversations", govkbot.H{})
	if err != nil {
		fmt.Errorf(err.Error())
	}
	print(PrettyString(string(bytes)))

	//info, err := api.GetConversation(334808440)
	//if err != nil {
	//	fmt.Errorf(err.Error())
	//}
	//
	//print(PrettyString(strconv.Itoa(info.ID)))
	//print("TITLE", info.Title)

}
