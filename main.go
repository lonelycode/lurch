package main

import (
	"bytes"
	"fmt"
	"github.com/lonelycode/botMaker"
	"github.com/sashabaranov/go-openai"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"log"
	"os"
	"strings"
	"text/template"
)

func main() {
	appToken := os.Getenv("SLACK_APP_TOKEN")
	botToken := os.Getenv("SLACK_BOT_TOKEN")
	if appToken == "" || botToken == "" {
		log.Fatal("SLACK_APP_TOKEN and SLACK_BOT_TOKEN must be set")
	}

	api := slack.New(
		botToken,
		slack.OptionDebug(false),
		slack.OptionLog(log.New(os.Stdout, "api: ", log.Lshortfile|log.LstdFlags)),
		slack.OptionAppLevelToken(appToken),
	)

	client := socketmode.New(
		api,
		socketmode.OptionDebug(false),
		socketmode.OptionLog(log.New(os.Stdout, "socketmode: ", log.Lshortfile|log.LstdFlags)),
	)

	lurch := &LurchBot{}
	lurch.Init("tyk-doc")

	go func() {
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
						message := strings.ReplaceAll(ev.Text, "<@U055QMW21DF>", "")
						message = strings.Trim(message, " ")
						response, err := lurch.Chat(ev.User, message)
						_, _, err = client.PostMessage(
							ev.Channel,
							slack.MsgOptionText(response, false))
						if err != nil {
							log.Fatalf("failed posting message: %v", err)
						}
					}
				default:
					client.Debugf("unsupported Events API event received")
				}
			default:
				fmt.Fprintf(os.Stderr, "Unexpected event type received: %s\n", evt.Type)
			}
		}
	}()

	err := client.Run()
	if err != nil {
		log.Println(err)
	}
}

type LurchBot struct {
	Conversations map[string][]*botMaker.RenderContext
	settings      *botMaker.BotSettings
	oai           *botMaker.OAIClient
	config        *botMaker.Config
}

func (l *LurchBot) Init(namespace string) {
	// Set up conversation history
	l.Conversations = make(map[string][]*botMaker.RenderContext)
	// Get the system config (API keys and Pinecone endpoint)
	cfg := botMaker.NewConfigFromEnv()
	l.config = cfg

	// Set up the OAI API client
	l.oai = botMaker.NewOAIClient(cfg.OpenAPIKey)

	// Get the tuning for the bot, we'll use some defaults
	l.settings = botMaker.NewBotSettings()

	// We set the ID for the bot as this will be used when querying
	// pinecone for context embeddings specifically for this bot -
	// use different IDs for difference PC namespaces to create
	// different context-flavours for bots
	l.settings.ID = namespace
	l.settings.Model = openai.GPT3Dot5Turbo
	l.settings.Temp = 0.3
	//l.settings.TopP = 0.4
	l.settings.MaxTokens = 4096 // need to set this for 3.5 turbo

	// If adding context (additional data outside of GPTs training data), y
	// you can attach a memory store to query
	l.settings.Memory = &botMaker.Pinecone{
		APIEndpoint: cfg.PineconeEndpoint,
		APIKey:      cfg.PineconeKey,
	}

}

func (l *LurchBot) Learn(history []*botMaker.RenderContext, with string) (int, error) {
	// Create some storage
	pc := &botMaker.Pinecone{
		APIEndpoint: l.config.PineconeEndpoint,
		APIKey:      l.config.PineconeKey,
		UUID:        l.settings.ID,
	}

	brain := botMaker.Learn{
		Model:      openai.GPT3Dot5Turbo,
		TokenLimit: 8191,
		ChunkSize:  20,
		Memory:     pc,
		Client:     l.oai,
	}

	tplStr := `
{{range $i := .}}
{{$i.Role}}: {{$i.Content}}
{{end}}
`
	var err error
	tpl := template.New("learn-tpl")
	tpl, err = tpl.Parse(tplStr)
	if err != nil {
		return 0, err
	}

	var b bytes.Buffer
	err = tpl.Execute(&b, history)
	if err != nil {
		return 0, err
	}

	count, err := brain.Learn(b.String(), fmt.Sprintf("conversation with %v", with))
	if err != nil {
		return 0, err
	}

	fmt.Println(b.String())
	fmt.Printf("embeddings: %v\n", count)

	return count, nil
}

func (l *LurchBot) Chat(with, message string) (string, error) {
	// Remember the response from the user
	oldBody := message
	l.Conversations[with] = append(l.Conversations[with],
		&botMaker.RenderContext{
			Role:    openai.ChatMessageRoleUser,
			Content: oldBody,
		})

	if message == "reset" {
		l.Conversations[with] = make([]*botMaker.RenderContext, 0)
		return "OK, I've wiped all history of our conversation", nil
	}

	if strings.HasPrefix(strings.ToLower(message), "learn this:") {
		h, ok := l.Conversations[with]
		if !ok {
			return "hmmm, I can't find who to learn from...", nil
		}
		count, err := l.Learn(h, with)
		if err != nil {
			return fmt.Sprintf("something went wrong with my brain: %v", err), err
		}

		l.Conversations[with] = make([]*botMaker.RenderContext, 0)
		return fmt.Sprintf("Saved %v items, I'll now wipe this exchange from my short term memory", count), nil

	}

	// We populate the Body with the query from the user
	prompt := botMaker.NewBotPrompt("", l.oai)
	// Set an initial instruction to the bot
	prompt.Instructions = "You are an AI chatbot that is happy and helpful, you help members of the Tyk organisation answer questions about the Tyk API Management platform and it's dependencies Redis, MongoDB and Postgres"
	prompt.Body = message

	history, ok := l.Conversations[with]
	if !ok {
		l.Conversations[with] = make([]*botMaker.RenderContext, 0)
	}

	// Populate chat history for this user
	prompt.History = history

	// make the OpenAI query, the prompt object will render the query
	// according to its template with the context embeddings pulled from Pinecone
	resp, _, err := l.oai.CallUnifiedCompletionAPI(l.settings, prompt)
	if err != nil {
		return fmt.Sprintf("I've encountered an error: %v", err), err
	}

	// save the response from the bot
	l.Conversations[with] = append(l.Conversations[with],
		&botMaker.RenderContext{
			Role:    openai.ChatMessageRoleAssistant,
			Content: resp,
		})

	return fmt.Sprintf("%s \n(contexts: %d, history: %d)",
		resp,
		len(prompt.GetContextsForLastPrompt()), len(prompt.History)), nil

}
