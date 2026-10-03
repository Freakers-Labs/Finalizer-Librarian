package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSettingsExcludePrivateLabels(t *testing.T) {
	label := "Example Private Studio Device"
	c := AppConfig{OutKey: portKey(label), InKey: portKey(label), Channel: 2, InChannel: 3}
	b, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(b, []byte(label)) {
		t.Fatal("device label persisted")
	}
	var loaded AppConfig
	if e = json.Unmarshal(b, &loaded); e != nil {
		t.Fatal(e)
	}
	if loaded != c || loaded.OutKey != portKey(label) {
		t.Fatal("settings did not round trip")
	}
	// Old settings must not bring plaintext labels into the new saved schema.
	if e = json.Unmarshal([]byte(`{"OutName":"Example Private Studio Device","InName":"Example Private Studio Device","Channel":1}`), &loaded); e != nil {
		t.Fatal(e)
	}
	b, _ = json.Marshal(loaded)
	if bytes.Contains(b, []byte(label)) {
		t.Fatal("legacy private label persisted")
	}
}
