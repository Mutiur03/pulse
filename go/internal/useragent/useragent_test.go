package useragent

import "testing"

func TestParse(t *testing.T) {
	cases := map[string]Info{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36":                         {"Chrome", "126", "Windows", "Desktop", false},
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.2592.87":       {"Edge", "126", "Windows", "Desktop", false},
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15":                      {"Safari", "17", "macOS", "Desktop", false},
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1": {"Safari", "17", "iOS", "Mobile", false},
		"Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/126.0 Mobile/15E148 Safari/604.1":           {"Chrome", "126", "iOS", "Tablet", false},
		"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0":                                                          {"Firefox", "127", "Linux", "Desktop", false},
		"Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/25.0 Chrome/121.0 Mobile Safari/537.36":  {"Samsung Internet", "25", "Android", "Mobile", false},
		"Mozilla/5.0 (Linux; Android 13; SM-X700) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36":                              {"Chrome", "126", "Android", "Tablet", false},
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/126.0.0.0 Safari/537.36":                           {"Chrome", "126", "Linux", "Desktop", true},
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)":                                                                {"", "", "", "", true},
		"Go-http-client/1.1": {},
		"":                   {},
	}
	for ua, want := range cases {
		if got := Parse(ua); got != want {
			t.Errorf("%q\n got %+v\nwant %+v", ua, got, want)
		}
	}
}
