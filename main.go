package main

import (
	"fmt"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
	"log"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"text/template"
)

var respTpl string = `
<@{{.User}}> {{.Response}}

{{ if .Titles }}*References:*
{{ range .Titles }}> {{.}}
{{end}}{{end}}> (contexts: {{.Contexts}}, history: {{.History}})
`

var responseTemplate *template.Template
var IDPattern = regexp.MustCompile(`<@([A-Z0-9]+)>`)

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

	var gracefulStop = make(chan os.Signal)
	signal.Notify(gracefulStop, syscall.SIGTERM)
	signal.Notify(gracefulStop, syscall.SIGINT)

	lurch := &LurchBot{}
	lurch.Init(s)

	go func() {
		sig := <-gracefulStop
		fmt.Printf("caught sig: %+v\n", sig)
		fmt.Println("marking bot as offline")
		err := lurch.SlackClient.SetUserPresence("away")
		if err != nil {
			log.Println(err)
		}
		os.Exit(0)
	}()

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

	lurch.SlackClient = client

	go handleEvents(client, lurch)

	err := client.Run()
	if err != nil {
		return err
	}

	return nil
}
