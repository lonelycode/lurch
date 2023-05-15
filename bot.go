package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/lonelycode/botMaker"
	"github.com/pkoukk/tiktoken-go"
	"github.com/sashabaranov/go-openai"
	"github.com/slack-go/slack/socketmode"
	"jaytaylor.com/html2text"
)

type LurchBot struct {
	Conversations      map[string]*RollingWindow
	SlackClient        *socketmode.Client
	ConversationWindow int
	settings           *botMaker.BotSettings
	oai                *botMaker.OAIClient
	config             *botMaker.Config
	help               string
	promptTemplate     string
	instructions       string
}

var SummaryTPL string = `{{.Instructions}}
user: Summarize the following content:
{{.Body}}`

func (l *LurchBot) Init(settings *Settings) {
	// Set up conversation history
	l.Conversations = make(map[string]*RollingWindow)
	l.ConversationWindow = settings.ConversationWindow
	if l.ConversationWindow == 0 {
		l.ConversationWindow = 5
	}

	// Get the system config (API keys and Pinecone endpoint)
	cfg := botMaker.NewConfigFromEnv()
	l.config = cfg

	// Set up the OAI API client
	l.oai = botMaker.NewOAIClient(cfg.OpenAPIKey)

	// Get the tuning for the bot, we'll use some defaults
	l.settings = &settings.Bot
	l.help = settings.Help

	// If adding context (additional data outside GPTs training data),
	// you can attach a memory store to query, only do so if a namespace is present
	if l.settings.ID != "" {
		l.settings.Memory = &botMaker.Pinecone{
			APIEndpoint: cfg.PineconeEndpoint,
			APIKey:      cfg.PineconeKey,
		}
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

func DownloadHTMLFromWebsite(url string) ([]byte, error) {
	// Create a new http request with the user-agent header set to Firefox
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:58.0) Gecko/20100101 Firefox/58.0")

	// Send the request and get the response
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check if status code is not 200 OK
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("response status code is not 200 OK")
	}

	// Read the body of the response and return it
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func ConvertHTMLToText(html string) (string, error) {
	text, err := html2text.FromString(html, html2text.Options{TextOnly: true})
	if err != nil {
		return "", err
	}

	return text, nil
}

func (l *LurchBot) Summarize(data string) (string, error) {
	// We populate the Body with the query from the user
	prompt := botMaker.NewBotPrompt(SummaryTPL, l.oai)
	// Set an initial instruction to the bot
	prompt.Instructions = "you are an AI copywriting assistant, you help summarize the following content into a maximum of 500 words."
	prompt.Body = data

	noMemSettings, _ := getSettings(os.Args[1])
	noMemSettings.Bot.Memory = nil

	// make the OpenAI query, the prompt object will render the query
	// according to its template with the context embeddings pulled from Pinecone
	resp, _, err := l.oai.CallUnifiedCompletionAPI(&noMemSettings.Bot, prompt)
	if err != nil {
		return fmt.Sprintf("I've encountered an error: %v", err), err
	}

	return resp, nil
}

func extractHyperlink(text string) (string, bool) {
	// Define a regular expression that matches hyperlinks
	re := regexp.MustCompile(`\bhttps?://\S+\b`)

	// Find the first hyperlink in the text
	hyperlink := re.FindString(text)

	// Check if a hyperlink was found
	if hyperlink == "" {
		return "", false
	}

	return hyperlink, true

}

var SummarizerTokenCutoff = 4000

func CanSummarize(text string, model string) bool {
	tke, err := tiktoken.EncodingForModel(model)
	if err != nil {
		log.Println(err)
		return false
	}

	tkn := tke.Encode(text, nil, nil)
	if len(tkn) > SummarizerTokenCutoff {
		return false
	}

	return true
}

func (l *LurchBot) Expand(message string) (string, string) {
	link, hasLink := extractHyperlink(message)
	if !hasLink {
		return "", ""
	}

	data, err := DownloadHTMLFromWebsite(link)
	if err != nil {
		log.Println(err)
		return "", ""
	}

	text, err := ConvertHTMLToText(string(data))
	if err != nil {
		log.Println(err)
		return "", ""
	}

	// compress that sh*t
	text = strings.ReplaceAll(text, "\n\n", "")
	text = strings.ReplaceAll(text, "\n", "")
	text = strings.ReplaceAll(text, "  ", " ")

	sumText := text
	if !CanSummarize(text, l.settings.Model) {
		log.Println("text too long to summarize, trying shorter version")
		for !CanSummarize(sumText, l.settings.Model) {
			rem := float32(len(sumText)) * 0.7
			if rem < 1 {
				log.Println("[expand] page too large to summarize")
				return link, "The website content was too large to process, tell the user that you couldn't summarize the page"
			}
			sumText = sumText[:int(rem)]
		}
	}

	summary, err := l.Summarize(sumText)
	if err != nil {
		log.Println(err)
		return link, fmt.Sprintf("couldn't summarize page: %v", err)
	}

	return link, summary
}

func (l *LurchBot) Chat(with, message string) (string, error) {
	// Remember the response from the user
	oldBody := message

	_, ok := l.Conversations[with]
	if !ok {
		l.Conversations[with] = NewRollingWindow(l.ConversationWindow)
	}

	l.Conversations[with].Append(&botMaker.RenderContext{
		Role:    openai.ChatMessageRoleUser,
		Content: oldBody,
	})

	if message == "reset" {
		l.Conversations[with] = NewRollingWindow(l.ConversationWindow)
		return "OK, I've wiped all history of our conversation", nil
	}

	if message == "help" {
		helpMsg := l.help
		if strings.HasPrefix(l.help, "file://") {
			f := strings.ReplaceAll(l.help, "file://", "")
			dat, err := os.ReadFile(f)
			if err != nil {
				return fmt.Sprint("hmm, I can't find my help response!"), nil
			}
			return string(dat), nil
		} else if l.help == "" {
			return "I am a but a simple chat-bot, here to serve.", nil
		}

		return helpMsg, nil

	}

	if strings.HasPrefix(strings.ToLower(message), "learn this:") {
		h, ok := l.Conversations[with]
		if !ok {
			return "hmmm, I can't find who to learn from...", nil
		}
		count, err := l.Learn(h.Iterate(), with)
		if err != nil {
			return fmt.Sprintf("something went wrong with my brain: %v", err), err
		}

		l.Conversations[with] = NewRollingWindow(l.ConversationWindow)
		return fmt.Sprintf("Saved %v items, I'll now wipe this exchange from my short term memory", count), nil

	}

	// pre-process the message
	link, extraContext := l.Expand(message)
	expanded := message
	if link != "" {
		if extraContext != "" {
			expanded = fmt.Sprintf(
				"%s\nThe link mentioned earlier contains the following content:\n%s\n", message, extraContext)
		}
	}

	// We populate the Body with the query from the user
	prompt := botMaker.NewBotPrompt(l.promptTemplate, l.oai)
	// Set an initial instruction to the bot
	prompt.Instructions = l.instructions
	prompt.Body = expanded

	history, ok := l.Conversations[with]
	if !ok {
		l.Conversations[with] = NewRollingWindow(l.ConversationWindow)
	}

	// Populate chat history for this user
	prompt.History = history.Iterate()

	// make the OpenAI query, the prompt object will render the query
	// according to its template with the context embeddings pulled from Pinecone
	resp, _, err := l.oai.CallUnifiedCompletionAPI(l.settings, prompt)
	if err != nil {
		return fmt.Sprintf("I've encountered an error: %v", err), err
	}

	// save the response from the bot
	l.Conversations[with].Append(
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

type RollingWindow struct {
	size    int
	current int
	buffer  []*botMaker.RenderContext
}

func NewRollingWindow(size int) *RollingWindow {
	return &RollingWindow{
		size:    size,
		current: 0,
		buffer:  make([]*botMaker.RenderContext, size),
	}
}

func (rw *RollingWindow) Append(obj *botMaker.RenderContext) {
	rw.buffer[rw.current] = obj
	rw.current = (rw.current + 1) % rw.size
}

func (rw *RollingWindow) Iterate() []*botMaker.RenderContext {
	result := make([]*botMaker.RenderContext, 0, rw.size)
	for i := 0; i < rw.size; i++ {
		index := (rw.current + i) % rw.size
		if rw.buffer[index] != nil {
			result = append(result, rw.buffer[index])
		}
	}
	return result
}
