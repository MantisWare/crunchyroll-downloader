package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Eyevinn/mp4ff/mp4"
	widevine "github.com/iyear/gowidevine"
	"github.com/iyear/gowidevine/widevinepb"
	"github.com/unki2aut/go-mpd"
	"google.golang.org/protobuf/proto"
)

var keys []*widevine.Key

// widevineSystemID is the Widevine DRM system ID used in PSSH boxes.
var widevineSystemID = mp4.UUID{0xed, 0xef, 0x8b, 0xa9, 0x79, 0xd6, 0x4a, 0xce, 0xa3, 0xc8, 0x27, 0xdc, 0xd5, 0x1d, 0x21, 0xed}

const widevineSchemeSuffix = "edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"

var rawPsshPattern = regexp.MustCompile(`(?is)<(?:[\w]+:)?pssh[^>]*>([^<]+)</(?:[\w]+:)?pssh>`)

// psshFromProtections returns the Widevine PSSH from ContentProtection elements,
// falling back to the first PSSH if none is explicitly Widevine. Crunchyroll also
// lists PlayReady alongside Widevine, so the first PSSH is not always correct.
func psshFromProtections(protections []mpd.Descriptor) *string {
	var fallback *string
	for _, contentProtection := range protections {
		if contentProtection.CencPSSH == nil {
			continue
		}
		if contentProtection.SchemeIDURI != nil && strings.Contains(strings.ToLower(*contentProtection.SchemeIDURI), widevineSchemeSuffix) {
			return contentProtection.CencPSSH
		}
		if fallback == nil {
			fallback = contentProtection.CencPSSH
		}
	}
	return fallback
}

func kidFromProtections(protections []mpd.Descriptor) *string {
	for _, contentProtection := range protections {
		if contentProtection.CencDefaultKeyId != nil && *contentProtection.CencDefaultKeyId != "" {
			return contentProtection.CencDefaultKeyId
		}
	}
	return nil
}

func findDefaultKID(manifest *mpd.MPD) *string {
	if len(manifest.Period) == 0 {
		return nil
	}
	for _, set := range manifest.Period[0].AdaptationSets {
		if set == nil {
			continue
		}
		if kid := kidFromProtections(set.ContentProtections); kid != nil {
			return kid
		}
		for _, representation := range set.Representations {
			if kid := kidFromProtections(representation.ContentProtections); kid != nil {
				return kid
			}
		}
	}
	return nil
}

func parseKIDBytes(kid string) ([]byte, error) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(kid), "-", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	if len(cleaned) != 32 {
		return nil, fmt.Errorf("invalid KID length: %q", kid)
	}
	return hex.DecodeString(cleaned)
}

func encodePsshBox(box *mp4.PsshBox) (*string, error) {
	var buf bytes.Buffer
	if err := box.Encode(&buf); err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	return &encoded, nil
}

func psshFromDefaultKID(manifest *mpd.MPD) *string {
	kid := findDefaultKID(manifest)
	if kid == nil {
		return nil
	}
	kidBytes, err := parseKIDBytes(*kid)
	if err != nil {
		return nil
	}

	payload, err := proto.Marshal(&widevinepb.WidevinePsshData{
		KeyIds: [][]byte{kidBytes},
	})
	if err != nil {
		return nil
	}

	encoded, err := encodePsshBox(&mp4.PsshBox{
		Version:  0,
		SystemID: widevineSystemID,
		Data:     payload,
	})
	if err != nil {
		return nil
	}
	return encoded
}

func psshFromRawXML(raw []byte) *string {
	matches := rawPsshPattern.FindAllSubmatch(raw, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		value := strings.TrimSpace(string(match[1]))
		if value == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			continue
		}
		normalized, err := toWidevinePssh(decoded)
		if err != nil {
			// Still usable if it already decodes as a PSSH box later.
			return &value
		}
		encoded := base64.StdEncoding.EncodeToString(normalized)
		return &encoded
	}
	return nil
}

