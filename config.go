package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type archiveConfig struct {
	archiveDir string
	indexJSONL string
	indexHTML  string
}

type configFile struct {
	ArchiveDir string `yaml:"archive_dir"`
	IndexJSONL string `yaml:"index_jsonl"`
	IndexHTML  string `yaml:"index_html"`
}

func loadConfig(path string) (archiveConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return archiveConfig{}, fmt.Errorf("read config %q: %w", path, err)
	}
	var values configFile
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&values); err != nil {
		return archiveConfig{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	if strings.TrimSpace(values.ArchiveDir) == "" {
		return archiveConfig{}, errors.New("config field archive_dir is required")
	}
	baseDir := filepath.Dir(path)
	archiveDir := values.ArchiveDir
	if !filepath.IsAbs(archiveDir) {
		archiveDir = filepath.Join(baseDir, archiveDir)
	}

	indexJSONL := values.IndexJSONL
	if indexJSONL == "" {
		indexJSONL = "index.jsonl"
	}
	indexHTML := values.IndexHTML
	if indexHTML == "" {
		indexHTML = "index.html"
	}
	if !validIndexFilename(indexJSONL) || !validIndexFilename(indexHTML) {
		return archiveConfig{}, errors.New("index_jsonl and index_html must be filenames inside archive_dir")
	}
	return archiveConfig{
		archiveDir: filepath.Clean(archiveDir),
		indexJSONL: filepath.Join(archiveDir, indexJSONL),
		indexHTML:  filepath.Join(archiveDir, indexHTML),
	}, nil
}

func validIndexFilename(name string) bool {
	return name != "" && name != "." && name != ".." &&
		!strings.ContainsAny(name, `/\`) && filepath.Base(name) == name
}
