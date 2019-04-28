package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"go-bot/events"
	"go-bot/gateway"
	"go-bot/voice"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
)

func main() {
	token, err := readToken()
	if err != nil {
		log.Fatal(err)
	}

	wsClient, err := gateway.NewClient(token)
	if err != nil {
		log.Fatal(err)
	}

	voiceClient := voice.NewClient(wsClient)

	wsClient.EventHandlers[events.MessageCreate] = func(data json.RawMessage) {
		var message events.MessageEvent
		err := json.Unmarshal(data, &message)

		if err != nil {
			log.Println("error parsing json", err)
		}

		if message.Content != "!s" {
			return
		}

		url := "https://discordapp.com/api/v6/channels/" + message.ChannelID + "/messages"

		go voiceClient.EstablishConnection("voice channel id")

		var jsonStr = []byte(`{"content":"Establishing voice", "tts": false}`)
		req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bot "+token)

		if err != nil {
			log.Println(err)
		}

		client := &http.Client{}
		_, err = client.Do(req)

		if err != nil {
			log.Println(err)
		}

	}

	go wsClient.Start()

	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

func readToken() (string, error) {
	b, err := ioutil.ReadFile("go-bot-token.txt")
	if err != nil {
		return "", fmt.Errorf("error reading token file: %v", err)
	}
	return strings.TrimRight(string(b), "\n"), nil
}
