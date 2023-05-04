package main

import (
	"encoding/json"
	"github.com/lonelycode/botMaker"
	"os"
	"path/filepath"
)

type Settings struct {
	Bot          botMaker.BotSettings
	Instructions string
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
