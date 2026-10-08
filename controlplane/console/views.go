package console

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
)

// What the templates receive. Everything is made here, in plain strings and numbers, so that a
// template only lays out; and every string reaches the page through the escaping of
// html/template, whatever it holds.

const (
	// instantLayout is how an instant is shown, in UTC.
	instantLayout = "2006-01-02 15:04"
	// absent is shown in place of a value there is none of.
	absent = "—"
	// shortFingerprint is how many characters of a fingerprint a list shows.
	shortFingerprint = 12

	hoursPerDay = 24
)

// layout is what the layout of every page receives.
type layout struct {
	Title string
	// Nav is the link of the layout the page is under, empty when it is under none.
	Nav      string
	Operator string
	// Unguarded tells the console is served without its gates.
	Unguarded bool
	// Body is what the page itself receives.
	Body any
}

// link is a text that leads somewhere. Without an Href it leads nowhere and is shown as text.
type link struct {
	Text string
	Href string
}

// messageView is a page that only says something.
type messageView struct {
	Heading string
	Text    string
}

type attentionView struct {
	Tallies []tally

	Silent          []instanceRow
	SilentAfterDays int
	RecentDays      int

	Expiring     []licenseRow
	ExpiringDays int

	OldPending        []pendingRow
	OldPendingReports int
	OldPendingHours   int

	Rejections      []rejectionRow
	RejectedReports int
	RejectionDays   int

	Shared []sharedRow
}

// tally is how much there is of one thing that needs attention, and where on the page.
type tally struct {
	Label string
	Href  string
	Count int
}

type licenseRow struct {
	License   link
	Customer  link
	Plan      string
	Telemetry string
	State     string
	ExpiresAt string
	// Succeeded tells, in a word, whether another license succeeds this one.
	Succeeded string
}

type sharedRow struct {
	licenseRow
	RecentInstances int
}

type instanceRow struct {
	Instance      link
	License       link
	Customer      link
	Version       string
	InstallKind   string
	FirstSeen     string
	LastSeen      string
	LastReportDay string
}

type rejectionRow struct {
	License  link
	Customer link
	Day      string
	Count    int
	LastAt   string
}

type pendingRow struct {
	Fingerprint string
	// Short is the beginning of the fingerprint.
	Short string
	// Href leads to the row of the fingerprint on the page of the pending reports.
	Href       string
	Reports    int
	OldReports int
	Oldest     string
	Newest     string
	Instances  []string
}

type customersView struct {
	Customers  []customerRow
	RecentDays int
}

type customerRow struct {
	Customer        link
	ExternalID      string
	Licenses        int
	RecentInstances int
	NearestExpiry   string
}

type customerView struct {
	Name       string
	ID         string
	ExternalID string
	Note       string
	CreatedAt  string
	UpdatedAt  string
	Licenses   []licenseRow
	Instances  []instanceRow
}

type licenseView struct {
	ID        string
	Customer  link
	Plan      string
	State     string
	Telemetry string
	// KeyTelemetry is what the key says of its telemetry when that is not the word of Telemetry.
	KeyTelemetry string
	IssuedAt     string
	ExpiresAt    string
	GraceDays    string
	// Features is nil when the key allows all of them.
	Features    []string
	AllFeatures bool
	// Limits is nil when the key carries none.
	Limits                []fact
	KeyFingerprint        string
	Kid                   string
	SigningKeyFingerprint string
	Origin                string
	Note                  string
	CreatedAt             string
	Predecessor           *link
	Successors            []link
	Instances             []instanceRow
	Rejections            []rejectionRow
	RejectionDays         int
}

// fact is a named value.
type fact struct {
	Name  string
	Value string
}

type instanceView struct {
	ID            string
	License       link
	Customer      link
	Version       string
	InstallKind   string
	FirstSeen     string
	LastSeen      string
	LastReportDay string
	// SourceCap is the cap of the license on the sources, in words when it has none.
	SourceCap string
	Reports   []reportRow
	// Truncated tells the list holds as many reports as a list may, so that older ones may exist.
	Truncated  bool
	ReportsCap int
}

type reportRow struct {
	Day        link
	ReceivedAt string
	Conflicts  int
	// Sources is the number of sources the report tells, against the cap when there is one.
	Sources string
	// Over tells the report counts more sources than the cap.
	Over bool
}

