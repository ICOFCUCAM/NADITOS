// Package notifications defines the contract for SMS / email / push providers.
package notifications

import (
	"context"

	"github.com/icofcucam/naditos/packages/go-common/contracts"
)

type Channel string

const (
	ChannelSMS   Channel = "sms"
	ChannelEmail Channel = "email"
	ChannelPush  Channel = "push"
)

type Message struct {
	TenantID string
	Channel  Channel
	To       string
	Subject  string            // email only
	Body     string
	TemplateID string          // optional, for transactional templates
	Data       map[string]any  // template vars
}

type Receipt struct {
	ID       string
	Status   string // queued | sent | failed | delivered
	Provider string
}

type Sender interface {
	Info() contracts.AdapterInfo
	Send(ctx context.Context, m Message) (*Receipt, error)
}
