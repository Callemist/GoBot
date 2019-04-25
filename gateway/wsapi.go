package gateway

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// NewClient returns a client to subscribe on Discord events
// sent via the gateway.
func NewClient(t string) (*Client, error) {
	var client Client
	client.token = t
	client.sequence = new(int64)
	client.EventHandlers = make(map[string]func(json.RawMessage))

	u, err := getWsURL()

	if err != nil {
		return &Client{}, err
	}

	c, _, err := websocket.DefaultDialer.Dial(u, nil)

	if err != nil {
		log.Println("Error creating websocket connection")
		return &Client{}, err
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

		log.Printf("received:\n%s\n", string(pretty.Bytes()))

		var p payload
		err = json.Unmarshal(message, &p)

		if err != nil {
			log.Printf("Invalid json: %s\n", err)
		}

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

func (c *Client) handleEvent(p payload) {
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

	ideResponse := payload{}
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
		log.Printf("Could not get wsURL: %s\n", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(u, nil)

	if err != nil {
		log.Printf("Error creating websocket connection: %s\n", err)
	}

	c.conn = conn
	c.wsMux.Unlock()

	go c.Start()
}

func getWsURL() (string, error) {
	resp, err := http.Get("https://discordapp.com/api/gateway")

	if err != nil {
		log.Println("Error getting websocket url")
		return "", err
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		log.Println("Error readying wsURL response")
		return "", err
	}

	var u wsURL
	err = json.Unmarshal(body, &u)

	if err != nil {
		log.Println("Error parsing url")
		return "", err
	}

	return u.URL, nil
}