type reportView struct {
	Day            string
	Instance       link
	License        link
	ReceivedAt     string
	Conflicts      int
	LastConflictAt string
	Document       string
	// AsReceived tells the document is not JSON and is shown as it came.
	AsReceived bool
}

type pendingView struct {
	Groups          []pendingRow
	OldPendingHours int
}

func instant(t time.Time) string {
	if t.IsZero() {
		return absent
	}
	return t.UTC().Format(instantLayout)
}

func day(t time.Time) string {
	if t.IsZero() {
		return absent
	}
	return t.UTC().Format(time.DateOnly)
}

func orAbsent(value string) string {
	if value == "" {
		return absent
	}
	return value
}

func customerLink(id uuid.UUID, name string) link {
	return link{Text: name, Href: "/customers/" + id.String()}
}

// dotSegment says whether an id, written as a segment of a path, would be taken out of it: a
// browser resolves "." and ".." before it asks, escaped or not, so that a link to such an id
// leads to another page.
func dotSegment(id string) bool {
	return id == "." || id == ".."
}

// licenseHref is the path of the page of a license, empty when no link can lead to it.
func licenseHref(id string) string {
	if dotSegment(id) {
		return ""
	}
	return "/licenses/" + url.PathEscape(id)
}

func licenseLink(id string) link {
	return link{Text: id, Href: licenseHref(id)}
}

// instanceHref is the path of the page of an instance, empty when no link can lead to it.
func instanceHref(licenseID, instanceID string) string {
	if dotSegment(licenseID) || dotSegment(instanceID) {
		return ""
	}
	return licenseHref(licenseID) + "/instances/" + url.PathEscape(instanceID)
}

// reportHref is the path of the page of a report, empty when no link can lead to it.
func reportHref(licenseID, instanceID string, day time.Time) string {
	instance := instanceHref(licenseID, instanceID)
	if instance == "" {
		return ""
	}
	return instance + "/reports/" + day.UTC().Format(time.DateOnly)
}

func instanceLink(licenseID, instanceID string) link {
	return link{Text: instanceID, Href: instanceHref(licenseID, instanceID)}
}

func newLicenseRow(l *cpstore.LicenseSummary) licenseRow {
	succeeded := "no"
	if l.HasSuccessor {
		succeeded = "yes"
	}
	return licenseRow{
		License:   licenseLink(l.ID),
		Customer:  customerLink(l.CustomerID, l.CustomerName),
		Plan:      orAbsent(l.Plan),
		Telemetry: string(l.Telemetry),
		State:     string(l.State),
		ExpiresAt: instant(l.ExpiresAt),
		Succeeded: succeeded,
	}
}

func newLicenseRows(licenses []cpstore.LicenseSummary) []licenseRow {
	rows := make([]licenseRow, 0, len(licenses))
	for i := range licenses {
		rows = append(rows, newLicenseRow(&licenses[i]))
	}
	return rows
}

// newInstanceRow leaves the customer out: only some lists have one to show.
func newInstanceRow(i *cpstore.InstanceSummary) instanceRow {
	return instanceRow{
		Instance:      instanceLink(i.LicenseID, i.InstanceID),
		License:       licenseLink(i.LicenseID),
		Version:       orAbsent(i.HusonymVersion),
		InstallKind:   orAbsent(i.InstallKind),
		FirstSeen:     instant(i.FirstSeenAt),
		LastSeen:      instant(i.LastSeenAt),
		LastReportDay: day(i.LastReportDay),
	}
}

func newInstanceRows(instances []cpstore.InstanceSummary) []instanceRow {
	rows := make([]instanceRow, 0, len(instances))
	for i := range instances {
		rows = append(rows, newInstanceRow(&instances[i]))
	}
	return rows
}

// newRejectionRows gives the rows of the rejections and how many reports they count in all.
func newRejectionRows(rejections []cpstore.SealRejection) (rows []rejectionRow, reports int) {
	rows = make([]rejectionRow, 0, len(rejections))
	for i := range rejections {
		r := &rejections[i]
		rows = append(rows, rejectionRow{
			License:  licenseLink(r.LicenseID),
			Customer: customerLink(r.CustomerID, r.CustomerName),
			Day:      day(r.Day),
			Count:    r.Count,
			LastAt:   instant(r.LastAt),
		})
		reports += r.Count
	}
	return rows, reports
}

