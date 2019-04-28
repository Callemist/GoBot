package voice

import (
	"encoding/json"
	"go-bot/events"
	"go-bot/gateway"
	"log"
)

// Client is used for interfacing with the voice api
type Client struct {
	gateway    *gateway.Client
	serverInfo events.VoiceServerUpdateEvent
	userInfo   events.VoiceStateUpdateResponseEvent
}

// NewClient retunres an initialized voice client
func NewClient(g *gateway.Client) *Client {
	c := Client{}
	c.gateway = g
	return &c
}

func (c *Client) EstablishConnection(channelID string) {
	// hardcoded to join plugg channel
	// TODO channelID should be picked from GUILDE state voice_states
	// check if user_id exist in voice_states and join that channel
	// wheh requested by text command
	channelID = "264090324190625794"
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
}