// getPssh finds the PSSH in the MPD. Crunchyroll uses two shapes: ContentProtection
// on the AdaptationSet, or on each Representation. Prefer the Widevine PSSH.
func getPssh(manifest *mpd.MPD, raw []byte) *string {
	if manifest != nil && len(manifest.Period) > 0 {
		for _, set := range manifest.Period[0].AdaptationSets {
			if set == nil {
				continue
			}
			if pssh := psshFromProtections(set.ContentProtections); pssh != nil {
				return pssh
			}
			for _, representation := range set.Representations {
				if pssh := psshFromProtections(representation.ContentProtections); pssh != nil {
					return pssh
				}
			}
		}
	}

	if pssh := psshFromRawXML(raw); pssh != nil {
		return pssh
	}
	if manifest != nil {
		return psshFromDefaultKID(manifest)
	}
	return nil
}

func extractWidevinePsshFromInit(initData []byte) *string {
	parsed, err := mp4.DecodeFile(bytes.NewReader(initData))
	if err != nil || parsed.Init == nil || parsed.Init.Moov == nil {
		return nil
	}

	var fallback *string
	for _, psshBox := range parsed.Init.Moov.Psshs {
		encoded, err := encodePsshBox(psshBox)
		if err != nil {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(*encoded)
		if err != nil {
			continue
		}
		normalized, err := toWidevinePssh(decoded)
		if err != nil {
			if fallback == nil {
				fallback = encoded
			}
			continue
		}
		normalizedEncoded := base64.StdEncoding.EncodeToString(normalized)
		if bytes.Equal(psshBox.SystemID, widevineSystemID) {
			return &normalizedEncoded
		}
		if fallback == nil {
			fallback = &normalizedEncoded
		}
	}
	return fallback
}

// toWidevinePssh rewrites a PSSH box so its system ID is Widevine's. Crunchyroll
// may serve Widevine PSSH data inside a box tagged with a PlayReady/common
// system ID, which gowidevine's NewPSSH rejects.
func toWidevinePssh(pssh []byte) ([]byte, error) {
	box, err := mp4.DecodeBox(0, bytes.NewReader(pssh))
	if err != nil {
		return nil, fmt.Errorf("decode PSSH box: %w", err)
	}
	psshBox, ok := box.(*mp4.PsshBox)
	if !ok {
		return nil, fmt.Errorf("box is a %s instead of a PSSH", box.Type())
	}
	if bytes.Equal(psshBox.SystemID, widevineSystemID) {
		return pssh, nil
	}

	psshBox.SystemID = widevineSystemID
	var buf bytes.Buffer
	if err := psshBox.Encode(&buf); err != nil {
		return nil, fmt.Errorf("encode PSSH box: %w", err)
	}
	return buf.Bytes(), nil
}

type CrunchyrollWidevineLicenseResponse struct {
	License string `json:"license"`
}

func sendChallenge(contentId, videoToken string, challenge []byte) ([]byte, error) {
	// Passed unwrapped so net/http can populate GetBody, which lets DoRequest
	// replay the challenge if the token needs refreshing mid-flight.
	req, err := http.NewRequest(http.MethodPost, "https://www.crunchyroll.com/license/v1/license/widevine", bytes.NewReader(challenge))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Cr-Content-Id", contentId)
	req.Header.Set("X-Cr-Video-Token", videoToken)
	req.Header.Set("Authorization", "Bearer "+currentToken())
	req.Header.Set("Origin", "https://static.crunchyroll.com")
	req.Header.Set("Referer", "https://static.crunchyroll.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0")
	resp, err := DoRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Parse JSON response
	res, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result CrunchyrollWidevineLicenseResponse
	if err = json.Unmarshal(res, &result); err != nil {
		return nil, err
	}

	decoded, err := base64.StdEncoding.DecodeString(result.License)
	if err != nil {
		return nil, err
	}

	return decoded, nil
}

func getWidevineDevice() (*widevine.Device, error) {
	execPath, err := os.Executable()
	if err != nil {
		execPath = ""
	}
	if execPath != "" {
		resolved, resolveErr := filepath.EvalSymlinks(execPath)
		if resolveErr == nil {
			execPath = resolved
		}
	}

	searchDirs := []string{"."}
	cwd, _ := os.Getwd()

	if execPath != "" {
		execDir := filepath.Dir(execPath)
		searchDirs = append(searchDirs, "assets", execDir, filepath.Join(execDir, "assets"))

		// Also add absolute paths relative to cwd in case "." resolves differently
		if cwd != "" {
			searchDirs = append(searchDirs, filepath.Join(cwd, "assets"))
		}
	}

	for _, dir := range searchDirs {
		absDir, _ := filepath.Abs(dir)
		files, readErr := os.ReadDir(dir)
		if readErr != nil {
			continue
		}

		for _, file := range files {
			if strings.HasSuffix(file.Name(), ".wvd") {
				wvdPath := filepath.Join(absDir, file.Name())
				fmt.Printf("Found WVD file: %s\n", wvdPath)

				wvd, openErr := os.Open(wvdPath)
				if openErr != nil {
					return nil, fmt.Errorf("opening WVD file %s: %w", wvdPath, openErr)
				}

				device, devErr := widevine.NewDevice(widevine.FromWVD(io.NopCloser(wvd)))
				if devErr != nil {
					return nil, fmt.Errorf("parsing WVD file %s: %w", wvdPath, devErr)
				}
				return device, nil
			}
		}
	}

	for _, dir := range searchDirs {
		clientIDPath := filepath.Join(dir, "client_id.bin")
		privateKeyPath := filepath.Join(dir, "private_key.pem")

		clientID, errC := os.ReadFile(clientIDPath)
		privateKey, errK := os.ReadFile(privateKeyPath)

		if errC == nil && errK == nil && len(clientID) > 0 && len(privateKey) > 0 {
			return widevine.NewDevice(widevine.FromRaw(clientID, privateKey))
		}
	}

	searchedPaths := make([]string, 0, len(searchDirs))
	for _, dir := range searchDirs {
		abs, _ := filepath.Abs(dir)
		searchedPaths = append(searchedPaths, abs)
	}
	return nil, fmt.Errorf("no WVD file found. Searched directories:\n  %s", strings.Join(searchedPaths, "\n  "))
}

func getLicense(psshData, contentId, videoToken string) error {
	device, err := getWidevineDevice()
	if err != nil {
		return fmt.Errorf("widevine device: %w", err)
	}
	if device == nil {
		return errors.New("no widevine device provided. You either need:\n- a \".wvd\" file,\n- or \"client_id.bin\" and \"private_key.pem\" files.\nPlace them in the current directory or assets/ folder.\n")
	}
	cdm := widevine.NewCDM(device)
	decodedPssh, err := base64.StdEncoding.DecodeString(psshData)
	if err != nil {
		return err
	}
	normalizedPssh, err := toWidevinePssh(decodedPssh)
	if err != nil {
		return err
	}
	pssh, err := widevine.NewPSSH(normalizedPssh)
	if err != nil {
		return err
	}

	challenge, parseLicense, err := cdm.GetLicenseChallenge(pssh, widevinepb.LicenseType_AUTOMATIC, false)
	if err != nil {
		return err
	}
	resp, err := sendChallenge(contentId, videoToken, challenge)
	if err != nil {
		return err
	}
	keys, err = parseLicense(resp)
	if err != nil {
		return err
	}

	return nil
}

// decryptMP4 decrypts a fragmented MP4, choosing the content key whose ID matches
// the track's KID. DecryptMP4Auto uses the first content key, which can decrypt
// to garbage when the license carries a separate key per track.
func decryptMP4(initData []byte, media io.Reader, licenseKeys []*widevine.Key, output io.Writer) error {
	init, err := mp4.DecodeFile(bytes.NewReader(initData))
	if err != nil {
		return fmt.Errorf("decode MP4 init segment: %w", err)
	}
	if init.Init == nil {
		return errors.New("MP4 has no initialization segment")
	}

	decryptInfo, err := mp4.DecryptInit(init.Init)
	if err != nil {
		return fmt.Errorf("read MP4 encryption info: %w", err)
	}

	var unmatched []string
	for _, track := range decryptInfo.TrackInfos {
		if track.Sinf == nil || track.Sinf.Schi == nil || track.Sinf.Schi.Tenc == nil {
			continue
		}
		kid := track.Sinf.Schi.Tenc.DefaultKID
		for _, key := range licenseKeys {
			if key.Type == widevinepb.License_KeyContainer_CONTENT && bytes.Equal(key.ID, kid) {
				return widevine.DecryptMP4(media, key.Key, output)
			}
		}
		unmatched = append(unmatched, fmt.Sprintf("%x", kid))
	}

	if len(unmatched) > 0 {
		return fmt.Errorf("no license key found for MP4 KID(s) %s", strings.Join(unmatched, ", "))
	}
	return errors.New("MP4 has no encrypted tracks")
}
