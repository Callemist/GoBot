package gateway

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client handles communcation with Discords websocket api
type Client struct {
	token            string
	wsMux            sync.Mutex
	conn             *websocket.Conn
	sequence         *int64
	lastHeartbeatAck time.Time
	EventHandlers    map[string]func(json.RawMessage)
	SessionInfo      readyEvent
}

type wsURL struct {
	URL string `json:"url"`
}

// Payload is a wrapper for messages received by
// the Discord gateway
type payload struct {
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

// heartbeat Opcode 1
type heartbeatOp struct {
	Op   int   `json:"op"`
	Data int64 `json:"d"`
}

type readyEvent struct {
	Version            int                 `json:"v"`
	UserInfo           User                `json:"user"`
	PrivateChannels    []int               `json:"private_channels"`
	UnavailableGuildes []unavailableGuilde `json:"guilds"`
	SeasionID          string              `json:"session_id"`
	Trace              []string            `json:"_trace"`
}

type unavailableGuilde struct {
	Enabled   bool   `json:"enabled"`
	ChannelID string `json:"channel_id"`
}

type User struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	Bot           bool   `json:"bot"`
	MfaEnabled    bool   `json:"mfa_enabled"`
	Language      string `json:"locale"`
	Verified      bool   `json:"verified"`
	Email         string `json:"email"`
	Flags         int    `json:"flags"`
	PremiumType   int    `json:"premium_type"`
}

type ResumeEvent struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
	Sequence  int    `json:"seq"`
}

type MessageEvent struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	GuildID   string    `json:"guild_id"`
	Author    User      `json:"author"`
	Content   string    `json:"content"`
	Created   time.Time `json:"timestamp"`
	Edited    time.Time `json:"edited_timestamp"`
	TTS       bool      `json:"tts"`
	// Add more properties when needed
}

const (
	ChannelCreateEvent = "CHANNEL_CREATE"
	ChannelDeleteEvent = "CHANNEL_DELETE"
	ChannelUpdateEvent = "CHANNEL_UPDATE"
	ConnectEvent       = "__CONNECT__"
	DisconnectEvent    = "__DISCONNECT__"
	GuildCreateEvent   = "GUILD_CREATE"
	GuildUpdateEvent   = "GUILD_UPDATE"
	MessageCreateEvent = "MESSAGE_CREATE"
	TypingStartEvent   = "TYPING_START"
)
