package main

import (
	"encoding/json"
	"time"
)

// payload is a wrapper for sending and receiving messages from Discord
type payload struct {
	Operation int             `json:"op"`
	EventData json.RawMessage `json:"d"`
	Sequence  int64           `json:"s"`
	Type      string          `json:"t"`
}

// simplePayload is for messages that does not need a sequence number or type
type simplePayload struct {
	Operation int             `json:"op"`
	EventData json.RawMessage `json:"d"`
}

type ready struct {
	Version            int                 `json:"v"`
	User               user                `json:"user"`
	PrivateChannels    []int               `json:"private_channels"`
	UnavailableGuildes []unavailableGuilde `json:"guilds"`
	SeasionID          string              `json:"session_id"`
	Trace              []string            `json:"_trace"`
}

type resume struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
	Sequence  int    `json:"seq"`
}

type message struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	GuildID   string    `json:"guild_id"`
	Author    user      `json:"author"`
	Content   string    `json:"content"`
	Created   time.Time `json:"timestamp"`
	Edited    time.Time `json:"edited_timestamp"`
	TTS       bool      `json:"tts"`
	// Add more properties when needed
}

type voiceStateUpdate struct {
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	SelfMute  bool   `json:"self_mute"`
	SelfDeaf  bool   `json:"self_deaf"`
}

type voiceStateUpdateResponse struct {
	Member    member `json:"member"`
	UserID    string `json:"user_id"`
	Suppress  bool   `json:"suppress"`
	SessionID string `json:"session_id"`
	SelfMute  bool   `json:"self_mute"`
	SelfDeaf  bool   `json:"self_deaf"`
	Mute      bool   `json:"mute"`
	GuildID   string `json:"guild_id"`
	Deaf      bool   `json:"deaf"`
	ChannelID string `json:"channel_id"`
}

type voiceServerUpdate struct {
	Token    string `json:"token"`
	GuildID  string `json:"guild_id"`
	Endpoint string `json:"endpoint"`
}

type member struct {
	User     user      `json:"user"`
	Roles    []string  `json:"roles"`
	Mute     bool      `json:"mute"`
	JoinedAt time.Time `json:"joined_at"`
	Deaf     bool      `json:"deaf"`
}

type unavailableGuilde struct {
	Unavailable bool   `json:"unavailable"`
	GuildID     string `json:"id"`
}

type user struct {
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

const (
	readyEvent             = "READY"
	channelCreateEvent     = "CHANNEL_CREATE"
	channelDeleteEvent     = "CHANNEL_DELETE"
	channelUpdateEvent     = "CHANNEL_UPDATE"
	connectEvent           = "__CONNECT__"
	disconnectEvent        = "__DISCONNECT__"
	guildCreateEvent       = "GUILD_CREATE"
	guildUpdateEvent       = "GUILD_UPDATE"
	messageCreateEvent     = "MESSAGE_CREATE"
	typingStartEvent       = "TYPING_START"
	voiceServerUpdateEvent = "VOICE_SERVER_UPDATE"
	voiceStateUpdateEvent  = "VOICE_STATE_UPDATE"
)
