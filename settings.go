package main

import (
	"encoding/json"
	"github.com/lonelycode/botMaker"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

type Settings struct {
	Bot          botMaker.BotSettings
	Instructions string
	Help         string
	template     string
}

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

	err = checkInstructions(botSettings)
	if err != nil {
		return nil, err
	}

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

// checkInstructions will check the `s.Instructions` property for a `file://` reference,
// if found it will read the file and fill the property with the contents of the file.
func checkInstructions(s *Settings) error {
	if strings.HasPrefix(s.Instructions, "file://") {
		filename := strings.TrimPrefix(s.Instructions, "file://")
		b, err := ioutil.ReadFile(filename)
		if err != nil {
			return err
		}
		s.Instructions = string(b)
	}
	return nil
}
