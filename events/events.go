package events

import "time"

type ReadyEvent struct {
	Version            int                 `json:"v"`
	UserInfo           User                `json:"user"`
	PrivateChannels    []int               `json:"private_channels"`
	UnavailableGuildes []UnavailableGuilde `json:"guilds"`
	SeasionID          string              `json:"session_id"`
	Trace              []string            `json:"_trace"`
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

type VoiceStateUpdateEvent struct {
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	SelfMute  bool   `json:"self_mute"`
	SelfDeaf  bool   `json:"self_deaf"`
}

type UnavailableGuilde struct {
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

const (
	Ready             = "READY"
	ChannelCreate     = "CHANNEL_CREATE"
	ChannelDelete     = "CHANNEL_DELETE"
	ChannelUpdate     = "CHANNEL_UPDATE"
	Connect           = "__CONNECT__"
	Disconnect        = "__DISCONNECT__"
	GuildCreate       = "GUILD_CREATE"
	GuildUpdate       = "GUILD_UPDATE"
	MessageCreate     = "MESSAGE_CREATE"
	TypingStart       = "TYPING_START"
	VoiceServerUpdate = "VOICE_SERVER_UPDATE"
	VoiceStateUpdate  = "VOICE_STATE_UPDATE"
)
