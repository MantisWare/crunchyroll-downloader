package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/unki2aut/go-mpd"
)

// onDemandMPD models the parts of a single-file ("on demand") DASH manifest that
// go-mpd does not: per-representation SegmentBase byte ranges. Crunchyroll serves
// this shape for some episodes, with ContentProtection on each Representation and
// the media stored in one contiguous file.
type onDemandMPD struct {
	Period []struct {
		AdaptationSets []struct {
			ContentType     string `xml:"contentType,attr"`
			MimeType        string `xml:"mimeType,attr"`
			Representations []struct {
				ID          string `xml:"id,attr"`
				Bandwidth   uint64 `xml:"bandwidth,attr"`
				Height      uint64 `xml:"height,attr"`
				BaseURL     string `xml:"BaseURL"`
				SegmentBase struct {
					IndexRange     string `xml:"indexRange,attr"`
					Initialization struct {
						Range string `xml:"range,attr"`
					} `xml:"Initialization"`
				} `xml:"SegmentBase"`
			} `xml:"Representation"`
		} `xml:"AdaptationSet"`
	} `xml:"Period"`
}

type onDemandRepresentation struct {
	ID         string
	Bandwidth  uint64
	Height     uint64
	BaseURL    string
	InitRange  string
	IndexRange string
}

type onDemandAdaptationSet struct {
	IsVideo         bool
	Representations []onDemandRepresentation
}

// isOnDemand reports whether the manifest is the single-file SegmentBase shape
// rather than the segmented SegmentTemplate shape.
func isOnDemand(m *mpd.MPD) bool {
	if m == nil || len(m.Period) == 0 || len(m.Period[0].AdaptationSets) == 0 {
		return false
	}
	set := m.Period[0].AdaptationSets[0]
	if set == nil {
		return false
	}
	if set.SegmentTemplate != nil {
		return false
	}
	for _, rep := range set.Representations {
		if rep.SegmentTemplate != nil {
			return false
		}
	}
	return true
}

func parseOnDemand(body []byte) ([]onDemandAdaptationSet, error) {
	var odm onDemandMPD
	if err := xml.Unmarshal(body, &odm); err != nil {
		return nil, fmt.Errorf("parse on-demand manifest: %w", err)
	}
	if len(odm.Period) == 0 {
		return nil, fmt.Errorf("manifest has no Period")
	}

	sets := make([]onDemandAdaptationSet, 0, len(odm.Period[0].AdaptationSets))
	for _, as := range odm.Period[0].AdaptationSets {
		set := onDemandAdaptationSet{
			IsVideo: as.ContentType == "video" || strings.HasPrefix(as.MimeType, "video/"),
		}
		for _, rep := range as.Representations {
			set.Representations = append(set.Representations, onDemandRepresentation{
				ID:         rep.ID,
				Bandwidth:  rep.Bandwidth,
				Height:     rep.Height,
				BaseURL:    rep.BaseURL,
				InitRange:  rep.SegmentBase.Initialization.Range,
				IndexRange: rep.SegmentBase.IndexRange,
			})
		}
		sets = append(sets, set)
	}
	return sets, nil
}

func parseByteRange(r string) (start, end int64, err error) {
	parts := strings.SplitN(r, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid byte range %q", r)
	}
	start, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid byte range %q: %w", r, err)
	}
	end, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid byte range %q: %w", r, err)
	}
	return start, end, nil
}

func selectOnDemandRepresentation(set onDemandAdaptationSet, quality string, isVideo bool) (onDemandRepresentation, bool) {
	if len(set.Representations) == 0 {
		return onDemandRepresentation{}, false
	}

	if isVideo {
		if target, err := strconv.ParseInt(strings.ReplaceAll(quality, "p", ""), 10, 64); err == nil {
			for _, rep := range set.Representations {
				if rep.Height == uint64(target) {
					return rep, true
				}
			}
		}
	} else {
		target := strings.ReplaceAll(quality, "k", "")
		for _, rep := range set.Representations {
			switch target {
			case "192":
				if rep.Bandwidth >= 192000 {
					return rep, true
				}
			case "128":
				if rep.Bandwidth >= 128000 {
					return rep, true
				}
			case "96":
				if rep.Bandwidth >= 96000 {
					return rep, true
				}
			}
		}
	}

	first := set.Representations[0]
	kind := "Audio"
	if isVideo {
		kind = "Video"
	}
	fmt.Printf("%s quality %s not found, deferring to %s\n", kind, quality, first.ID)
	return first, true
}

