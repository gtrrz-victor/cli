package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing/protocol/packp"

	"github.com/entireio/cli/cmd/entire/cli/auth"
)

// gitHubHTTPSRe / gitHubSSHRe / gitHubBareRe parse the GitHub URL shapes
// `mirror create`/`remove` accept, mirroring the standalone entiredb CLI:
//
//	https://github.com/<owner>/<repo>(.git)
//	git@github.com:<owner>/<repo>(.git)
//	(github.com/)<owner>/<repo>
//
// owner/repo are lowercased so the synthesised /gh/<owner>/<repo> slug
// matches what the server persists.
var (
	gitHubHTTPSRe = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+?)(?:\.git)?$`)
	gitHubSSHRe   = regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+?)(?:\.git)?$`)
	gitHubBareRe  = regexp.MustCompile(`^(?:github\.com/)?([^/\s]+)/([^/\s]+?)(?:\.git)?$`)
)

func parseGitHubURL(rawURL string) (owner, repo string, err error) {
	for _, re := range []*regexp.Regexp{gitHubHTTPSRe, gitHubSSHRe, gitHubBareRe} {
		if m := re.FindStringSubmatch(rawURL); m != nil {
			return strings.ToLower(m[1]), strings.ToLower(m[2]), nil
		}
	}
	return "", "", fmt.Errorf("not a recognized GitHub URL: %s", rawURL)
}

// waitForMirrorClone blocks until the mirror at /gh/<owner>/<repo> on
// clusterHost advertises a resolvable HEAD (the initial GitHub→EntireDB
// clone has landed) or the deadline expires. It probes the data plane's
// smart-HTTP info/refs endpoint every 2s using a repo-scoped pull token,
// printing a heartbeat so a long clone doesn't look hung.
func waitForMirrorClone(ctx context.Context, out io.Writer, clusterHost, owner, repo string, timeout time.Duration) error {
	repoSlug := "/gh/" + owner + "/" + repo
	token, err := auth.RepoScopedToken(ctx, "https://"+clusterHost, repoSlug, "pull")
	if err != nil {
		return fmt.Errorf("authorize clone probe: %w", err)
	}
	checkURL := fmt.Sprintf("https://%s%s/info/refs?service=git-upload-pack", clusterHost, repoSlug)

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	if ready, _ := mirrorAdvertisesHead(ctx, checkURL, token); ready {
		fmt.Fprintln(out, "  ready to use")
		return nil
	}
	fmt.Fprint(out, "  cloning")
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(out)
			return fmt.Errorf("timed out waiting for initial clone: %w", ctx.Err())
		case <-time.After(2 * time.Second):
		}
		ready, reason := mirrorAdvertisesHead(ctx, checkURL, token)
		if ready {
			fmt.Fprintln(out, " ready")
			return nil
		}
		if reason != "" {
			fmt.Fprint(out, ".")
		}
	}
}

// mirrorAdvertisesHead fetches the smart-HTTP ref advertisement and
// reports whether HEAD resolves to a real commit. The string return is a
// short human-readable reason when it doesn't (for debugging a stuck
// clone). Auth is the repo-scoped token as HTTP basic-auth password, the
// same shape git presents over the entire:// transport.
func mirrorAdvertisesHead(ctx context.Context, checkURL, token string) (bool, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return false, fmt.Sprintf("build request: %v", err)
	}
	req.SetBasicAuth("entire-cli", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Sprintf("transport: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	// Smart-HTTP wraps the advertisement in a "# service=..." pkt-line
	// header + flush; AdvRefs.Decode expects to start at the first ref
	// line, so strip the wrapper first.
	var sr packp.SmartReply
	if err := sr.Decode(resp.Body); err != nil {
		return false, fmt.Sprintf("decode smart reply: %v", err)
	}
	var adv packp.AdvRefs
	if err := adv.Decode(resp.Body); err != nil {
		return false, fmt.Sprintf("decode advrefs: %v", err)
	}
	if _, err := adv.ResolvedHead(); err != nil {
		return false, "HEAD not yet resolvable (clone in progress)"
	}
	return true, ""
}
