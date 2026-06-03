package otp

import "regexp"

var patterns = []*regexp.Regexp{
	regexp.MustCompile(`\b\d{6}\b`),
	regexp.MustCompile(`\b\d{4,8}\b`),
	regexp.MustCompile(`\b[A-Z0-9]{5}\b`),
}

func Extract(parts ...string) string {
	text := ""
	for _, part := range parts {
		if part != "" {
			text += "\n" + part
		}
	}
	if text == "" {
		return ""
	}
	for _, pattern := range patterns {
		if match := pattern.FindString(text); match != "" {
			return match
		}
	}
	return ""
}
