package voice

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"go-bot/events"
	"go-bot/gateway"
	"io"
	"log"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/nacl/secretbox"
)

// Client is used for interfacing with the voice api
type Client struct {
	gateway          *gateway.Client
	serverInfo       events.VoiceServerUpdateEvent
	userInfo         events.VoiceStateUpdateResponseEvent
	wsMux            sync.Mutex
	conn             *websocket.Conn
	lastHeartbeatAck time.Time
	currentChannelID string
	UDPInfo          voiceReadyEvent
	EncryptionMode   string
	SecretKey        [32]byte
}

// NewClient retunres an initialized voice client
func NewClient(g *gateway.Client) *Client {
	c := Client{}
	c.gateway = g
	return &c
}

func (c *Client) EstablishConnection(channelID string) {
	// hardcoded to join try hard pvp channel
	// TODO channelID should be picked from GUILDE state voice_states
	// check if user_id exist in voice_states and join that channel
	// wheh requested by text command
	channelID = "220255406964998144"

	c.currentChannelID = channelID
	c.gateway.RequestVoice(channelID)

	for i := 0; i < 2; i++ {
		p := <-c.gateway.VoiceUpdateResponse
		if p.Type == events.VoiceStateUpdate {
			var v events.VoiceStateUpdateResponseEvent
			err := json.Unmarshal(p.EventData, &v)
			if err != nil {
				log.Println("could not unmarshal VoiceStateUpdateResponseEvent:", err)
			}
			c.userInfo = v
		}

		if p.Type == events.VoiceServerUpdate {
			var v events.VoiceServerUpdateEvent
			err := json.Unmarshal(p.EventData, &v)
			if err != nil {
				log.Println("could not unmarshal VoiceStateUpdateResponseEvent:", err)
			}
			c.serverInfo = v
		}
	}

	err := c.connectToVoiceWebsocket()
	logIfError("", err)

	err = c.identify()
	logIfError("", err)

	c.start()
}

func (c *Client) connectToVoiceWebsocket() error {
	URL := "wss://" + strings.TrimSuffix(c.serverInfo.Endpoint, ":80")
	conn, _, err := websocket.DefaultDialer.Dial(URL, nil)

	if err != nil {
		return fmt.Errorf("error creating voice websocket connection, %v", err)
	}

	c.conn = conn
	return nil
}

func (c *Client) start() {
	c.lastHeartbeatAck = time.Now().UTC()
	stopc := make(chan int)
	var interval float32

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Println(fmt.Errorf("error reading message: %v", err))
			// This might cause problems so add back later

			// log.Println("trying to re-establish connection")

			// stopc <- 0
			// c.wsMux.Lock()
			// c.conn.Close()
			// c.wsMux.Unlock()
			// c.EstablishConnection(c.currentChannelID)

			stopc <- 0
			return
		}

		var pretty bytes.Buffer
		json.Indent(&pretty, message, "", "    ")
		log.Printf("voice received:\n%s\n", string(pretty.Bytes()))

		var p events.Payload
		err = json.Unmarshal(message, &p)

		if err != nil {
			log.Println(fmt.Errorf("error parsing payload: %v", err))
			return
		}

		if p.Operation == 2 {
			var ready voiceReadyEvent
			err := json.Unmarshal(p.EventData, &ready)

			if err != nil {
				log.Println(fmt.Errorf("error parsing ready event %v", err))
				return
			}

			c.UDPInfo = ready
			err = c.establishUDPConnection(ready)
			if err != nil {
				log.Println(err)
			}
		}

		if p.Operation == 8 {
			var he helloEvent
			err := json.Unmarshal(p.EventData, &he)

			if err != nil {
				log.Println(fmt.Errorf("error parsing hello event %v", err))
				return
			}

			interval = float32(he.HeartbeatInterval) * 0.75
			go c.startHeartbeat(interval, stopc)
		}

		// session description
		if p.Operation == 4 {
			var desc sessionDescription
			err := json.Unmarshal(p.EventData, &desc)
			if err != nil {
				log.Printf("error parsing description %v\n", err)
			}
			c.EncryptionMode = desc.Encryption
			c.SecretKey = desc.SecretKey
		}

		if p.Operation == 3 {
			c.lastHeartbeatAck = time.Now().UTC()
			log.Println("Received Voice ACK")
		}

		if time.Now().UTC().Sub(c.lastHeartbeatAck) > time.Millisecond*time.Duration(interval) {
			stopc <- 0
			c.reconnect()
			return
		}
	}
}

