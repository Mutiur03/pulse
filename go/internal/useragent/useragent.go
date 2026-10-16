// Package useragent derives browser, OS and device type from a User-Agent
// header. It covers the common browsers and platforms; anything else is
// reported as empty rather than guessed.
package useragent

import "strings"

// Info is what a User-Agent tells about the client.
type Info struct {
	Browser string
	Version string // major version
	OS      string
	Device  string // Desktop, Mobile or Tablet; empty when unknown
	Bot     bool
}

func major(ua, token string) string {
	i := strings.Index(ua, token)
	if i < 0 {
		return ""
	}
	v := ua[i+len(token):]
	end := 0
	for end < len(v) && v[end] >= '0' && v[end] <= '9' {
		end++
	}
	return v[:end]
}

// Parse reads a User-Agent string.
func Parse(ua string) Info {
	var in Info
	if ua == "" {
		return in
	}
	l := strings.ToLower(ua)
	for _, b := range []string{"bot", "crawler", "spider", "headlesschrome", "lighthouse", "slurp"} {
		if strings.Contains(l, b) {
			in.Bot = true
		}
	}
	switch {
	case strings.Contains(ua, "Edg/"):
		in.Browser, in.Version = "Edge", major(ua, "Edg/")
	case strings.Contains(ua, "EdgA/"):
		in.Browser, in.Version = "Edge", major(ua, "EdgA/")
	case strings.Contains(ua, "OPR/"):
		in.Browser, in.Version = "Opera", major(ua, "OPR/")
	case strings.Contains(ua, "SamsungBrowser/"):
		in.Browser, in.Version = "Samsung Internet", major(ua, "SamsungBrowser/")
	case strings.Contains(ua, "Firefox/"):
		in.Browser, in.Version = "Firefox", major(ua, "Firefox/")
	case strings.Contains(ua, "FxiOS/"):
		in.Browser, in.Version = "Firefox", major(ua, "FxiOS/")
	case strings.Contains(ua, "CriOS/"):
		in.Browser, in.Version = "Chrome", major(ua, "CriOS/")
	case strings.Contains(ua, "HeadlessChrome/"):
		in.Browser, in.Version = "Chrome", major(ua, "HeadlessChrome/")
	case strings.Contains(ua, "Chrome/"):
		in.Browser, in.Version = "Chrome", major(ua, "Chrome/")
	case strings.Contains(ua, "Safari/") && strings.Contains(ua, "Version/"):
		in.Browser, in.Version = "Safari", major(ua, "Version/")
	}
	switch {
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPod"):
		in.OS, in.Device = "iOS", "Mobile"
	case strings.Contains(ua, "iPad"):
		in.OS, in.Device = "iOS", "Tablet"
	case strings.Contains(ua, "Android"):
		in.OS, in.Device = "Android", "Mobile"
		if !strings.Contains(ua, "Mobile") {
			in.Device = "Tablet"
		}
	case strings.Contains(ua, "CrOS"):
		in.OS, in.Device = "ChromeOS", "Desktop"
	case strings.Contains(ua, "Windows"):
		in.OS, in.Device = "Windows", "Desktop"
	case strings.Contains(ua, "Mac OS X"), strings.Contains(ua, "Macintosh"):
		in.OS, in.Device = "macOS", "Desktop"
	case strings.Contains(ua, "Linux"):
		in.OS, in.Device = "Linux", "Desktop"
	}
	return in
}
