package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Crunchyroll content IDs vary by era/format, e.g.:
//
//	GJ0H7Q5ZJ (9), GT00378115 (10), GE00198973JAJP (14)
var contentIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{9,14}$`)

type contentKind string

const (
	kindWatch  contentKind = "watch"
	kindSeries contentKind = "series"
)

type parsedContent struct {
	Kind contentKind
	ID   string
}

func parseContentURL(raw string) (parsedContent, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.Split(raw, "/")
	if len(parts) < 5 {
		return parsedContent{}, fmt.Errorf("invalid URL format: %s", raw)
	}

	contentType := parts[3]
	contentID := parts[4]
	if !contentIDPattern.MatchString(contentID) {
		return parsedContent{}, fmt.Errorf("invalid URL format: %s", raw)
	}
	if contentType != "watch" && contentType != "series" {
		return parsedContent{}, fmt.Errorf("invalid URL (must be /watch/ or /series/): %s", raw)
	}

	return parsedContent{Kind: contentKind(contentType), ID: contentID}, nil
}
