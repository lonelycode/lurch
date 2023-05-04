package main

import (
	"fmt"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"log"
	"os"
	"strings"
)

func handleEvents(client *socketmode.Client, lurch *LurchBot) {
	for evt := range client.Events {
		switch evt.Type {
		case socketmode.EventTypeEventsAPI:
			eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
			if !ok {
				//fmt.Printf("Ignored %+v\n", evt)
				continue
			}

			fmt.Printf("Event received: %+v\n", eventsAPIEvent)

			client.Ack(*evt.Request)

			switch eventsAPIEvent.Type {
			case slackevents.CallbackEvent:
				innerEvent := eventsAPIEvent.InnerEvent
				switch ev := innerEvent.Data.(type) {
				case *slackevents.AppMentionEvent:
					handleAppMentionEvent(lurch, client, ev)
				}
			default:
				client.Debugf("unsupported Events API event received")
			}
		case socketmode.EventTypeHello:
			//
		default:
			fmt.Fprintf(os.Stderr, "Unexpected event type received: %s\n", evt.Type)
		}
	}
}

func handleAppMentionEvent(lurch *LurchBot, client *socketmode.Client, ev *slackevents.AppMentionEvent) {
	// Find all matches using the pattern
	matches := IDPattern.FindAllStringSubmatch(ev.Text, -1)
	message := ev.Text

	// Remove userID from response
	if len(matches) > 0 {
		message = strings.ReplaceAll(message, matches[0][0], "")
	}

	message = strings.Trim(message, " ")
	response, err := lurch.Chat(ev.User, message)
	if err != nil {
		log.Fatal(err)
	}
	_, _, err = client.PostMessage(
		ev.Channel,
		slack.MsgOptionText(response, false),
		slack.MsgOptionTS(ev.TimeStamp),
		slack.MsgOptionDisableLinkUnfurl())
	if err != nil {
		log.Fatalf("failed posting message: %v", err)
	}
}
