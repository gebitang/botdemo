package main

import (
	"context"
	"encoding/json"
	"github.com/MixinNetwork/bot-api-go-client"
	"log"
	"os"
)

func (f mixinBlazeHandler) OnMessage(ctx context.Context, msg bot.MessageView, clientID string) error {
	return f(ctx, msg, clientID)
}

func (f mixinBlazeHandler) OnAckReceipt(ctx context.Context, msg bot.MessageView, clientID string) error {
	indent, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		return err
	}
	log.Println("ack Message...", string(indent))
	return nil
}

func (f mixinBlazeHandler) SyncAck() bool {
	return true
}

func main() {
	name := "config.json"
	args := os.Args[1:]
	if len(args) >= 1 {
		name = args[0]
	}
	config, err := readConfig(name)
	if err != nil {
		return
	}
	initHelp()
	ctx := context.Background()
	mars = NewClient(ctx, config)
	for {
		client := bot.NewBlazeClient(config.ClientId, config.SessionId, config.PrivateKey)
		mars.Client = client
		if err := client.Loop(ctx, mixinBlazeHandler(handler)); err != nil {
			log.Println("test...", err)
		}
	}
}