func (c *Client) establishUDPConnection(UDPInfo voiceReadyEvent) error {
	log.Println("establishing UDP voice connection")
	addr := fmt.Sprintf("%s:%d", UDPInfo.IP, UDPInfo.Port)
	log.Println(addr)

	// addr, err := net.ResolveUDPAddr("udp", UDPInfo.IP+":"+string(UDPInfo.Port))
	// if err != nil {
	// 	return fmt.Errorf("error creating UDP address: %v", err)
	// }

	conn, err := net.Dial("udp", addr)
	if err != nil {
		return fmt.Errorf("error establishing UDP connection: %v", err)
	}

	sendBuffer := make([]byte, 70)
	binary.BigEndian.PutUint32(sendBuffer, UDPInfo.SSRC)
	_, err = conn.Write(sendBuffer)
	if err != nil {
		return fmt.Errorf("error starting IP discovery: %v", err)
	}

	readBuffer := make([]byte, 70)
	_, err = conn.Read(readBuffer)
	if err != nil {
		return fmt.Errorf("error reading UDP response: %v", err)
	}

	var ip string
	for i := 4; i < 20; i++ {
		if readBuffer[i] == 0 {
			break
		}
		ip += string(readBuffer[i])
	}

	port := binary.LittleEndian.Uint16(readBuffer[68:70])

	jsonData, err := json.Marshal(communicationInfo{"udp", data{ip, port, "xsalsa20_poly1305"}})
	if err != nil {
		return fmt.Errorf("error parsing communicationInfo: %v", err)
	}

	p := events.Payload{}
	p.Operation = 1
	p.EventData = jsonData

	c.wsMux.Lock()
	err = c.conn.WriteJSON(p)
	c.wsMux.Unlock()

	if err != nil {
		return fmt.Errorf("error sending UDP communcation info: %v", err)
	}

	type voiceSpeakingData struct {
		Speaking bool `json:"speaking"`
		Delay    int  `json:"delay"`
		SSRC     int  `json:"ssrc"`
	}

	type voiceSpeakingOp struct {
		Op   int               `json:"op"` // Always 5
		Data voiceSpeakingData `json:"d"`
	}

	c.wsMux.Lock()
	c.conn.WriteJSON(voiceSpeakingOp{5, voiceSpeakingData{true, 0, int(c.UDPInfo.SSRC)}})
	c.wsMux.Unlock()

	timestamp := uint32(1)
	sequnce := uint16(0)
	RTPHeader := make([]byte, 12)

	RTPHeader[0] = 0x80
	RTPHeader[1] = 0x78
	binary.BigEndian.PutUint32(RTPHeader[8:], c.UDPInfo.SSRC)

	yID := "LDU_Txk06tM"
	cmd := exec.Command("youtube-dl", "-o", "-", "-f 251", yID)

	r, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(fmt.Errorf("error getting StdoutPipe: %v", err))
	}

	err = cmd.Start()
	if err != nil {
		log.Fatal(fmt.Errorf("error starting command: %v", err))
	}

	buf := make([]byte, 0, 4096)
	for {
		var nonce [24]byte

		binary.BigEndian.PutUint16(RTPHeader[2:], sequnce)
		binary.BigEndian.PutUint32(RTPHeader[4:], timestamp)

		copy(nonce[:], RTPHeader)

		// using buf[:cap(buf)] will make the slice use the whole underlying array of 4096 bytes
		n, err := r.Read(buf[:cap(buf)])

		// only use the read bytes of buf
		// since the slice is between 0 and n the cap of buf will not change
		buf = buf[:n]

		// read will not return an error if n > 0
		if n == 0 {
			if err == nil {
				// nothing happend
				continue
			}
			if err == io.EOF {
				break
			}
			log.Fatal(fmt.Errorf("error reading from stdout: %v", err))
		}

		sendbuf := secretbox.Seal(RTPHeader, buf, &nonce, &c.SecretKey)

		nb, err := conn.Write(sendbuf)
		if err != nil {
			log.Printf("error sending to udp voice: %v\n", err)
		}
		sequnce++
		timestamp += 960
		log.Println("sent:", nb)
	}

	return nil
}

