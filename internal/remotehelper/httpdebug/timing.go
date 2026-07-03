package httpdebug

import (
	"net/http"
	"time"

	"github.com/entireio/cli/internal/remotehelper/debuglog"
)

// TimingRoundTripper logs one ENTIRE_DEBUG-gated line per request with the
// wall-clock time to response headers (bodies stream afterwards, so transfer
// time for large packfiles is NOT included — that's deliberate: the auth and
// discovery calls this instruments are small-bodied, and time-to-headers is
// the latency signal we want).
type TimingRoundTripper struct {
	Next http.RoundTripper
	// Label distinguishes the client the request rode on (e.g. "auth", "git").
	Label string
}

// RoundTrip implements http.RoundTripper.
func (t *TimingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if !debuglog.Enabled() {
		return t.Next.RoundTrip(req)
	}
	start := time.Now()
	resp, err := t.Next.RoundTrip(req)
	dur := time.Since(start)
	if err != nil {
		debuglog.Printf("timing: %s %s %s error=%v dur_ms=%d", t.Label, req.Method, req.URL.Redacted(), err, dur.Milliseconds())
		return resp, err
	}
	debuglog.Printf("timing: %s %s %s status=%d dur_ms=%d", t.Label, req.Method, req.URL.Redacted(), resp.StatusCode, dur.Milliseconds())
	return resp, nil
}
