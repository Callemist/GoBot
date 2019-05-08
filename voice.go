package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/nacl/secretbox"
)

// voice is used for interfacing with Discords voice api
type voice struct {
	serverInfo       voiceServerUpdate
	userInfo         voiceStateUpdateResponse
	wsMux            sync.Mutex
	conn             *websocket.Conn
	lastHeartbeatAck time.Time
	currentChannelID string
	udpInfo          voiceReady
	encryptionMode   string
	secretKey        [32]byte
	udpConn          net.Conn
	opusReceiver     chan []byte

	// writing to this channel shutsdown the voice websocket
	// and udp connection
	exitc chan int
}

func newVoice() *voice {
	v := voice{}
	v.exitc = make(chan int)
	v.opusReceiver = make(chan []byte)
	return &v
}

func (v *voice) establishConnection(channelID string, gw *gateway) error {
	// hardcoded to join try hard pvp channel
	// TODO channelID should be picked from GUILDE state voice_states
	// check if user_id exist in voice_states and join that channel
	// wheh requested by text command
	channelID = "220255406964998144"
	v.currentChannelID = channelID

	respListener, err := gw.requestVoice(channelID)
	if err != nil {
		return fmt.Errorf("failed to request voice connection: %v", err)
	}

	// wait for the state and server update from the gateway
	// will cause a deadlock if called from the same goroutine that
	// the gateway open method runs in
	for i := 0; i < 2; i++ {
		p := <-respListener

		if p.Type == voiceStateUpdateEvent {
			var vstate voiceStateUpdateResponse
			err := json.Unmarshal(p.EventData, &vstate)
			if err != nil {
				return fmt.Errorf("could not unmarshal voiceStateUpdateResponse: %v", err)
			}
			v.userInfo = vstate
		}

		if p.Type == voiceServerUpdateEvent {
			var vServer voiceServerUpdate
			err := json.Unmarshal(p.EventData, &vServer)
			if err != nil {
				return fmt.Errorf("could not unmarshal voiceServerUpdate: %v", err)
			}
			v.serverInfo = vServer
		}
	}

	err = v.connectToVoiceWebsocket()
	if err != nil {
		return fmt.Errorf("failed to establish voice websocket connection: %v", err)
	}

	err = v.identify()
	if err != nil {
		return fmt.Errorf("error sending voice identification: %v", err)
	}

	go v.open()
	return nil
}

func (v *voice) connectToVoiceWebsocket() error {
	URL := "wss://" + strings.TrimSuffix(v.serverInfo.Endpoint, ":80")
	conn, _, err := websocket.DefaultDialer.Dial(URL, nil)

	if err != nil {
		return fmt.Errorf("error creating voice websocket connection, %v", err)
	}

	v.conn = conn
	return nil
}

func (v *voice) identify() error {
	ide := voiceIdentification{
		v.serverInfo.GuildID,
		v.userInfo.UserID,
		v.userInfo.SessionID,
		v.serverInfo.Token}

	jsonData, err := json.Marshal(ide)
	if err != nil {
		return fmt.Errorf("error parsing voice identification: %v", err)
	}

	v.wsMux.Lock()
	err = v.conn.WriteJSON(simplePayload{0, jsonData})
	v.wsMux.Unlock()

	if err != nil {
		return fmt.Errorf("error sending voice identification: %v", err)
	}

	return nil
}

func (v *voice) open() {
	v.lastHeartbeatAck = time.Now().UTC()
	stopHeart := make(chan int)
	terminateUDPConnection := make(chan int)
	var interval float32

	for {
		select {
		case <-v.exitc:
			stopHeart <- 0
			terminateUDPConnection <- 0
			v.wsMux.Lock()
			v.conn.Close()
			v.wsMux.Unlock()
			return
		default:
		}

		_, message, err := v.conn.ReadMessage()
		if err != nil {
			log.Println(fmt.Errorf("error reading message: %v", err))
			// This might cause problems so add back later
			// log.Println("trying to re-establish connection")
			// stopc <- 0
			// c.wsMux.Lock()
			// c.conn.Close()
			// c.wsMux.Unlock()
			// c.EstablishConnection(c.currentChannelID)

			terminateUDPConnection <- 0
			stopHeart <- 0
			return
		}

		var pretty bytes.Buffer
		json.Indent(&pretty, message, "", "    ")
		log.Printf("voice received:\n%s\n", string(pretty.Bytes()))

		var p payload
		err = json.Unmarshal(message, &p)
		if err != nil {
			log.Printf("error parsing payload: %v\n", err)
		}

		if p.Operation == 2 {
			var ready voiceReady
			err := json.Unmarshal(p.EventData, &ready)
			if err != nil {
				// TODO handle this better
				log.Fatal(fmt.Errorf("error parsing ready event %v", err))
			}
			v.udpInfo = ready
			err = v.establishUDPConnection()
			if err != nil {
				// TODO handle this better
				log.Fatal(fmt.Errorf("error connecting to voice UDP %v", err))
			}

		}

		if p.Operation == 8 {
			var he voiceHello
			err := json.Unmarshal(p.EventData, &he)
			if err != nil {
				// TODO handle this better
				log.Fatal(fmt.Errorf("error parsing hello event %v", err))
			}

			interval = float32(he.HeartbeatInterval) * 0.75
			go v.startHeart(interval, stopHeart)
		}

		// session description
		if p.Operation == 4 {
			var sd sessionDescription
			err := json.Unmarshal(p.EventData, &sd)
			if err != nil {
				log.Printf("error parsing description %v\n", err)
			}
			v.encryptionMode = sd.Encryption
			v.secretKey = sd.SecretKey

			go v.startOpusSender(terminateUDPConnection)

		}

		if p.Operation == 3 {
			v.lastHeartbeatAck = time.Now().UTC()
			log.Println("received voice ACK")
		}

		if time.Now().UTC().Sub(v.lastHeartbeatAck) > time.Millisecond*time.Duration(interval) {
			stopHeart <- 0
			terminateUDPConnection <- 0
			v.reconnect()
			return
		}
	}
}

