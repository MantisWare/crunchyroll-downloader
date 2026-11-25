package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	certBundleOnce sync.Once
	certBundlePath string
)

func trustedCertBundle() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	certBundleOnce.Do(func() {
		certBundlePath = writeMacOSCertBundle()
	})
	return certBundlePath
}

func writeMacOSCertBundle() string {
	dir, err := configDir()
	if err != nil {
		return ""
	}
	dest := filepath.Join(dir, "certs", "macos-trust.pem")
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return ""
	}

	var body bytes.Buffer
	for _, keychain := range macOSKeychains() {
		info, statErr := os.Stat(keychain)
		if statErr != nil || info.IsDir() {
			continue
		}
		out, cmdErr := exec.Command("security", "find-certificate", "-a", "-p", keychain).Output()
		if cmdErr != nil || len(out) == 0 {
			continue
		}
		body.Write(out)
		if !bytes.HasSuffix(out, []byte("\n")) {
			body.WriteByte('\n')
		}
	}
	if !bytes.Contains(body.Bytes(), []byte("BEGIN CERTIFICATE")) {
		return ""
	}
	if err := os.WriteFile(dest, body.Bytes(), 0o600); err != nil {
		return ""
	}
	return dest
}

func macOSKeychains() []string {
	chains := []string{
		"/System/Library/Keychains/SystemRootCertificates.keychain",
		"/Library/Keychains/System.keychain",
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return chains
	}
	return append(chains,
		filepath.Join(home, "Library", "Keychains", "login.keychain-db"),
		filepath.Join(home, "Library", "Keychains", "login.keychain"),
	)
}

func ytdlpTLSArgs(bundle string) []string {
	if strings.TrimSpace(bundle) == "" {
		return nil
	}
	return []string{"--compat-options", "no-certifi"}
}

func withCertEnv(base []string, bundle string) []string {
	if strings.TrimSpace(bundle) == "" {
		return base
	}
	values := map[string]string{
		"SSL_CERT_FILE":      bundle,
		"REQUESTS_CA_BUNDLE": bundle,
		"CURL_CA_BUNDLE":     bundle,
	}
	out := make([]string, 0, len(base)+len(values))
	for _, item := range base {
		name, _, found := strings.Cut(item, "=")
		if found {
			if _, replace := values[name]; replace {
				continue
			}
		}
		out = append(out, item)
	}
	for name, value := range values {
		out = append(out, name+"="+value)
	}
	return out
}
