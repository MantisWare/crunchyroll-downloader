package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/iyear/gowidevine"
	"github.com/iyear/gowidevine/widevinepb"
	"github.com/unki2aut/go-mpd"
)

var keys []*widevine.Key

// getPssh finds the PSSH in the MPD manifest
func getPssh(mpd *mpd.MPD) *string {
	set := mpd.Period[0].AdaptationSets[0]
	if set == nil {
		return nil
	}

	for _, contentProtection := range set.ContentProtections {
		if contentProtection.CencPSSH != nil {
			return contentProtection.CencPSSH
		}
	}

	return nil
}

type CrunchyrollWidevineLicenseResponse struct {
	License string `json:"license"`
}

func sendChallenge(contentId, videoToken string, challenge []byte) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, "https://www.crunchyroll.com/license/v1/license/widevine", io.NopCloser(bytes.NewReader(challenge)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Cr-Content-Id", contentId)
	req.Header.Set("X-Cr-Video-Token", videoToken)
	req.Header.Set("Authorization", "Bearer "+token)
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
	execPath, _ := os.Executable()
	execDir := filepath.Dir(execPath)

	searchDirs := []string{
		".",
		"assets",
		filepath.Join(execDir, "assets"),
	}

	for _, dir := range searchDirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, file := range files {
			if strings.HasSuffix(file.Name(), ".wvd") {
				wvd, err := os.Open(filepath.Join(dir, file.Name()))
				if err != nil {
					return nil, err
				}

				return widevine.NewDevice(widevine.FromWVD(io.NopCloser(wvd)))
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

	return nil, nil
}

func getLicense(psshData, contentId, videoToken string) error {
	device, err := getWidevineDevice()
	if device == nil {
		return errors.New("no widevine device provided. You either need:\n- a \".wvd\" file,\n- or \"client_id.bin\" and \"private_key.pem\" files.\nI'm not sharing links for obvious reasons, but search \"ready to use cdms\" on Google :)\n")
	} else if err != nil {
		return err
	}
	cdm := widevine.NewCDM(device)
	decodedPssh, err := base64.StdEncoding.DecodeString(psshData)
	if err != nil {
		return err
	}
	pssh, err := widevine.NewPSSH(decodedPssh)
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
