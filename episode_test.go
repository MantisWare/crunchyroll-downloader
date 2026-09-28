package main

import (
	"strings"
	"testing"
)

func TestParsePlaybackResponseAcceptsStringError(t *testing.T) {
	t.Parallel()

	_, err := parsePlaybackResponse([]byte(`{"error":"TOO_MANY_ACTIVE_STREAMS"}`))
	if err == nil {
		t.Fatal("expected playback error")
	}
	if !strings.Contains(err.Error(), "TOO_MANY_ACTIVE_STREAMS") {
		t.Fatalf("got %v", err)
	}
	if !isRetryablePlaybackError(err) {
		t.Fatal("stream-limit errors should be retryable")
	}
}

func TestParsePlaybackResponseAcceptsNumericError(t *testing.T) {
	t.Parallel()

	_, err := parsePlaybackResponse([]byte(`{"error":403,"code":"TOO_MANY_ACTIVE_STREAMS"}`))
	if err == nil {
		t.Fatal("expected playback error")
	}
	if !strings.Contains(err.Error(), "TOO_MANY_ACTIVE_STREAMS") {
		t.Fatalf("numeric error should surface the code, got %v", err)
	}
	if !isRetryablePlaybackError(err) {
		t.Fatal("numeric stream-limit errors should be retryable")
	}
}

func TestParsePlaybackResponseAcceptsNumericErrorWithoutCode(t *testing.T) {
	t.Parallel()

	_, err := parsePlaybackResponse([]byte(`{"error":403}`))
	if err == nil {
		t.Fatal("expected playback error")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("got %v", err)
	}
	if isRetryablePlaybackError(err) {
		t.Fatal("unknown numeric errors should not be retried as stream limits")
	}
}

func TestParsePlaybackResponseReportsRateLimit4294(t *testing.T) {
	t.Parallel()

	_, err := parsePlaybackResponse([]byte(`{"error":4294,"reason":"Too many requests"}`))
	if err == nil {
		t.Fatal("expected playback error")
	}
	if !strings.Contains(err.Error(), "4294") {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "too many requests") {
		t.Fatalf("rate-limit reason missing: %v", err)
	}
	if !isRateLimitedPlaybackError(err) {
		t.Fatal("4294 should be classified as a rate limit")
	}
	if isRetryablePlaybackError(err) {
		t.Fatal("rate limits should not use the short stream-slot retry")
	}
}

func TestParsePlaybackResponseIgnoresZeroAndNullError(t *testing.T) {
	t.Parallel()

	episode, err := parsePlaybackResponse([]byte(`{"url":"https://example.com/manifest.mpd","token":"abc","error":0}`))
	if err != nil {
		t.Fatalf("error 0 should mean success: %v", err)
	}
	if episode.ManifestURL != "https://example.com/manifest.mpd" || episode.Token != "abc" {
		t.Fatalf("got %+v", episode)
	}

	episode, err = parsePlaybackResponse([]byte(`{"url":"https://example.com/manifest.mpd","token":"abc","error":null}`))
	if err != nil {
		t.Fatalf("null error should mean success: %v", err)
	}
	if episode.Token != "abc" {
		t.Fatalf("got %+v", episode)
	}
}

func TestParsePlaybackResponseAcceptsBooleanError(t *testing.T) {
	t.Parallel()

	_, err := parsePlaybackResponse([]byte(`{"error":true,"code":"TOO_MANY_ACTIVE_STREAMS","message":"too many streams"}`))
	if err == nil {
		t.Fatal("expected playback error")
	}
	if !strings.Contains(err.Error(), "TOO_MANY_ACTIVE_STREAMS") {
		t.Fatalf("got %v", err)
	}
}