// newPendingRows gives the rows of the groups and how many old reports they count in all.
func newPendingRows(groups []cpstore.PendingGroup) (rows []pendingRow, oldReports int) {
	rows = make([]pendingRow, 0, len(groups))
	for i := range groups {
		g := &groups[i]
		short := g.KeyFingerprint
		if len(short) > shortFingerprint {
			short = short[:shortFingerprint]
		}
		rows = append(rows, pendingRow{
			Fingerprint: g.KeyFingerprint,
			Short:       short,
			Href:        "/pending#fp-" + url.PathEscape(g.KeyFingerprint),
			Reports:     g.Reports,
			OldReports:  g.OldReports,
			Oldest:      instant(g.Oldest),
			Newest:      instant(g.Newest),
			Instances:   g.InstanceIDs,
		})
		oldReports += g.OldReports
	}
	return rows, oldReports
}

func newAttentionView(a *cpstore.Attention) *attentionView {
	view := &attentionView{
		SilentAfterDays: cpstore.SilentAfterDays,
		RecentDays:      cpstore.RecentInstanceDays,
		Expiring:        newLicenseRows(a.ExpiringLicenses),
		ExpiringDays:    int(cpstore.ExpiringWithin.Hours()) / hoursPerDay,
		OldPendingHours: int(cpstore.OldPendingAfter.Hours()),
		RejectionDays:   cpstore.SealRejectionDays,
		Silent:          make([]instanceRow, 0, len(a.SilentInstances)),
		Shared:          make([]sharedRow, 0, len(a.SharedLicenses)),
	}
	for i := range a.SilentInstances {
		silent := &a.SilentInstances[i]
		row := newInstanceRow(&silent.InstanceSummary)
		row.Customer = customerLink(silent.CustomerID, silent.CustomerName)
		view.Silent = append(view.Silent, row)
	}
	view.OldPending, view.OldPendingReports = newPendingRows(a.OldPending)
	view.Rejections, view.RejectedReports = newRejectionRows(a.SealRejections)
	for i := range a.SharedLicenses {
		shared := &a.SharedLicenses[i]
		view.Shared = append(view.Shared, sharedRow{
			licenseRow:      newLicenseRow(&shared.LicenseSummary),
			RecentInstances: shared.RecentInstances,
		})
	}
	view.Tallies = []tally{
		{Label: "Silent instances", Href: "#silent-instances", Count: len(view.Silent)},
		{Label: "Expiring licenses", Href: "#expiring-licenses", Count: len(view.Expiring)},
		{Label: "Old pending reports", Href: "#old-pending", Count: view.OldPendingReports},
		{Label: "Seal rejections", Href: "#seal-rejections", Count: view.RejectedReports},
		{Label: "Shared licenses", Href: "#shared-licenses", Count: len(view.Shared)},
	}
	return view
}

func newCustomersView(customers []cpstore.CustomerSummary) *customersView {
	view := &customersView{
		Customers:  make([]customerRow, 0, len(customers)),
		RecentDays: cpstore.RecentInstanceDays,
	}
	for i := range customers {
		c := &customers[i]
		view.Customers = append(view.Customers, customerRow{
			Customer:        customerLink(c.ID, c.Name),
			ExternalID:      c.ExternalID,
			Licenses:        c.Licenses,
			RecentInstances: c.RecentInstances,
			NearestExpiry:   instant(c.NearestExpiry),
		})
	}
	return view
}

func newCustomerView(c *cpstore.CustomerDetail) *customerView {
	return &customerView{
		Name:       c.Name,
		ID:         c.ID.String(),
		ExternalID: c.ExternalID,
		Note:       orAbsent(c.Note),
		CreatedAt:  instant(c.CreatedAt),
		UpdatedAt:  instant(c.UpdatedAt),
		Licenses:   newLicenseRows(c.Licenses),
		Instances:  newInstanceRows(c.Instances),
	}
}

