package main

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/unki2aut/go-mpd"
)

func parseManifest(url string) (*mpd.MPD, []byte, error) {
	req, err := http.NewRequestWithContext(downloadContext(), http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("creating manifest request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+currentToken())
	req.Header.Set("User-Agent", userAgent)
	resp, err := mediaClient.Do(req)
	if err != nil {
		if isCancelled() {
			return nil, nil, errCancelled
		}
		return nil, nil, fmt.Errorf("manifest request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if isCancelled() {
			return nil, nil, errCancelled
		}
		return nil, nil, fmt.Errorf("reading manifest: %w", err)
	}
	manifest := new(mpd.MPD)
	if err := manifest.Decode(body); err != nil {
		fmt.Printf("Warning: failed to parse MPD: %v\n", err)
	}

	return manifest, body, nil
}

func getBaseUrl(set *mpd.AdaptationSet, isVideoSet bool, quality string) (*string, *string) {
	for _, representation := range set.Representations {
		if isVideoSet {
			toInt, _ := strconv.ParseInt(strings.ReplaceAll(quality, "p", ""), 10, 64)
			if *representation.Height == uint64(toInt) {
				return &representation.BaseURL[0].Value, representation.ID
			}
		} else {
			if strings.Contains(*representation.ID, "audio/") {
				if strings.Contains(*representation.ID, quality) {
					return &representation.BaseURL[0].Value, representation.ID
				}
			} else if representation.Bandwidth != nil {
				num := strings.ReplaceAll(quality, "k", "")

				// Crunchyroll MPDs are weird on the "bandwidth" value, it can be 192002 (not just 192000) on certain manifests
				if num == "192" && *representation.Bandwidth >= 192000 {
					return &representation.BaseURL[0].Value, representation.ID
				} else if num == "128" && *representation.Bandwidth >= 128000 {
					return &representation.BaseURL[0].Value, representation.ID
				} else if num == "96" && *representation.Bandwidth >= 96000 {
					return &representation.BaseURL[0].Value, representation.ID
				}
			}
		}
	}

	// Fallback for audio: pick the highest-bandwidth representation available
	if !isVideoSet {
		var bestBaseUrl *string
		var bestId *string
		var bestBandwidth uint64

		for _, representation := range set.Representations {
			bw := uint64(0)
			if representation.Bandwidth != nil {
				bw = *representation.Bandwidth
			}
			if bw >= bestBandwidth && len(representation.BaseURL) > 0 {
				bestBandwidth = bw
				bestBaseUrl = &representation.BaseURL[0].Value
				bestId = representation.ID
			}
		}
		return bestBaseUrl, bestId
	}

	return nil, nil
}

func resolveSegmentTemplate(set *mpd.AdaptationSet, representationId string) *mpd.SegmentTemplate {
	if set == nil {
		return nil
	}
	if set.SegmentTemplate != nil {
		return set.SegmentTemplate
	}
	for _, representation := range set.Representations {
		if representation.SegmentTemplate == nil {
			continue
		}
		if representation.ID != nil && *representation.ID == representationId {
			return representation.SegmentTemplate
		}
	}
	for _, representation := range set.Representations {
		if representation.SegmentTemplate != nil {
			return representation.SegmentTemplate
		}
	}
	return nil
}

func findAdaptationSets(manifest *mpd.MPD) (*mpd.AdaptationSet, *mpd.AdaptationSet) {
	var videoSet, audioSet *mpd.AdaptationSet
	if manifest == nil || len(manifest.Period) == 0 {
		return nil, nil
	}

	for _, set := range manifest.Period[0].AdaptationSets {
		mime := strings.ToLower(set.MimeType)
		contentType := ""
		if set.ContentType != nil {
			contentType = strings.ToLower(*set.ContentType)
		}

		switch {
		case videoSet == nil && (strings.Contains(mime, "video") || contentType == "video"):
			videoSet = set
		case audioSet == nil && (strings.Contains(mime, "audio") || contentType == "audio"):
			audioSet = set
		}
	}

	// Fallback to positional if mime/content type detection didn't work
	if videoSet == nil && len(manifest.Period[0].AdaptationSets) > 0 {
		videoSet = manifest.Period[0].AdaptationSets[0]
	}
	if audioSet == nil && len(manifest.Period[0].AdaptationSets) > 1 {
		audioSet = manifest.Period[0].AdaptationSets[1]
	}

	return videoSet, audioSet
}

func expandTimeline(timeline []*mpd.SegmentTimelineS, startNumber int64) []int64 {
	var result []int64
	segNum := startNumber

	for _, s := range timeline {
		repeat := int64(0)
		if s.R != nil && *s.R > 0 {
			repeat = *s.R
		}

		total := repeat + 1 // DASH rule: total segments = r + 1

		for i := int64(0); i < total; i++ {
			result = append(result, segNum)
			segNum++
		}
	}

	return result
}
