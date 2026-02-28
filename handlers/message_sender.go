package handlers

import (
	"time"

	"github.com/cenkalti/backoff/v4"

	"eksecd/clients"
	"eksecd/core/log"
)

// OutgoingMessage represents a message to be sent via HTTP POST
type OutgoingMessage struct {
	Event string
	Data  any
}

// MessageSender handles queuing and sending messages to the backend via HTTP.
// It provides backpressure through a buffered channel (size 1) ensuring
// that callers block until their message is consumed.
type MessageSender struct {
	messageQueue chan OutgoingMessage
	apiClient    *clients.AgentsApiClient
}

// NewMessageSender creates a new MessageSender instance.
func NewMessageSender() *MessageSender {
	return &MessageSender{
		messageQueue: make(chan OutgoingMessage, 1),
	}
}

// Run starts the message sender goroutine that processes the queue.
// It blocks until the message queue is closed.
func (ms *MessageSender) Run(apiClient *clients.AgentsApiClient) {
	ms.apiClient = apiClient
	log.Info("📤 MessageSender: Started processing queue (HTTP mode)")

	for msg := range ms.messageQueue {
		ms.sendWithRetry(msg)
	}

	log.Info("📤 MessageSender: Queue closed, exiting")
}

// sendWithRetry attempts to send a message via HTTP with exponential backoff retry logic.
func (ms *MessageSender) sendWithRetry(msg OutgoingMessage) {
	expBackoff := backoff.NewExponentialBackOff()
	expBackoff.InitialInterval = 1 * time.Second
	expBackoff.MaxInterval = 4 * time.Second
	expBackoff.MaxElapsedTime = 10 * time.Second
	expBackoff.Multiplier = 2

	attempt := 0
	operation := func() error {
		attempt++
		err := ms.apiClient.SubmitMessage(msg.Data)
		if err != nil {
			log.Warn("⚠️ MessageSender: Failed to submit message on event '%s' (attempt %d): %v", msg.Event, attempt, err)
			return err
		}
		log.Info("📤 MessageSender: Successfully submitted message on event '%s' (attempt %d)", msg.Event, attempt)
		return nil
	}

	err := backoff.Retry(operation, expBackoff)
	if err != nil {
		log.Error("❌ MessageSender: Failed to submit message on event '%s' after %d attempts: %v. Message lost.", msg.Event, attempt, err)
	}
}

// QueueMessage adds a message to the send queue.
// Blocks until the message is consumed by the sender goroutine.
func (ms *MessageSender) QueueMessage(event string, data any) {
	log.Info("📥 MessageSender: Queueing message for event '%s'", event)
	ms.messageQueue <- OutgoingMessage{
		Event: event,
		Data:  data,
	}
	log.Info("📤 MessageSender: Message for event '%s' has been consumed by sender", event)
}

// Close closes the message queue, causing Run() to exit.
func (ms *MessageSender) Close() {
	close(ms.messageQueue)
}
