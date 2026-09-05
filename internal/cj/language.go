// Copyright 2026 Jeremy Lilley (jeremy@jlilley.net)
// SPDX-License-Identifier: Apache-2.0
//
// This file includes a Go port of CJClassifier 1.0.5. See
// third_party/cjclassifier for the license, notice, and provenance.
package cj

import "strings"

// Language is one of the languages understood by CJClassifier.
//
// The numeric values are part of the model format: the model stores
// probabilities in the order zh-hans, zh-hant, ja.
type Language int

const (
	Unknown            Language = -1
	ChineseSimplified  Language = 0
	ChineseTraditional Language = 1
	Japanese           Language = 2
)

// Languages is the fixed model order used by cjclassifier.
var Languages = [...]Language{ChineseSimplified, ChineseTraditional, Japanese}

type languageInfo struct {
	isoCode  string
	isoCode3 string
	altNames []string
	name     string
}

var languageInfos = map[Language]languageInfo{
	Unknown: {
		name: "UNKNOWN",
	},
	ChineseSimplified: {
		isoCode:  "zh-hans",
		isoCode3: "zho-hans",
		name:     "CHINESE_SIMPLIFIED",
		altNames: []string{"chinese", "zh", "zh-cn", "zh-hans-cn", "zh-hans-sg"},
	},
	ChineseTraditional: {
		isoCode:  "zh-hant",
		isoCode3: "zho-hant",
		name:     "CHINESE_TRADITIONAL",
		altNames: []string{"zh-hant-hk", "zh-hk", "zh-hant-tw"},
	},
	Japanese: {
		isoCode:  "ja",
		isoCode3: "jpn",
		name:     "JAPANESE",
		altNames: []string{"jp"},
	},
}

var languageByName = func() map[string]Language {
	result := make(map[string]Language)
	for language, info := range languageInfos {
		result[strings.ToLower(info.name)] = language
		if info.isoCode != "" {
			result[info.isoCode] = language
		}
		if info.isoCode3 != "" {
			result[info.isoCode3] = language
		}
		for _, name := range info.altNames {
			result[name] = language
		}
	}
	return result
}()

// ISOCode returns the short language code used in quality-gate details.
func (language Language) ISOCode() string {
	return languageInfos[language].isoCode
}

// IsChinese reports whether language is either Chinese variant.
func (language Language) IsChinese() bool {
	return language == ChineseSimplified || language == ChineseTraditional
}

// IsJapanese reports whether language is Japanese.
func (language Language) IsJapanese() bool { return language == Japanese }

// ParseLanguage resolves an enum name, ISO code, or upstream alias. Unknown
// strings intentionally return Unknown, matching CJLanguage.from_string().
func ParseLanguage(value string) Language {
	if language, ok := languageByName[strings.ToLower(value)]; ok {
		return language
	}
	return Unknown
}
