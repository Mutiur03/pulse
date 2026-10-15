package grouping

import "testing"

const v8a = `TypeError: Cannot read properties of undefined (reading 'id')
    at pay (https://app.example.com/assets/checkout.3f2a1b9c.js:10:5)
    at onClick (https://app.example.com/assets/checkout.3f2a1b9c.js:52:11)
    at HTMLButtonElement.<anonymous> (https://app.example.com/node_modules/react-dom/index.js:1:1)`

// Same bug after a redeploy: new bundle hash, different line numbers and host.
const v8b = `TypeError: Cannot read properties of undefined (reading 'id')
    at pay (https://cdn.example.net/assets/checkout.99aa77bb.js:14:9)
    at onClick (https://cdn.example.net/assets/checkout.99aa77bb.js:60:2)`

func TestV8FramesAndRedeployStability(t *testing.T) {
	a, b := Group("TypeError", "x", v8a), Group("TypeError", "y", v8b)
	if a.Fingerprint != b.Fingerprint {
		t.Fatalf("redeploy changed fingerprint:\n%+v\n%+v", a.Frames, b.Frames)
	}
	if a.Culprit != "pay (assets/checkout.js)" {
		t.Errorf("culprit %q", a.Culprit)
	}
	if a.Frames[2].InApp {
		t.Error("node_modules frame must not be in-app")
	}
}

func TestDifferentTopFrameDiffers(t *testing.T) {
	other := `TypeError: x
    at refund (https://app.example.com/assets/checkout.js:3:1)`
	if Group("TypeError", "", v8a).Fingerprint == Group("TypeError", "", other).Fingerprint {
		t.Fatal("different functions should not group")
	}
	if Group("TypeError", "", v8a).Fingerprint == Group("RangeError", "", v8a).Fingerprint {
		t.Fatal("different types should not group")
	}
}

func TestGeckoFrames(t *testing.T) {
	f := ParseStack("pay@https://app.example.com/checkout.js:10:5\nonClick@https://app.example.com/checkout.js:52:11")
	if len(f) != 2 || f[0].Function != "pay" || f[0].File != "checkout.js" {
		t.Fatalf("%+v", f)
	}
}

func TestGoStack(t *testing.T) {
	stack := `goroutine 7 [running]:
main.(*server).pay(0xc000010000, {0x7a1b20, 0xc0000a2000})
	/app/examples/go-demo-api/main.go:88 +0x1d
net/http.HandlerFunc.ServeHTTP(0xc000123, {0x7a1b20, 0xc0000a2000}, 0xc000142000)
	/usr/local/go/src/net/http/server.go:2166 +0x29`
	f := ParseStack(stack)
	if len(f) != 2 || f[0].Function != "main.(*server).pay" || !f[0].InApp || f[1].InApp {
		t.Fatalf("%+v", f)
	}
	if f[0].File != "go-demo-api/main.go" {
		t.Errorf("file %q", f[0].File)
	}
}

func TestPythonStackIsInnermostFirst(t *testing.T) {
	stack := `Traceback (most recent call last):
  File "/srv/app/views.py", line 10, in checkout
    pay(order)
  File "/srv/app/billing.py", line 42, in pay
    raise ValueError("bad")
ValueError: bad`
	f := ParseStack(stack)
	if len(f) != 2 || f[0].Function != "pay" || f[0].File != "app/billing.py" {
		t.Fatalf("%+v", f)
	}
}

func TestJavaStack(t *testing.T) {
	f := ParseStack("java.lang.IllegalStateException: no\n\tat com.acme.Billing.pay(Billing.java:42)\n\tat java.base/java.lang.Thread.run(Thread.java:833)")
	if len(f) < 1 || f[0].Function != "com.acme.Billing.pay" || !f[0].InApp {
		t.Fatalf("%+v", f)
	}
}

func TestNormalizeMessage(t *testing.T) {
	cases := map[string]string{
		"order 12345 not found":                                    "order <n> not found",
		"user 3fa85f64-5717-4562-b3fc-2c963f66afa6 missing":        "user <uuid> missing",
		"bad pointer 0xc000123abc":                                 "bad pointer <hex>",
		"trace 4bf92f3577b34da6a3ce929d0e0e4736 failed":            "trace <hex> failed",
		"no account for me@example.com":                            "no account for <email>",
		"GET https://api.example.com/v1/orders/9?x=1 returned 502": "GET <url> returned <n>",
		`duplicate key value "abc" violates   unique constraint`:   "duplicate key value <str> violates unique constraint",
		"timeout after 3.5s\nsecond line":                          "timeout after <n>s",
	}
	for in, want := range cases {
		if got := NormalizeMessage(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestMessageFallbackWhenNoStack(t *testing.T) {
	a := Group("DBError", "connection 17 to 10.0.0.4:5432 refused", "")
	b := Group("DBError", "connection 92 to 10.0.0.9:5432 refused", "")
	c := Group("DBError", "permission denied", "")
	if a.Fingerprint != b.Fingerprint || a.Fingerprint == c.Fingerprint {
		t.Fatal("message fallback grouping wrong")
	}
	if a.Title != "DBError: connection 17 to 10.0.0.4:5432 refused" {
		t.Errorf("title %q", a.Title)
	}
}
