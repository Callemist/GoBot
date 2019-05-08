package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
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

	gw, err := newGateway(token)
	if err != nil {
		log.Fatal(err)
	}

	voi := newVoice()

	gw.eventHandlers[messageCreateEvent] = func(data json.RawMessage) {
		var m message
		err := json.Unmarshal(data, &m)

		if err != nil {
			log.Println("error parsing json", err)
		}

		if m.Content != "!s" {
			return
		}

		url := "https://discordapp.com/api/v6/channels/" + m.ChannelID + "/messages"

		err = voi.establishConnection("voice channel id", gw)
		if err != nil {
			log.Printf("error establishing voice connection: %v\n", err)
		}

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

	go gw.open()

	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

func readToken() (string, error) {
	b, err := ioutil.ReadFile("go-bot-token.txt")
	if err != nil {
		return "", fmt.Errorf("error reading token file: %v", err)
	}
	return strings.TrimRight(string(b), "\n"), nil
}
