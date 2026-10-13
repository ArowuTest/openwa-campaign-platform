package libpqdriver

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestConnectTimeoutPreservesLibpqURIQuery(t *testing.T) {
	for _, rawQuery := range []string{
		"options=-c%20role%3Dcampaign_export_worker",
		"application_name=literal+plus&options=-c%20role%3Dcampaign_export_worker",
		"application_name=literal%2Bplus&options=-c%20role%3Dcampaign_export_worker",
		"options=-c%20statement_timeout%3D30000%20-c%20role%3Dcampaign_export_worker&application_name=encoded%20space",
	} {
		t.Run(rawQuery, func(t *testing.T) {
			dsn := "postgres://owner@example/campaign?" + rawQuery
			got := withConnectTimeout(context.Background(), dsn)
			parsed, err := url.Parse(got)
			if err != nil {
				t.Fatal(err)
			}
			if want := rawQuery + "&connect_timeout=10"; parsed.RawQuery != want {
				t.Fatalf("timeout injection altered libpq query bytes: got=%q want=%q", parsed.RawQuery, want)
			}
			if got := parsed.Query().Get("connect_timeout"); got != "10" {
				t.Fatalf("injected connect timeout=%q want10", got)
			}
		})
	}
}

func TestConnectTimeoutPreservesExplicitURIAndKeywordSettings(t *testing.T) {
	for _, dsn := range []string{
		"postgresql://owner@example/campaign?application_name=literal+plus&options=-c%20role%3Dcampaign_export_worker&connect_timeout=17",
		"postgresql://own%65r:p%2bass@example/campaign?options=-c%20role%3Dcampaign_export_worker&connect_timeout=17",
		"host=example dbname=campaign connect_timeout=17 options='-c role=campaign_export_worker'",
	} {
		if got := withConnectTimeout(context.Background(), dsn); got != dsn {
			t.Fatalf("explicit timeout connection string changed: got=%q want=%q", got, dsn)
		}
	}
	dsn := "host=example dbname=campaign"
	if got := withConnectTimeout(context.Background(), dsn); got != dsn+" connect_timeout=10" {
		t.Fatalf("keyword timeout injection=%q", got)
	}
}

func TestConnectTimeoutUsesContextDeadlineForURI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	got := withConnectTimeout(ctx, "postgres://owner@example/campaign")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if timeout := parsed.Query().Get("connect_timeout"); timeout != "3" {
		t.Fatalf("connect timeout=%q want3", timeout)
	}
	if strings.Count(parsed.RawQuery, "connect_timeout=") != 1 {
		t.Fatal("timeout injection must create one connection parameter")
	}
}

func TestConnectTimeoutPreservesEmptySettingAndAppendsEffectiveLastValue(t *testing.T) {
	dsn := "postgres://owner@example/campaign?options=-c%20role%3Dcampaign_export_worker&connect_timeout="
	got := withConnectTimeout(context.Background(), dsn)
	if want := dsn + "&connect_timeout=10"; got != want {
		t.Fatalf("empty explicit setting bytes must be retained: got=%q want=%q", got, want)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	values := parsed.Query()["connect_timeout"]
	if len(values) != 2 || values[0] != "" || values[1] != "10" {
		t.Fatalf("last timeout parameter must carry the generated limit: values=%v", values)
	}
}
