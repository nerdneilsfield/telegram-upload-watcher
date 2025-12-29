package config

import (
	"fmt"
	"strings"

	"gopkg.in/ini.v1"
)

func NormalizeAPIURL(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}
	if !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}
	return strings.TrimRight(url, "/")
}

func LoadConfig(path string) ([]string, []string, error) {
	cfg, err := ini.Load(path)
	if err != nil {
		return nil, nil, err
	}

	apiURL := cfg.Section("Telegram").Key("api_url").MustString("https://api.telegram.org")
	apiURLs := []string{}
	for _, value := range strings.Split(apiURL, ",") {
		normalized := NormalizeAPIURL(value)
		if normalized != "" {
			apiURLs = append(apiURLs, normalized)
		}
	}

	tokens := []string{}
	for _, section := range cfg.Sections() {
		if !strings.HasPrefix(section.Name(), "Token") {
			continue
		}
		token := strings.TrimSpace(section.Key("token").String())
		if token != "" {
			tokens = append(tokens, token)
		}
	}

	return apiURLs, tokens, nil
}

func SaveConfig(path string, apiURLs []string, tokens []string) error {
	cfg := ini.Empty()
	apiURL := strings.Join(apiURLs, ",")
	if apiURL == "" {
		apiURL = "https://api.telegram.org"
	}
	cfg.Section("Telegram").Key("api_url").SetValue(apiURL)
	for idx, token := range tokens {
		if strings.TrimSpace(token) == "" {
			continue
		}
		section := cfg.Section(fmt.Sprintf("Token%d", idx+1))
		section.Key("name").SetValue(fmt.Sprintf("token-%d", idx+1))
		section.Key("id").SetValue(fmt.Sprintf("token-%d", idx+1))
		section.Key("token").SetValue(strings.TrimSpace(token))
	}
	return cfg.SaveTo(path)
}
