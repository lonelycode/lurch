package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/lonelycode/botMaker"
	"github.com/sashabaranov/go-openai"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

type Settings struct {
	Bot          botMaker.BotSettings
	Instructions string
	template     string
}

var respTpl string = `
"<@{{.User}}> {{.Response}}

{{ if .Titles }}*References:*
{{ range .Titles }}> {{.}}
{{end}}{{end}}> (contexts: {{.Contexts}}, history: {{.History}})"
`

var responseTemplate *template.Template

func getSettings(dir string) (*Settings, error) {
	cfgFile := filepath.Join(dir, "lurch.json")
	promptTpl := filepath.Join(dir, "prompt.tpl")
	_, err := os.Stat(cfgFile)
	if err != nil {
		return nil, err
	}

	cfg, err := os.ReadFile(cfgFile)
	if err != nil {
		return nil, err
	}

	botSettings := &Settings{}
	err = json.Unmarshal(cfg, botSettings)
	if err != nil {
		return nil, err
	}

	// just in case
	defaults := botMaker.NewBotSettings()
	botSettings.Bot.EmbeddingModel = defaults.EmbeddingModel

	_, err = os.Stat(promptTpl)
	if err != nil {
		return nil, err
	}

	tpl, err := os.ReadFile(promptTpl)
	if err != nil {
		return nil, err
	}

	botSettings.template = string(tpl)
	return botSettings, nil
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("lurch requires at least one argument of a bot configuration, e.g. `./lurch ./bots/tyk`")
	}

	s, err := getSettings(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	responseTemplate = template.New("learn-tpl")
	responseTemplate, err = responseTemplate.Parse(respTpl)
	if err != nil {
		log.Fatal(err)
	}

	lurch := &LurchBot{}
	lurch.Init(s)
	err = StartBot(lurch)
	if err != nil {
		log.Fatal(err)
	}

}

func StartBot(lurch *LurchBot) error {
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

	IDPattern := regexp.MustCompile(`<@([A-Z0-9]+)>`)

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
						// Find all matches using the pattern
						matches := IDPattern.FindAllStringSubmatch(ev.Text, -1)
						message := ev.Text

						// Remove userID from response
						if len(matches) > 0 {
							message = strings.ReplaceAll(message, matches[0][0], "")
						}

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
		return err
	}

	return nil
}

type LurchBot struct {
	Conversations map[string][]*botMaker.RenderContext
	settings      *botMaker.BotSettings
	oai           *botMaker.OAIClient
	config        *botMaker.Config
}

func (l *LurchBot) Init(settings *Settings) {
	// Set up conversation history
	l.Conversations = make(map[string][]*botMaker.RenderContext)
	// Get the system config (API keys and Pinecone endpoint)
	cfg := botMaker.NewConfigFromEnv()
	l.config = cfg

	// Set up the OAI API client
	l.oai = botMaker.NewOAIClient(cfg.OpenAPIKey)

	// Get the tuning for the bot, we'll use some defaults
	l.settings = &settings.Bot

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

	if message == "help" {
		dat, err := os.ReadFile("help_response.md")
		if err != nil {
			return fmt.Sprint("hmm, I can't find my help response!"), nil
		}

		return string(dat), nil
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

	fullResponse, err := l.renderResponse(with, resp, prompt)
	if err != nil {
		return fmt.Sprintf("I've encountered an error: %v", err), err
	}

	return fullResponse, nil

}

func (l *LurchBot) renderResponse(with, resp string, prompt *botMaker.BotPrompt) (string, error) {
	dat := map[string]interface{}{
		"User":     with,
		"Response": resp,
		"Titles":   prompt.ContextTitles,
		"Contexts": len(prompt.GetContextsForLastPrompt()),
		"History":  len(prompt.History),
	}

	var b bytes.Buffer
	err := responseTemplate.Execute(&b, &dat)
	if err != nil {
		return "", err
	}

	return b.String(), nil
}