func (v *voice) startHeart(interval float32, stop chan int) {
	log.Println("Voice Heart started")

	ticker := time.NewTicker(time.Millisecond * time.Duration(interval))
	defer ticker.Stop()
	nonce := 1

	for {
		v.wsMux.Lock()
		err := v.conn.WriteJSON(voiceHeartbeat{3, nonce})
		v.wsMux.Unlock()

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

func (v *voice) establishUDPConnection() error {
	addr := fmt.Sprintf("%s:%d", v.udpInfo.IP, v.udpInfo.Port)
	log.Printf("connecting to UDP address: %s\n", addr)

	conn, err := net.Dial("udp", addr)
	if err != nil {
		return fmt.Errorf("error establishing UDP connection: %v", err)
	}
	v.udpConn = conn

	sendBuffer := make([]byte, 70)
	binary.BigEndian.PutUint32(sendBuffer, v.udpInfo.SSRC)
	_, err = v.udpConn.Write(sendBuffer)
	if err != nil {
		return fmt.Errorf("error starting IP discovery: %v", err)
	}

	readBuffer := make([]byte, 70)
	n, err := v.udpConn.Read(readBuffer)
	if err != nil {
		return fmt.Errorf("error reading UDP response: %v", err)
	}
	if n < 70 {
		return errors.New("IP discovery resposne needs to be 70 bytes")
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

	v.wsMux.Lock()
	err = v.conn.WriteJSON(simplePayload{1, jsonData})
	v.wsMux.Unlock()
	if err != nil {
		return fmt.Errorf("error sending UDP communcation info: %v", err)
	}

	return nil
}

func (v *voice) sendOpusData(data []byte) {
	v.opusReceiver <- data
}

func (v *voice) startOpusSender(terminate <-chan int) {
	// this part of the header will stay the same for all packages
	RTPHeader := make([]byte, 12)
	RTPHeader[0] = 0x80
	RTPHeader[1] = 0x78
	binary.BigEndian.PutUint32(RTPHeader[8:], v.udpInfo.SSRC)

	// https://loadmultiplier.com/content/rtp-timestamp-calculation
	// send every 20 miliseconds = 50 sends per second
	// Discord wants 48kHz audio
	// timestam incrementation value = sampling rate / packets per second
	// 48000 / 50 = 960
	ticker := time.NewTicker(time.Millisecond * 20)
	defer ticker.Stop()

	var timestamp uint32
	var sequnce uint16
	var nonce [24]byte
	var frame []byte

	for {
		select {
		case frame = <-v.opusReceiver:
		case <-terminate:
			v.udpConn.Close()
			return
		}

		binary.BigEndian.PutUint16(RTPHeader[2:], sequnce)
		sequnce++

		binary.BigEndian.PutUint32(RTPHeader[4:], timestamp)
		timestamp += 960

		copy(nonce[:], RTPHeader)

		sendbuf := secretbox.Seal(RTPHeader, frame, &nonce, &v.secretKey)

		<-ticker.C

		_, err := v.udpConn.Write(sendbuf)
		if err != nil {
			log.Printf("error writing to UDP connection: %v\n", err)
		}
	}
}

func (v *voice) reconnect() {
	log.Println("reconnecting to voice")

	type resumeConnection struct {
		ServerID  string `json:"server_id"`
		SessionID string `json:"session_id"`
		Token     string `json:"token"`
	}

	v.wsMux.Lock()
	v.conn.Close()
	err := v.connectToVoiceWebsocket()
	if err != nil {
		// TODO handle better / log to discord text chat if possible
		log.Printf("error recreating voice ws: %v", err)
		return
	}

	rc := resumeConnection{v.serverInfo.GuildID, v.userInfo.SessionID, v.serverInfo.Token}
	jsonData, _ := json.Marshal(rc)

	err = v.conn.WriteJSON(simplePayload{7, jsonData})
	if err != nil {
		// TODO handle better / log to discord text chat if possible
		log.Printf("error sending resume voice payload: %v", err)
		return
	}

	v.wsMux.Unlock()
	v.open()
}

func (v *voice) speaking(b bool) error {
	type voiceSpeakingData struct {
		Speaking bool `json:"speaking"`
		Delay    int  `json:"delay"`
		SSRC     int  `json:"ssrc"`
	}

	type voiceSpeaking struct {
		Op   int               `json:"op"` // Always 5
		Data voiceSpeakingData `json:"d"`
	}

	v.wsMux.Lock()
	err := v.conn.WriteJSON(voiceSpeaking{5, voiceSpeakingData{b, 0, int(v.udpInfo.SSRC)}})
	v.wsMux.Unlock()

	if err != nil {
		return fmt.Errorf("failed to send speaking state: %v", err)
	}
	return nil
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

type voiceIdentification struct {
	ServerID  string `json:"server_id"`
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	Token     string `json:"token"`
}

type voiceReady struct {
	SSRC            uint32   `json:"ssrc"`
	IP              string   `json:"ip"`
	Port            int      `json:"port"`
	EncryptionModes []string `json:"modes"`
}

type voiceHello struct {
	// Use HeartbeatInterval * 0.75 to avoid Discord bug
	HeartbeatInterval int `json:"heartbeat_interval"`
}

type voiceHeartbeat struct {
	Op    int `json:"op"`
	Nonce int `json:"d"`
}