func newLicenseView(l *cpstore.LicenseDetail) *licenseView {
	view := &licenseView{
		ID:                    l.ID,
		Customer:              customerLink(l.CustomerID, l.CustomerName),
		Plan:                  orAbsent(l.Plan),
		State:                 string(l.State),
		Telemetry:             string(l.Telemetry),
		IssuedAt:              instant(l.IssuedAt),
		ExpiresAt:             instant(l.ExpiresAt),
		GraceDays:             "not said",
		Features:              l.Features,
		AllFeatures:           l.Features == nil,
		Limits:                limitFacts(l.Limits),
		KeyFingerprint:        orAbsent(l.KeyFingerprint),
		Kid:                   orAbsent(l.Kid),
		SigningKeyFingerprint: orAbsent(l.SigningKeyFingerprint),
		Origin:                orAbsent(l.Origin),
		Note:                  orAbsent(l.Note),
		CreatedAt:             instant(l.CreatedAt),
		Instances:             newInstanceRows(l.Instances),
		RejectionDays:         cpstore.SealRejectionDays,
	}
	if l.StoredTelemetry != string(l.Telemetry) {
		view.KeyTelemetry = "the key does not say"
		if l.StoredTelemetry != "" {
			view.KeyTelemetry = "the key says " + strconv.Quote(l.StoredTelemetry)
		}
	}
	if l.GraceDays != nil {
		view.GraceDays = strconv.Itoa(*l.GraceDays)
	}
	if l.PredecessorID != "" {
		predecessor := licenseLink(l.PredecessorID)
		view.Predecessor = &predecessor
	}
	for _, id := range l.SuccessorIDs {
		view.Successors = append(view.Successors, licenseLink(id))
	}
	view.Rejections, _ = newRejectionRows(l.SealRejections)
	return view
}

// limitFacts names each limit as the key does. A limit the key does not set is uncapped.
func limitFacts(limits *license.Limits) []fact {
	if limits == nil {
		return nil
	}
	capOf := func(value *int) string {
		if value == nil {
			return "uncapped"
		}
		return strconv.Itoa(*value)
	}
	types := "uncapped"
	if limits.AllowedConnectionTypes != nil {
		types = orAbsent(strings.Join(limits.AllowedConnectionTypes, ", "))
	}
	return []fact{
		{Name: "max_jobs", Value: capOf(limits.MaxJobs)},
		{Name: "max_connections", Value: capOf(limits.MaxConnections)},
		{Name: "max_sources", Value: capOf(limits.MaxSources)},
		{Name: "allowed_connection_types", Value: types},
	}
}

func newInstanceView(i *cpstore.InstanceDetail) *instanceView {
	view := &instanceView{
		ID:            i.InstanceID,
		License:       licenseLink(i.LicenseID),
		Customer:      customerLink(i.CustomerID, i.CustomerName),
		Version:       orAbsent(i.HusonymVersion),
		InstallKind:   orAbsent(i.InstallKind),
		FirstSeen:     instant(i.FirstSeenAt),
		LastSeen:      instant(i.LastSeenAt),
		LastReportDay: day(i.LastReportDay),
		SourceCap:     "none",
		Reports:       make([]reportRow, 0, len(i.Reports)),
		Truncated:     len(i.Reports) >= cpstore.InstanceReportsCap,
		ReportsCap:    cpstore.InstanceReportsCap,
	}
	var sourceCap *int
	if i.Limits != nil {
		sourceCap = i.Limits.MaxSources
	}
	if sourceCap != nil {
		view.SourceCap = strconv.Itoa(*sourceCap)
	}
	for n := range i.Reports {
		report := &i.Reports[n]
		row := reportRow{
			Day:        link{Text: day(report.Day), Href: reportHref(i.LicenseID, i.InstanceID, report.Day)},
			ReceivedAt: instant(report.ReceivedAt),
			Conflicts:  report.Conflicts,
			Sources:    absent,
		}
		if report.Sources != nil {
			row.Sources = strconv.Itoa(*report.Sources)
			if sourceCap != nil {
				row.Sources += " / " + strconv.Itoa(*sourceCap)
				row.Over = *report.Sources > *sourceCap
			}
		}
		view.Reports = append(view.Reports, row)
	}
	return view
}

func newReportView(r *cpstore.StoredReport) *reportView {
	view := &reportView{
		Day:            day(r.Day),
		Instance:       instanceLink(r.LicenseID, r.InstanceID),
		License:        licenseLink(r.LicenseID),
		ReceivedAt:     instant(r.ReceivedAt),
		Conflicts:      r.Conflicts,
		LastConflictAt: instant(r.LastConflictAt),
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, r.Document, "", "  "); err != nil {
		view.Document, view.AsReceived = string(r.Document), true
		return view
	}
	view.Document = indented.String()
	return view
}

func newPendingView(groups []cpstore.PendingGroup) *pendingView {
	rows, _ := newPendingRows(groups)
	return &pendingView{Groups: rows, OldPendingHours: int(cpstore.OldPendingAfter.Hours())}
}
