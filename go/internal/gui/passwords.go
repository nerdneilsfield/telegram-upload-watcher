package gui

import (
	"os"
	"strings"
)

func LoadZipPasswords(values []string, filePath string) ([]string, error) {
	passwords := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			passwords = append(passwords, value)
		}
	}
	if filePath == "" {
		return passwords, nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			passwords = append(passwords, line)
		}
	}
	return passwords, nil
}
