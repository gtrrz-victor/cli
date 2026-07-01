package cli

import (
	"sort"
	"testing"

	"github.com/entireio/cli/internal/coreapi"
)

func TestDistinctActiveClusterHosts(t *testing.T) {
	t.Parallel()
	mirrors := []coreapi.Mirror{
		{ClusterHost: "aws-us-east-2.entire.io"},
		{ClusterHost: "AWS-US-EAST-2.entire.io"},                                       // dup (case-insensitive)
		{ClusterHost: "aws-eu-west-1.entire.io"},                                       // distinct
		{ClusterHost: "aws-eu-west-1.entire.io", IsArchived: coreapi.NewOptBool(true)}, // archived → excluded
		{ClusterHost: ""}, // empty → excluded
	}
	got := distinctActiveClusterHosts(mirrors)
	sort.Strings(got)
	want := []string{"aws-eu-west-1.entire.io", "aws-us-east-2.entire.io"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("distinctActiveClusterHosts = %v, want %v", got, want)
	}
}

func TestDistinctActiveClusterHosts_AllArchived(t *testing.T) {
	t.Parallel()
	mirrors := []coreapi.Mirror{
		{ClusterHost: "aws-us-east-2.entire.io", IsArchived: coreapi.NewOptBool(true)},
	}
	if got := distinctActiveClusterHosts(mirrors); len(got) != 0 {
		t.Fatalf("distinctActiveClusterHosts = %v, want empty", got)
	}
}

func TestMatchClusterByHost(t *testing.T) {
	t.Parallel()
	clusters := []coreapi.Cluster{
		{PublicUrl: "https://us.entire.io", Jurisdiction: "us", ApiUrl: coreapi.NewOptString("https://aws-us-east-2.api.entire.io")},
		{PublicUrl: "https://eu.entire.io", Jurisdiction: "eu", ApiUrl: coreapi.NewOptString("https://aws-eu-west-1.api.entire.io")},
	}

	// Match is on the public host, case-insensitive.
	cl, ok := matchClusterByHost(clusters, "EU.entire.io")
	if !ok {
		t.Fatal("expected a match for eu.entire.io")
	}
	if cl.Jurisdiction != "eu" || cl.ApiUrl.Or("") != "https://aws-eu-west-1.api.entire.io" {
		t.Fatalf("matched wrong cluster: %+v", cl)
	}

	if _, ok := matchClusterByHost(clusters, "ap.entire.io"); ok {
		t.Fatal("expected no match for unknown host")
	}
	if _, ok := matchClusterByHost(clusters, ""); ok {
		t.Fatal("expected no match for empty host")
	}
}
