package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go-bot/events"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Client handles communcation with Discords websocket api
type Client struct {
	token               string
	wsMux               sync.Mutex
	conn                *websocket.Conn
	sequence            *int64
	lastHeartbeatAck    time.Time
	EventHandlers       map[string]func(json.RawMessage)
	SessionInfo         events.ReadyEvent
	VoiceUpdateResponse chan Payload
}

// NewClient returns a client to subscribe on Discord events
// sent via the gateway.
func NewClient(t string) (*Client, error) {
	var client Client
	client.token = t
	client.sequence = new(int64)
	client.EventHandlers = make(map[string]func(json.RawMessage))
	client.VoiceUpdateResponse = make(chan Payload)

	u, err := getWsURL()

	if err != nil {
		return &Client{}, err
	}

	c, _, err := websocket.DefaultDialer.Dial(u, nil)

	if err != nil {
		return &Client{}, fmt.Errorf("error creating client websocket connection: %v", err)
	}

	client.conn = c
	return &client, nil
}

// Start initiate a new gateway connection and start listening for events.
func (c *Client) Start() {
	c.lastHeartbeatAck = time.Now().UTC()
	stopc := make(chan int)
	var interval int

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("Error reading message: %s\n", err)
		}

		var pretty bytes.Buffer
		json.Indent(&pretty, message, "", "    ")

		//log.Printf("received:\n%s\n", string(pretty.Bytes()))

		var p Payload
		err = json.Unmarshal(message, &p)

		if err != nil {
			log.Printf("Invalid json: %s\n", err)
		}

		// Hello event
		if p.Operation == 10 {
			var he helloEvent
			err := json.Unmarshal(p.EventData, &he)

			if err != nil {
				log.Printf("Invalid json: %s\n", err)
			}

			c.identify()

			interval = he.HeartBeatInterval
			go c.startHeartbeat(he.HeartBeatInterval, stopc)
		}

		// Heartbeat ACK event
		if p.Operation == 11 {
			c.lastHeartbeatAck = time.Now().UTC()
			log.Println("Received ACK")
		}

		// Opcode 1 is a heartbeat request from the Discord gateway
		if p.Operation == 1 {
			c.wsMux.Lock()
			err := c.conn.WriteJSON(heartbeatOp{1, atomic.LoadInt64(c.sequence)})
			c.wsMux.Unlock()

			if err != nil {
				log.Printf("Error sending heartbeat on request of gateway: %s", err)
			}
		}

		if p.Operation == 0 {
			atomic.StoreInt64(c.sequence, p.Sequence)
			c.handleEvent(p)
		}

		if time.Now().UTC().Sub(c.lastHeartbeatAck) > time.Millisecond*time.Duration(interval*2) {
			stopc <- 0
			c.reconnect()
			return
		}
	}
}

func (c *Client) handleEvent(p Payload) {
	log.Println(p.Type)
	if p.Type == events.Ready {
		var r events.ReadyEvent
		err := json.Unmarshal(p.EventData, &r)
		if err != nil {
			log.Println("could not unmarshal readyEvent:", err)
		}
		c.SessionInfo = r
	}

	if p.Type == events.VoiceStateUpdate || p.Type == events.VoiceServerUpdate {
		c.VoiceUpdateResponse <- p
	}

	if _, ok := c.EventHandlers[p.Type]; ok {
		c.EventHandlers[p.Type](p.EventData)
	}
}

func (c *Client) identify() error {
	log.Println("Identifying")
	ide, err := json.Marshal(identifyEvent{c.token, properties{"windows", "go-bot", "go-bot"}})

	if err != nil {
		log.Printf("Invalid json: %s\n", err)
		return err
	}

	ideResponse := Payload{}
	ideResponse.Operation = 2
	ideResponse.EventData = ide

	c.wsMux.Lock()
	err = c.conn.WriteJSON(ideResponse)
	c.wsMux.Unlock()

	if err != nil {
		log.Printf("Error sending identification: %s\n", err)
		return err
	}

	return nil
}

func (c *Client) startHeartbeat(interval int, stop chan int) {
	log.Println("Heart started")

	ticker := time.NewTicker(time.Millisecond * time.Duration(interval))
	defer ticker.Stop()

	for {
		c.wsMux.Lock()
		err := c.conn.WriteJSON(heartbeatOp{1, atomic.LoadInt64(c.sequence)})
		c.wsMux.Unlock()

		if err != nil {
			log.Printf("Error sending heartbeat: %s\n", err)
		}

		select {
		case <-ticker.C:
		case <-stop:
			return
		}
	}
}

func (c *Client) reconnect() {
	log.Println("Reconnecting")
	c.wsMux.Lock()
	c.conn.Close()
	u, err := getWsURL()

	if err != nil {
		log.Println(err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(u, nil)

	if err != nil {
		log.Printf("Error creating websocket connection: %s\n", err)
	}

	c.conn = conn
	c.wsMux.Unlock()

	go c.Start()
}

// RequestVoice sends a VoiceStateUpdate to the Discord voice server to
// let it know that we want to connect, Discord should responed with
// a VOICE_SERVER_UPDATE event and a VOICE_STATE_UPDATE event
func (c *Client) RequestVoice(channelID string) {
	voiceState := events.VoiceStateUpdateEvent{
		GuildID:   c.SessionInfo.UnavailableGuildes[0].GuildID,
		ChannelID: channelID,
		SelfMute:  false,
		SelfDeaf:  false}

	jsonData, err := json.Marshal(voiceState)

	if err != nil {
		log.Println("error parsing voice state data:", err)
	}

	p := Payload{}
	p.Operation = 4
	p.EventData = jsonData

	c.wsMux.Lock()
	c.conn.WriteJSON(p)
	c.wsMux.Unlock()

	log.Println("voice request sent")

	if err != nil {
		log.Println("error requesting voice connection:", err)
	}
}

func getWsURL() (string, error) {
	resp, err := http.Get("https://discordapp.com/api/gateway")

	if err != nil {
		return "", fmt.Errorf("error getting gateway websocket url: %v", err)
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return "", fmt.Errorf("error readying wsURL response: %v", err)
	}

	var u wsURL
	err = json.Unmarshal(body, &u)

	if err != nil {
		return "", fmt.Errorf("error parsing wsUrl: %v", err)
	}

	return u.URL, nil
}

type wsURL struct {
	URL string `json:"url"`
}

// heartbeat Opcode 1
type heartbeatOp struct {
	Op   int   `json:"op"`
	Data int64 `json:"d"`
}

// IdentifyEvent Opcode 2
type identifyEvent struct {
	Token string     `json:"token"`
	Specs properties `json:"properties"`
}

// Properties contains specs that are needed for
// identification
type properties struct {
	OS      string `json:"os"`
	Browser string `json:"browser"`
	Device  string `json:"device"`
}

// Payload is a wrapper for messages received by
// the Discord gateway
type Payload struct {
	Operation int             `json:"op"`
	EventData json.RawMessage `json:"d"`
	Sequence  int64           `json:"s"`
	Type      string          `json:"t"`
}

// HelloEvent Opcode 10
type helloEvent struct {
	HeartBeatInterval int      `json:"heartbeat_interval"`
	Trace             []string `json:"_trace"`
}
