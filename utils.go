package main

import (
	"sort"
	"strings"
)

var languageNames = map[string]string{
	"ja-JP":  "Japanese",
	"en-US":  "English",
	"en-IN":  "English (India)",
	"id-ID":  "Bahasa Indonesia",
	"ms-MY":  "Bahasa Melayu",
	"ca-ES":  "Català",
	"de-DE":  "Deutsch",
	"es-419": "Español (América Latina)",
	"es-ES":  "Español (España)",
	"fr-FR":  "Français",
	"it-IT":  "Italiano",
	"pl-PL":  "Polski",
	"pt-BR":  "Português (Brasil)",
	"pt-PT":  "Português (Portugal)",
	"vi-VN":  "Tiếng Việt",
	"tr-TR":  "Türkçe",
	"ru-RU":  "Русский",
	"ar-SA":  "العربية",
	"hi-IN":  "हिंदी",
	"ta-IN":  "தமிழ்",
	"te-IN":  "తెలుగు",
	"zh-CN":  "中文 (普通话)",
	"zh-HK":  "中文 (粵語)",
	"zh-TW":  "中文 (國語)",
	"ko-KR":  "한국어",
	"th-TH":  "ไทย",
}

func sortedLanguageCodes() []string {
	codes := make([]string, 0, len(languageNames))
	for code := range languageNames {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func languageLabel(code string) string {
	name := languageNames[code]
	if name == "" {
		return code
	}
	return name + " (" + code + ")"
}

func languageCodeFromLabel(label string) string {
	for code := range languageNames {
		if languageLabel(code) == label {
			return code
		}
	}
	open := strings.LastIndexByte(label, '(')
	if open != -1 && strings.HasSuffix(label, ")") {
		return label[open+1 : len(label)-1]
	}
	return label
}
