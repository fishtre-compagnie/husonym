package console_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

var (
	tableFrame = regexp.MustCompile(
		`<div class="table-wrap" tabindex="0" role="region" aria-label="([^"]+)"><table>\n<caption class="visually-hidden">([^<]+)</caption>`)
	headerCell = regexp.MustCompile(`<th[ >]`)
	scopedCell = regexp.MustCompile(`<th scope="col"[ >]`)
)

// tablesOf checks how every table of a page is labelled, and gives their captions in the order of
// the page. A table that scrolls sideways is reached with the keyboard and named for a screen
// reader; each of its header cells says it heads a column.
func tablesOf(t *testing.T, body string) []string {
	t.Helper()
	frames := tableFrame.FindAllStringSubmatch(body, -1)
	require.Len(t, frames, strings.Count(body, "<table"), "every table is in a labelled, focusable frame and has a caption")
	require.Equal(t, strings.Count(body, `class="table-wrap"`), len(frames), "no frame without its table")
	captions := make([]string, 0, len(frames))
	for _, frame := range frames {
		require.Equal(t, frame[2], frame[1], "the frame is named as its table")
		captions = append(captions, frame[2])
	}
	cells := len(headerCell.FindAllString(body, -1))
	require.Positive(t, cells)
	require.Len(t, scopedCell.FindAllString(body, -1), cells, "every header cell has scope=col")
	return captions
}

func Test_Tables_AreCaptionedFocusableAndTheirHeaderCellsScoped(t *testing.T) {
	rejection := cpstore.SealRejection{
		LicenseID: "lic-1", CustomerID: customerID, CustomerName: "Acme",
		Day: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), Count: 4, LastAt: time.Date(2026, 10, 7, 9, 41, 0, 0, time.UTC),
	}
	pending := cpstore.PendingGroup{
		KeyFingerprint: fingerprint, Reports: 5, OldReports: 3,
		Oldest: time.Date(2026, 10, 5, 4, 0, 0, 0, time.UTC), Newest: time.Date(2026, 10, 8, 4, 0, 0, 0, time.UTC),
		InstanceIDs: []string{instanceOne},
	}
	valid := licenseSummary("lic-1", license.StateValid, today.AddDate(1, 0, 0))
	b := newBench(t)
	b.store.attention = &cpstore.Attention{
		SilentInstances: []cpstore.SilentInstance{
			{InstanceSummary: instanceSummary("lic-1", instanceOne), CustomerID: customerID, CustomerName: "Acme"},
		},
		ExpiringLicenses: []cpstore.LicenseSummary{valid},
		OldPending:       []cpstore.PendingGroup{pending},
		SealRejections:   []cpstore.SealRejection{rejection},
		SharedLicenses:   []cpstore.SharedLicense{{LicenseSummary: valid, RecentInstances: 2}},
	}
	b.store.customers = []cpstore.CustomerSummary{{ID: customerID, ExternalID: "cust-1", Name: "Acme"}}
	b.store.customer = &cpstore.CustomerDetail{
		ID: customerID, ExternalID: "cust-1", Name: "Acme",
		Licenses:  []cpstore.LicenseSummary{valid},
		Instances: []cpstore.InstanceSummary{instanceSummary("lic-1", instanceOne)},
	}
	b.store.license = &cpstore.LicenseDetail{
		LicenseSummary: valid,
		Instances:      []cpstore.InstanceSummary{instanceSummary("lic-1", instanceOne)},
		SealRejections: []cpstore.SealRejection{rejection},
		RenewalAsks:    []cpstore.RenewalAsk{{InstanceID: instanceOne, LastAskedAt: today}},
	}
	b.store.instance = instanceWithReports(nil)
	b.store.pending = []cpstore.PendingGroup{pending}

	for path, captions := range map[string][]string{
		"/": {
			"Silent instances", "Expiring licenses", "Old pending reports by key fingerprint",
			"Seal rejections by license and day", "Shared licenses",
		},
		"/customers":                        {"Customers"},
		"/customers/" + customerID.String(): {"Licenses of the customer", "Instances of the customer"},
		"/licenses/lic-1": {
			"Instances of the license", "Renewal asks of the license by instance", "Seal rejections of the license by day",
		},
		"/licenses/lic-1/instances/" + instanceOne: {"Reports of the instance"},
		"/pending": {"Pending reports by key fingerprint"},
	} {
		t.Run(path, func(t *testing.T) {
			got := b.get(path)

			require.Equal(t, http.StatusOK, got.status)
			require.Equal(t, captions, tablesOf(t, got.body))
		})
	}
}

// Under a license, every instance is of that license: the column would say the same id on every row.
func Test_License_ItsInstancesTableHasNoLicenseColumn(t *testing.T) {
	b := newBench(t)
	b.store.license = &cpstore.LicenseDetail{
		LicenseSummary: licenseSummary("lic-1", license.StateValid, today.AddDate(1, 0, 0)),
		Instances:      []cpstore.InstanceSummary{instanceSummary("lic-1", instanceOne)},
	}
	b.store.customer = &cpstore.CustomerDetail{
		ID: customerID, Name: "Acme", Instances: []cpstore.InstanceSummary{instanceSummary("lic-1", instanceOne)},
	}

	underLicense := section(t, b.get("/licenses/lic-1").body, "instances")
	underCustomer := section(t, b.get("/customers/"+customerID.String()).body, "instances")

	require.Contains(t, underLicense, `<th scope="col">Instance</th><th scope="col">Version</th>`)
	require.NotContains(t, underLicense, `href="/licenses/lic-1"`)
	require.Contains(t, row(t, underLicense, instanceOne), "v0.3.0")
	require.Contains(t, underCustomer, `<th scope="col">Instance</th><th scope="col">License</th>`)
	require.Contains(t, underCustomer, `href="/licenses/lic-1"`)
}

func Test_Stylesheet_HidesACaptionFromTheEyeOnlyAndShowsTheFocusOfAFrame(t *testing.T) {
	b := newBench(t)

	css := b.get("/static/console.css").body

	hidden := between(t, css, ".visually-hidden {", "}")
	require.Contains(t, hidden, "position: absolute")
	require.NotContains(t, hidden, "display: none", "a caption that is not displayed is not read either")
	require.Contains(t, css, ".table-wrap:focus-visible {")
	require.Contains(t, between(t, css, "\n\n.fingerprint {", "}"), "overflow-wrap: anywhere")
}
