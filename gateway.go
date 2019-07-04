package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// gateway handles communcation with Discords websocket api
type gateway struct {
	token               string
	wsMux               sync.Mutex
	conn                *websocket.Conn
	sequence            *int64
	lastHeartbeatAck    time.Time
	eventHandlers       map[string]func(json.RawMessage)
	sessionInfo         ready
	voiceUpdateResponse chan payload
}

// newGateway returns a client to subscribe on Discord events
// sent via the gateway.
func newGateway(t string) (*gateway, error) {
	g := gateway{
		token:               t,
		sequence:            new(int64),
		eventHandlers:       make(map[string]func(json.RawMessage)),
		voiceUpdateResponse: make(chan payload)}

	u, err := getWsURL()
	if err != nil {
		return &gateway{}, fmt.Errorf("error getting gateway url %v", err)
	}

	c, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		return &gateway{}, fmt.Errorf("error creating gateway websocket connection: %v", err)
	}

	g.conn = c
	return &g, nil
}

// open initiate a new gateway connection and start listening for events.
func (g *gateway) open() {
	g.lastHeartbeatAck = time.Now().UTC()
	stopc := make(chan int)
	var interval int

	for {
		_, message, err := g.conn.ReadMessage()
		if err != nil {
			log.Printf("error reading gateway message: %v\n", err)
		}

		var pretty bytes.Buffer
		json.Indent(&pretty, message, "", "    ")
		log.Printf("received:\n%s\n", string(pretty.Bytes()))

		var p payload
		err = json.Unmarshal(message, &p)
		if err != nil {
			log.Printf("error unmarshalling payload: %v\n", err)
		}

		// Hello event
		if p.Operation == 10 {
			var he gatewayHello
			err := json.Unmarshal(p.EventData, &he)
			if err != nil {
				log.Printf("error unmarshalling gatewayhello: %v\n", err)
			}

			g.identify()

			interval = he.HeartbeatInterval
			go g.startHeart(he.HeartbeatInterval, stopc)
		}

		// Heartbeat ACK event
		if p.Operation == 11 {
			g.lastHeartbeatAck = time.Now().UTC()
			log.Println("received gateway ACK")
		}

		if time.Now().UTC().Sub(g.lastHeartbeatAck) > time.Millisecond*time.Duration(interval) {
			stopc <- 0
			g.reconnect()
			return
		}

		// Opcode 1 is a heartbeat request from the Discord gateway
		if p.Operation == 1 {
			g.wsMux.Lock()
			err := g.conn.WriteJSON(gatewayHeartbeat{1, atomic.LoadInt64(g.sequence)})
			g.wsMux.Unlock()

			if err != nil {
				log.Printf("error sending heartbeat on request of gateway: %v\n", err)
			}
		}

		if p.Operation == 0 {
			atomic.StoreInt64(g.sequence, p.Sequence)
			g.handleEvent(p)
		}
	}
}

func (g *gateway) handleEvent(p payload) {
	log.Println(p.Type)
	if p.Type == readyEvent {
		var r ready
		err := json.Unmarshal(p.EventData, &r)
		if err != nil {
			log.Println("could not unmarshal readyEvent:", err)
		}
		g.sessionInfo = r
	}

	if p.Type == voiceStateUpdateEvent || p.Type == voiceServerUpdateEvent {
		g.voiceUpdateResponse <- p
	}

	if _, ok := g.eventHandlers[p.Type]; ok {
		go g.eventHandlers[p.Type](p.EventData)
	}
}

func (g *gateway) identify() error {
	log.Println("sending gateway identification")

	ide, err := json.Marshal(gatewayIdentification{g.token, properties{"windows", "go-bot", "go-bot"}})
	if err != nil {
		return fmt.Errorf("failed to marshal gateway identification: %v", err)
	}

	g.wsMux.Lock()
	err = g.conn.WriteJSON(simplePayload{2, ide})
	g.wsMux.Unlock()

	if err != nil {
		return fmt.Errorf("failed to send identification: %v", err)
	}

	return nil
}

func (g *gateway) startHeart(interval int, stop chan int) {
	log.Println("heart started")

	ticker := time.NewTicker(time.Millisecond * time.Duration(interval))
	defer ticker.Stop()

	for {
		g.wsMux.Lock()
		err := g.conn.WriteJSON(gatewayHeartbeat{1, atomic.LoadInt64(g.sequence)})
		g.wsMux.Unlock()

		if err != nil {
			log.Printf("error sending gateway heartbeat: %v\n", err)
		}

		select {
		case <-ticker.C:
		case <-stop:
			return
		}
	}
}

func (g *gateway) reconnect() {
	log.Println("reconnecting to gateway")

	g.wsMux.Lock()
	g.conn.Close()

	u, err := getWsURL()
	if err != nil {
		log.Printf("error getting websocket url while reconnecting: %v", err)
		return
	}

	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		log.Fatal(fmt.Sprintf("failed to reconnect: %v\n exiting..", err))
		// would probably be good to terminate the voice connecion when this happens
		return
	}

	g.conn = conn
	g.wsMux.Unlock()

	go g.open()
}

// requestVoice sends a VoiceStateUpdate to the Discord voice server to
// let it know that we want to connect, Discord should responed with
// a VOICE_SERVER_UPDATE event and a VOICE_STATE_UPDATE event
func (g *gateway) requestVoice(channelID string) error {
	voiceState := voiceStateUpdate{
		GuildID:   g.sessionInfo.UnavailableGuildes[0].GuildID,
		ChannelID: channelID,
		SelfMute:  false,
		SelfDeaf:  false}

	jsonData, err := json.Marshal(voiceState)
	if err != nil {
		return fmt.Errorf("error parsing voice state data: %v", err)
	}

	g.wsMux.Lock()
	err = g.conn.WriteJSON(simplePayload{4, jsonData})
	g.wsMux.Unlock()
	if err != nil {
		return fmt.Errorf("failed to send voice request: %v", err)
	}

	log.Println("voice request sent")
	return nil
}

func getWsURL() (string, error) {
	resp, err := http.Get("https://discordapp.com/api/gateway")
	if err != nil {
		return "", fmt.Errorf("failed to get websocket url: %v", err)
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read wsURL response: %v", err)
	}

	var u wsURL
	err = json.Unmarshal(body, &u)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling wsUrl: %v", err)
	}

	return u.URL, nil
}

type wsURL struct {
	URL string `json:"url"`
}

// heartbeat Opcode 1
type gatewayHeartbeat struct {
	Op   int   `json:"op"`
	Data int64 `json:"d"`
}

// HelloEvent Opcode 10
type gatewayHello struct {
	HeartbeatInterval int      `json:"heartbeat_interval"`
	Trace             []string `json:"_trace"`
}

// IdentifyEvent Opcode 2
type gatewayIdentification struct {
	Token string     `json:"token"`
	Specs properties `json:"properties"`
}

// Properties contains specs that are needed for
// identification
type properties struct {
	OperatigSystem string `json:"os"`
	Browser        string `json:"browser"`
	Device         string `json:"device"`
}