func (c *Client) reconnect() {
	log.Println("Reconnecting to voice websocket")

	type resumeConnection struct {
		ServerID  string `json:"server_id"`
		SessionID string `json:"session_id"`
		Token     string `json:"token"`
	}

	r := resumeConnection{c.serverInfo.GuildID, c.userInfo.SessionID, c.serverInfo.Token}
	jsonData, _ := json.Marshal(r)

	p := events.Payload{}
	p.Operation = 7
	p.EventData = jsonData

	c.wsMux.Lock()
	c.conn.Close()
	err := c.connectToVoiceWebsocket()
	logIfError("error recreating voice ws:", err)

	err = c.conn.WriteJSON(p)
	logIfError("error sending resume event:", err)

	c.wsMux.Unlock()

	c.start()
}

func (c *Client) identify() error {
	ide := identification{
		c.serverInfo.GuildID,
		c.userInfo.UserID,
		c.userInfo.SessionID,
		c.serverInfo.Token}

	jsonData, err := json.Marshal(ide)

	if err != nil {
		return fmt.Errorf("error parsing voice identification: %v", err)
	}

	p := events.Payload{}
	p.Operation = 0
	p.EventData = jsonData

	c.wsMux.Lock()
	err = c.conn.WriteJSON(p)
	c.wsMux.Unlock()

	if err != nil {
		return fmt.Errorf("error sending voice identification: %v", err)
	}

	return nil
}

func (c *Client) startHeartbeat(interval float32, stop chan int) {
	log.Println("Voice Heart started")

	ticker := time.NewTicker(time.Millisecond * time.Duration(interval))
	defer ticker.Stop()
	nonce := 1

	for {
		c.wsMux.Lock()
		err := c.conn.WriteJSON(heartbeat{3, nonce})
		c.wsMux.Unlock()

		if err != nil {
			log.Printf("error sending voice heartbeat: %v\n", err)
		}

		nonce++

		select {
		case <-ticker.C:
		case <-stop:
			return
		}
	}
}

func logIfError(text string, err error) {
	if err != nil {
		log.Println(fmt.Errorf("%s %v", text, err))
	}
}

type sessionDescription struct {
	Encryption     string   `json:"mode"`
	SecretKey      [32]byte `json:"secret_key"`
	MediaSessionID string   `json:"media_session_id"`
	VideoCodec     string   `json:"video_codec"`
	AudioCodec     string   `json:"audio_codec"`
}

type communicationInfo struct {
	Protocol string `json:"protocol"`
	Data     data   `json:"data"`
}

type data struct {
	Address    string `json:"address"`
	Port       uint16 `json:"port"`
	Encryption string `json:"mode"`
}

type identification struct {
	ServerID  string `json:"server_id"`
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	Token     string `json:"token"`
}

type voiceReadyEvent struct {
	SSRC            uint32   `json:"ssrc"`
	IP              string   `json:"ip"`
	Port            int      `json:"port"`
	EncryptionModes []string `json:"modes"`
}

type helloEvent struct {
	// Use HeartbeatInterval * 0.75 to avoid Discord bug
	HeartbeatInterval int `json:"heartbeat_interval"`
}

type heartbeat struct {
	Op    int `json:"op"`
	Nonce int `json:"d"`
}