func onDemandRequest(url, byteRange string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(downloadContext(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	req.Header.Set("Origin", "https://static.crunchyroll.com")
	req.Header.Set("Referer", "https://static.crunchyroll.com/")
	req.Header.Set("User-Agent", userAgent)
	resp, err := mediaClient.Do(req)
	if err != nil {
		if isCancelled() {
			return nil, errCancelled
		}
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return resp, nil
}

func downloadRange(url string, start, end int64) ([]byte, error) {
	resp, err := onDemandRequest(url, fmt.Sprintf("bytes=%d-%d", start, end))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func streamRange(w io.Writer, url string, start int64) error {
	resp, err := onDemandRequest(url, fmt.Sprintf("bytes=%d-", start))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if _, err = io.Copy(w, resp.Body); err != nil {
		if isCancelled() {
			return errCancelled
		}
		return err
	}
	return nil
}

func tempMediaFile(isVideo bool) string {
	pattern := "crdl-audio-*.mp3"
	if isVideo {
		pattern = "crdl-video-*.mp4"
	}
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return ""
	}
	name := f.Name()
	f.Close()
	return name
}

func downloadOnDemandAdaptation(sets []onDemandAdaptationSet, isVideo bool, quality string) (string, error) {
	var chosen *onDemandAdaptationSet
	for i := range sets {
		if sets[i].IsVideo == isVideo {
			chosen = &sets[i]
			break
		}
	}
	if chosen == nil {
		kind := "audio"
		if isVideo {
			kind = "video"
		}
		return "", fmt.Errorf("no %s adaptation set in on-demand manifest", kind)
	}

	rep, ok := selectOnDemandRepresentation(*chosen, quality, isVideo)
	if !ok {
		kind := "audio"
		if isVideo {
			kind = "video"
		}
		return "", fmt.Errorf("no %s representation available", kind)
	}

	return downloadOnDemandParts(rep, isVideo)
}

// downloadOnDemandParts downloads a single-file representation: init range for
// key selection, then the remainder of the file streamed to disk, then decrypts.
func downloadOnDemandParts(rep onDemandRepresentation, isVideo bool) (string, error) {
	if rep.BaseURL == "" {
		return "", fmt.Errorf("representation %s has no BaseURL", rep.ID)
	}
	if rep.InitRange == "" || rep.IndexRange == "" {
		return "", fmt.Errorf("representation %s is missing SegmentBase ranges", rep.ID)
	}

	initStart, initEnd, err := parseByteRange(rep.InitRange)
	if err != nil {
		return "", fmt.Errorf("parse init range: %w", err)
	}
	initData, err := downloadRange(rep.BaseURL, initStart, initEnd)
	if err != nil {
		return "", fmt.Errorf("download init segment: %w", err)
	}

	indexStart, _, err := parseByteRange(rep.IndexRange)
	if err != nil {
		return "", fmt.Errorf("parse index range: %w", err)
	}

	filename := tempMediaFile(isVideo)
	if filename == "" {
		return "", fmt.Errorf("failed to create temp media file")
	}
	encPath := filename + ".enc"
	encFile, err := os.Create(encPath)
	if err != nil {
		return "", err
	}
	defer os.Remove(encPath)
	defer encFile.Close()

	if _, err = encFile.Write(initData); err != nil {
		return "", fmt.Errorf("writing init segment: %w", err)
	}

	kind := "audio"
	if isVideo {
		kind = "video"
	}
	fmt.Printf("Downloading %s file...\n", kind)
	if err = streamRange(encFile, rep.BaseURL, indexStart); err != nil {
		return "", err
	}
	fmt.Println("Finished downloading!")

	if _, err = encFile.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewinding %s: %w", encPath, err)
	}

	file, err := os.Create(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if err = decryptMP4(initData, encFile, keys, file); err != nil {
		return "", fmt.Errorf("decryptMP4: %w", err)
	}

	return filename, nil
}
