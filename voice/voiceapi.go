package voice

import (
	"go-bot/gateway"
)

// Client is used for interfacing with the voice api
type Client struct {
	gateway *gateway.Client
}

// NewClient retunres an initialized voice client
func NewClient(g *gateway.Client) *Client {
	c := Client{}
	c.gateway = g
	return &c
}

func (c *Client) EstablishConnection(channelID string) {
	//c.RequestVoice()
}
