package main

import (
	"crypto/sha256"
	"fmt"
)

// Only channel numbers and hashed port identifiers are persisted.
// Device labels are displayed in memory, never copied into the settings file.
type AppConfig struct {
	OutKey, InKey      string
	Channel, InChannel int
}

func portKey(name string) string {
	if name == "" {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(name)))
}
