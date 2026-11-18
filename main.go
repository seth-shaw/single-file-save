package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const maxNativeMessageSize = 1 << 30

type nativeMessage struct {
	Method   string `json:"method"`
	PageData struct {
		Filename string `json:"filename"`
		URL      string `json:"url"`
		Content  string `json:"content"`
	} `json:"pageData"`
}

type archiveEntry struct {
	Date  string `json:"date"`
	URL   string `json:"url"`
	File  string `json:"file"`
	Title string `json:"title"`
}

type nativeError struct {
	Error string `json:"error"`
}

func main() {
	configPath := filepath.Join(filepath.Dir(executablePath()), "sfc.yml")
	if err := handleNativeMessage(os.Stdin, os.Stdout, configPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func executablePath() string {
	path, err := os.Executable()
	if err != nil {
		return "."
	}
	return path
}

func handleNativeMessage(input io.Reader, output io.Writer, configPath string) error {
	message, err := readNativeMessage(input)
	if err == nil {
		var config archiveConfig
		config, err = loadConfig(configPath)
		if err == nil {
			err = savePage(message, config)
		}
	}
	if err != nil {
		if writeErr := writeNativeMessage(output, nativeError{Error: err.Error()}); writeErr != nil {
			return fmt.Errorf("%v (also failed to send native-messaging error: %w)", err, writeErr)
		}
		return err
	}
	return nil
}

func readNativeMessage(input io.Reader) (nativeMessage, error) {
	var message nativeMessage
	var size [4]byte
	if _, err := io.ReadFull(input, size[:]); err != nil {
		return message, fmt.Errorf("read native message length: %w", err)
	}
	length := binary.LittleEndian.Uint32(size[:])
	if length == 0 || uint64(length) > maxNativeMessageSize {
		return message, fmt.Errorf("invalid native message size: %d", length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(input, payload); err != nil {
		return message, fmt.Errorf("read native message body: %w", err)
	}
	if err := json.Unmarshal(payload, &message); err != nil {
		return message, fmt.Errorf("parse native message: %w", err)
	}
	return message, nil
}

func writeNativeMessage(output io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode native message: %w", err)
	}
	if uint64(len(payload)) > uint64(^uint32(0)) {
		return errors.New("native message response is too large")
	}
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(len(payload)))
	if _, err := output.Write(size[:]); err != nil {
		return fmt.Errorf("write native message length: %w", err)
	}
	if _, err := output.Write(payload); err != nil {
		return fmt.Errorf("write native message body: %w", err)
	}
	return nil
}

func savePage(message nativeMessage, config archiveConfig) error {
	if message.Method != "save" {
		return fmt.Errorf("unsupported method %q", message.Method)
	}
	filename := message.PageData.Filename
	if filename == "" || filename == "." || filename == ".." ||
		strings.ContainsAny(filename, `/\`) || filepath.Base(filename) != filename {
		return fmt.Errorf("invalid page filename %q", filename)
	}
	if strings.EqualFold(filename, filepath.Base(config.indexJSONL)) ||
		strings.EqualFold(filename, filepath.Base(config.indexHTML)) {
		return fmt.Errorf("page filename %q conflicts with an index file", filename)
	}
	if message.PageData.Content == "" {
		return errors.New("page content is empty")
	}

	if err := os.MkdirAll(config.archiveDir, 0755); err != nil {
		return fmt.Errorf("create archive directory: %w", err)
	}
	pagePath := filepath.Join(config.archiveDir, filename)
	if err := os.WriteFile(pagePath, []byte(message.PageData.Content), 0644); err != nil {
		return fmt.Errorf("write saved page: %w", err)
	}

	entry := archiveEntry{
		Date:  time.Now().Format(time.RFC3339),
		URL:   message.PageData.URL,
		File:  filename,
		Title: pageTitle(message.PageData.Content),
	}
	if err := appendIndex(config.indexJSONL, entry); err != nil {
		return err
	}
	if err := generateHTMLIndex(config.indexJSONL, config.indexHTML); err != nil {
		return err
	}
	return nil
}

func pageTitle(content string) string {
	document, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return ""
	}
	var findTitle func(*html.Node) string
	findTitle = func(node *html.Node) string {
		if node.Type == html.ElementNode && node.Data == "title" {
			var text strings.Builder
			var collect func(*html.Node)
			collect = func(child *html.Node) {
				if child.Type == html.TextNode {
					text.WriteString(child.Data)
				}
				for nested := child.FirstChild; nested != nil; nested = nested.NextSibling {
					collect(nested)
				}
			}
			collect(node)
			return strings.TrimSpace(text.String())
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if title := findTitle(child); title != "" {
				return title
			}
		}
		return ""
	}
	return findTitle(document)
}

func appendIndex(path string, entry archiveEntry) error {
	encoded, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode index entry: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open JSONL index: %w", err)
	}
	if _, err = file.Write(append(encoded, '\n')); err != nil {
		file.Close()
		return fmt.Errorf("append JSONL index: %w", err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("close JSONL index: %w", err)
	}
	return nil
}

var indexTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Web archive</title>
  <style>
    body { font: 16px system-ui, sans-serif; margin: 2rem auto; max-width: 72rem; padding: 0 1rem; }
    table { border-collapse: collapse; width: 100%; }
    th, td { border-bottom: 1px solid #ddd; padding: .6rem; text-align: left; vertical-align: top; }
    th { position: sticky; top: 0; background: #fff; }
    .date { white-space: nowrap; }
  </style>
</head>
<body>
  <h1>Web archive</h1>
  <table>
    <thead><tr><th>Date</th><th>Title</th><th>URL</th><th>Saved page</th></tr></thead>
    <tbody>
    {{range .}}<tr>
      <td class="date">{{.Date}}</td>
      <td>{{.Title}}</td>
      <td><a href="{{.URL}}">{{.URL}}</a></td>
      <td><a href="{{.File}}">{{.File}}</a></td>
    </tr>{{end}}
    </tbody>
  </table>
</body>
</html>
`))

func generateHTMLIndex(jsonlPath, htmlPath string) error {
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		return fmt.Errorf("read JSONL index: %w", err)
	}
	var entries []archiveEntry
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var entry archiveEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return fmt.Errorf("parse JSONL index: %w", err)
		}
		entries = append(entries, entry)
	}

	var output bytes.Buffer
	if err := indexTemplate.Execute(&output, entries); err != nil {
		return fmt.Errorf("render HTML index: %w", err)
	}
	if err := os.WriteFile(htmlPath, output.Bytes(), 0644); err != nil {
		return fmt.Errorf("write HTML index: %w", err)
	}
	return nil
}
