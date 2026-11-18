package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleNativeMessageSavesPageAndUpdatesIndexes(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "sfc.yml")
	archiveDir := filepath.Join(dir, "archive")
	if err := os.WriteFile(configPath, []byte("archive_dir: "+archiveDir+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	request := []byte(`{"method":"save","pageData":{"filename":"sample.html","url":"https://example.test/a?x=1&y=2","content":"<!doctype html><html><head><title>A &amp; B</title></head><body>Saved</body></html>"}}`)
	input := frame(request)
	var output bytes.Buffer
	if err := handleNativeMessage(bytes.NewReader(input), &output, configPath); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("successful request wrote unexpected response: %q", output.Bytes())
	}

	savedPage, err := os.ReadFile(filepath.Join(archiveDir, "sample.html"))
	if err != nil || !bytes.Contains(savedPage, []byte("Saved")) {
		t.Fatalf("saved page missing or incorrect: %v", err)
	}
	jsonl, err := os.ReadFile(filepath.Join(archiveDir, "index.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var entry archiveEntry
	if err := json.Unmarshal(bytes.TrimSpace(jsonl), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.URL != "https://example.test/a?x=1&y=2" || entry.File != "sample.html" || entry.Title != "A & B" || entry.Date == "" {
		t.Fatalf("unexpected index entry: %+v", entry)
	}
	index, err := os.ReadFile(filepath.Join(archiveDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "A &amp; B") || !strings.Contains(string(index), "sample.html") {
		t.Fatalf("HTML index does not contain escaped entry: %s", index)
	}
}

func TestHandleNativeMessageRejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "sfc.yml")
	if err := os.WriteFile(configPath, []byte("archive_dir: archive\n"), 0600); err != nil {
		t.Fatal(err)
	}
	request := []byte(`{"method":"save","pageData":{"filename":"../outside.html","url":"https://example.test","content":"<title>bad</title>"}}`)
	var output bytes.Buffer
	err := handleNativeMessage(bytes.NewReader(frame(request)), &output, configPath)
	if err == nil {
		t.Fatal("expected invalid filename error")
	}
	if output.Len() == 0 {
		t.Fatal("expected framed native-messaging error response")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "outside.html")); !os.IsNotExist(statErr) {
		t.Fatalf("unexpected path traversal output: %v", statErr)
	}
}

func TestHandleNativeMessageRejectsIndexFilename(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "sfc.yml")
	if err := os.WriteFile(configPath, []byte("archive_dir: archive\n"), 0600); err != nil {
		t.Fatal(err)
	}
	request := []byte(`{"method":"save","pageData":{"filename":"index.html","url":"https://example.test","content":"<title>bad</title>"}}`)
	var output bytes.Buffer
	if err := handleNativeMessage(bytes.NewReader(frame(request)), &output, configPath); err == nil {
		t.Fatal("expected index collision error")
	}
	if _, err := os.Stat(filepath.Join(dir, "archive")); !os.IsNotExist(err) {
		t.Fatalf("archive unexpectedly created: %v", err)
	}
}

func frame(payload []byte) []byte {
	var result bytes.Buffer
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(len(payload)))
	result.Write(size[:])
	result.Write(payload)
	return result.Bytes()
}
