package main

import (
	"bytes"
	"fmt"
	"github.com/lonelycode/botMaker"
	"github.com/sashabaranov/go-openai"
	"github.com/slack-go/slack/socketmode"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

type LurchBot struct {
	Conversations  map[string][]*botMaker.RenderContext
	SlackClient    *socketmode.Client
	settings       *botMaker.BotSettings
	oai            *botMaker.OAIClient
	config         *botMaker.Config
	promptTemplate string
	instructions   string
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

	l.promptTemplate = settings.template
	l.instructions = settings.Instructions

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

	// We're using sentence based learning
	count, err := brain.Learn(b.String(), fmt.Sprintf("conversation with %v", with), true)
	if err != nil {
		return 0, err
	}

	fmt.Println(b.String())
	fmt.Printf("embeddings: %v\n", count)

	err = writeToFile(filepath.Join("learn", fmt.Sprintf("conversation-with-%s", with)), b.String())
	if err != nil {
		return count, fmt.Errorf("saved to codex, but failed to save file: %v", err)
	}

	return count, nil
}

func writeToFile(filename string, text string) error {
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	defer f.Close()

	if _, err = f.Stat(); err != nil {
		return err
	}

	if _, err = f.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	if _, err = f.WriteString("===\n" + text); err != nil {
		return err
	}

	return nil
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
	prompt := botMaker.NewBotPrompt(l.promptTemplate, l.oai)
	// Set an initial instruction to the bot
	prompt.Instructions = l.instructions
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
