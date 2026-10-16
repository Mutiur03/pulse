package kafka

import "testing"

func TestSecurityOpts(t *testing.T) {
	cases := []struct {
		sec  Security
		n    int
		fail bool
	}{
		{Security{}, 0, false},
		{Security{TLS: true}, 1, false},
		{Security{SASLUsername: "u", SASLPassword: "p"}, 1, false},
		{Security{TLS: true, SASLUsername: "u", SASLMechanism: "SCRAM-SHA-256"}, 2, false},
		{Security{SASLUsername: "u", SASLMechanism: "PLAIN"}, 1, false},
		{Security{SASLUsername: "u", SASLMechanism: "GSSAPI"}, 0, true},
		{Security{SASLMechanism: "GSSAPI"}, 0, false},
	}
	for _, c := range cases {
		opts, err := securityOpts(c.sec)
		if (err != nil) != c.fail || len(opts) != c.n {
			t.Errorf("%+v: %d opts, err %v", c.sec, len(opts), err)
		}
	}
}
