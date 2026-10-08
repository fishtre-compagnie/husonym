// Package console is the operator console of the control plane: pages that show the customers,
// the licenses, the instances and the reports it holds, and the few acts of the operator, each of
// them a POST that is journaled: recording a customer, issuing or renewing a license, showing the
// key of one again.
package console

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fishtre-compagnie/husonym/controlplane/accessgate"
	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/issuing"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
)

//go:embed templates/*.html static/console.css
var embedded embed.FS

const (
	layoutFile     = "layout.html"
	stylesheetFile = "static/console.css"
	stylesheetPath = "/static/console.css"
	// layoutTemplate is the template every page is rendered through.
	layoutTemplate = "layout"

	pageAttention = "attention.html"
	pageCustomers = "customers.html"
	pageCustomer  = "customer.html"
	pageLicense   = "license.html"
	pageInstance  = "instance.html"
	pageReport    = "report.html"
	pagePending   = "pending.html"
	pageMessage   = "message.html"

	pageCustomerForm   = "customer-form.html"
	pageLicenseForm    = "license-form.html"
	pageLicenseConfirm = "license-confirm.html"
	pageLicenseKey     = "license-key.html"
	pageJournal        = "journal.html"

	navAttention = "attention"
	navCustomers = "customers"
	navPending   = "pending"
	navJournal   = "journal"

	contentTypeHTML = "text/html; charset=utf-8"
	contentTypeCSS  = "text/css; charset=utf-8"
	contentTypeText = "text/plain; charset=utf-8"

	contentSecurityPolicy = "default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

	// localOperator is who the operator is said to be when no gate tells.
	localOperator = "local"
	// noValue is logged where there is no operator or no route to name.
	noValue = "-"

	failedHeading = "Something went wrong"
	failedText    = "The page could not be shown. The error is in the log."
)

var pageFiles = []string{
	pageAttention, pageCustomers, pageCustomer, pageLicense, pageInstance, pageReport, pagePending, pageMessage,
	pageCustomerForm, pageLicenseForm, pageLicenseConfirm, pageLicenseKey, pageJournal,
}

// Reader is what the console reads of the store.
type Reader interface {
	Customers(ctx context.Context, now time.Time) ([]cpstore.CustomerSummary, error)
	Customer(ctx context.Context, id uuid.UUID, now time.Time) (*cpstore.CustomerDetail, error)
	LicenseDetail(ctx context.Context, id string, now time.Time) (*cpstore.LicenseDetail, error)
	Instance(ctx context.Context, licenseID, instanceID string) (*cpstore.InstanceDetail, error)
	Report(ctx context.Context, licenseID, instanceID string, day time.Time) (*cpstore.StoredReport, error)
	PendingByFingerprint(ctx context.Context, now time.Time) ([]cpstore.PendingGroup, error)
	Attention(ctx context.Context, now time.Time) (*cpstore.Attention, error)
	Journal(ctx context.Context, limit int) ([]cpstore.OperatorAction, error)
}

// Writer is what the console writes to the store. Each write is journaled by the store, in the
// transaction of the write.
type Writer interface {
	CreateCustomer(ctx context.Context, operator string, c cpstore.NewCustomer, now time.Time) (uuid.UUID, error)
	UpdateCustomer(ctx context.Context, operator string, id uuid.UUID, name, note string, now time.Time) error
	RecordIssuedLicense(
		ctx context.Context, operator string, key *license.Key, issued *license.IssuedLicense,
		signingKeyFingerprint, succeeds, note string, now time.Time,
	) (added bool, err error)
	// ShowLicenseKey is the only read of a key the console has, and it is journaled.
	ShowLicenseKey(ctx context.Context, operator, licenseID string, now time.Time) (string, error)
}

// Signer signs the licenses the console issues. The console holds it behind this interface only,
// and never prints it.
type Signer interface {
	Issue(d *issuing.Draft, now time.Time) (*license.IssuedLicense, *license.Key, error)
	// PublicKeyFingerprint is the fingerprint of the public key that verifies what Issue signs.
	PublicKeyFingerprint() string
}

// Promoter goes through the reports that were pending under the fingerprint of a key just issued.
type Promoter interface {
	PromotePending(ctx context.Context, fingerprint string) (stored, discarded int, err error)
}

// Config is what the console is made of.
type Config struct {
	Reader Reader
	Writer Writer
	// Signer is nil on a server that issues no license: the customers are still managed, the
	// pages say issuing is not configured, and the routes of issuing answer the not found page.
	Signer Signer
	// Promoter is asked after each issue; it is not asked without a Signer.
	Promoter Promoter
	Now      func() time.Time
	Logger   *slog.Logger
}

// New returns the console. It is to be served behind the Access gate: the operator of a request
// is the one the gate let through, and a request that did not pass it gets no page.
//
// A page is a GET and an act of the operator a POST, refused when it comes from another origin;
// anything else is answered the not found page. Every answer forbids caching and carries a
// content security policy that allows the stylesheet of the console, forms sent to the console
// itself, and nothing else. One line is logged per request: the operator, the method, the pattern
// of the route and the status, never the path as it was written nor a value of a form.
//
// The templates are read here, and an error is returned when one of them is not sound.
func New(cfg *Config) (http.Handler, error) {
	return newConsole(embedded, cfg, false)
}

// NewUnguarded returns the console as New does, to be served without any gate, on one's own
// machine: the operator is shown as "local" and every page says the gates are off.
func NewUnguarded(cfg *Config) (http.Handler, error) {
	return newConsole(embedded, cfg, true)
}

type console struct {
	store    Reader
	writer   Writer
	signer   Signer
	promoter Promoter
	now      func() time.Time
	logger   *slog.Logger
	// sameOrigin refuses the POST another origin makes the browser of the operator send.
	sameOrigin *http.CrossOriginProtection
	unguarded  bool
	pages      map[string]*template.Template
	stylesheet []byte
	mux        *http.ServeMux
}

func newConsole(files fs.FS, cfg *Config, unguarded bool) (http.Handler, error) {
	c := &console{
		store:      cfg.Reader,
		writer:     cfg.Writer,
		signer:     cfg.Signer,
		promoter:   cfg.Promoter,
		now:        cfg.Now,
		logger:     cfg.Logger,
		sameOrigin: http.NewCrossOriginProtection(),
		unguarded:  unguarded,
		pages:      make(map[string]*template.Template, len(pageFiles)),
		mux:        http.NewServeMux(),
	}
	for _, file := range pageFiles {
		page, err := parsePage(files, file)
		if err != nil {
			return nil, err
		}
		c.pages[file] = page
	}
	stylesheet, err := fs.ReadFile(files, stylesheetFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read the stylesheet of the console: %w", err)
	}
	c.stylesheet = stylesheet

	c.route("GET /{$}", c.attention)
	c.route("GET /customers", c.customers)
	c.route("GET /customers/{id}", c.customer)
	c.route("GET /licenses/{id}", c.license)
	c.route("GET /licenses/{license}/instances/{instance}", c.instance)
	c.route("GET /licenses/{license}/instances/{instance}/reports/{day}", c.report)
	c.route("GET /pending", c.pending)
	c.route("GET /journal", c.journal)
	c.routeWrites()
	c.mux.HandleFunc("GET "+stylesheetPath, func(w http.ResponseWriter, _ *http.Request) {
		answer := replyOf(w)
		answer.route = "GET " + stylesheetPath
		answer.send(http.StatusOK, contentTypeCSS, c.stylesheet)
	})
	// Whatever no route takes, another method on a route included.
	c.mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		c.notFound(replyOf(w))
	})
	return c, nil
}

// replyOf is the answer a handler of the mux writes to: ServeHTTP hands the mux nothing else.
func replyOf(w http.ResponseWriter) *reply {
	return w.(*reply)
}

// parsePage reads the layout and one page, and makes sure they hold together: html/template
// only finds a missing template, or a value written where it cannot be made safe, when a page is
// first rendered. It is rendered here, empty, so that it is found at the start.
func parsePage(files fs.FS, file string) (*template.Template, error) {
	page, err := template.New(file).ParseFS(files, "templates/"+layoutFile, "templates/"+file)
	if err == nil {
		err = page.ExecuteTemplate(io.Discard, layoutTemplate, layout{})
	}
	if err != nil {
		return nil, fmt.Errorf("unable to read the template %s of the console: %w", file, err)
	}
	return page, nil
}

// content is a page to render: which one, under which title and link of the layout, with what.
type content struct {
	file  string
	title string
	nav   string
	body  any
}

// reply is the answer to one request, and what its log line says.
type reply struct {
	http.ResponseWriter
	operator string
	route    string
	status   int
}

// WriteHeader remembers the status, whoever answers: the mux redirects by itself a path that is
// not written cleanly.
func (a *reply) WriteHeader(status int) {
	a.status = status
	a.ResponseWriter.WriteHeader(status)
}

func (a *reply) send(status int, contentType string, body []byte) {
	a.Header().Set("Content-Type", contentType)
	a.Header().Set("Content-Length", strconv.Itoa(len(body)))
	a.WriteHeader(status)
	// The caller may be gone: there is nobody to tell. A body is a page html/template made, every
	// value in it escaped, or the stylesheet.
	_, _ = a.Write(body) //nolint:gosec // see above: nothing of a request is written unescaped
}

// SetSecurityHeaders sets on an answer the headers every answer of the console carries: no
// caching, a content security policy that allows the stylesheet of the console and nothing else,
// no guessing of the content type and no referrer. The console sets them itself; whoever answers
// in front of it, a gate or a health check, sets them with this.
func SetSecurityHeaders(header http.Header) {
	header.Set("Cache-Control", "no-store")
	header.Set("Content-Security-Policy", contentSecurityPolicy)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
}

func (c *console) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	SetSecurityHeaders(w.Header())

	answer := &reply{ResponseWriter: w, operator: noValue, route: noValue}
	defer func() {
		if recover() != nil {
			// Fixed words only: what a panic carries may hold something of what was read. A
			// page is made whole before any of it is sent, so nothing was sent yet.
			c.logger.Error("the console panicked")
			if answer.status == 0 {
				c.failed(answer)
			}
		}
		c.logger.Info("request",
			"operator", answer.operator, "method", r.Method, "route", answer.route, "status", answer.status)
	}()

	if c.unguarded {
		answer.operator = localOperator
	} else if operator, ok := accessgate.Operator(r.Context()); ok {
		answer.operator = operator
	} else {
		// The console is wired without its gate: it shows nothing rather than show it to anyone.
		c.logger.Error("a request reached the console without passing the Access gate")
		c.failed(answer)
		return
	}
	c.mux.ServeHTTP(answer, r)
}

// route serves pattern with the page read gives. cpstore.ErrNotFound from read is the not found
// page; any other error is the failure page, and the error is logged.
func (c *console) route(pattern string, read func(r *http.Request) (*content, error)) {
	c.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		answer := replyOf(w)
		answer.route = pattern
		page, err := read(r)
		switch {
		case errors.Is(err, cpstore.ErrNotFound):
			c.notFound(answer)
		case err != nil:
			c.logger.Error("unable to read what a page of the console shows", "route", pattern, "error", err.Error())
			c.failed(answer)
		default:
			c.render(answer, http.StatusOK, page)
		}
	})
}

// render makes the page whole, then sends it: a template that fails once half of the page is
// made gives the failure page, not half a page.
func (c *console) render(answer *reply, status int, page *content) {
	body, err := c.execute(answer, page)
	if err != nil {
		c.logger.Error("unable to render a page of the console", "page", page.file, "error", err.Error())
		c.failed(answer)
		return
	}
	answer.send(status, contentTypeHTML, body)
}

func (c *console) execute(answer *reply, page *content) ([]byte, error) {
	var body bytes.Buffer
	err := c.pages[page.file].ExecuteTemplate(&body, layoutTemplate, layout{
		Title:     page.title,
		Nav:       page.nav,
		Operator:  answer.operator,
		Unguarded: c.unguarded,
		Body:      page.body,
	})
	return body.Bytes(), err
}

func (c *console) notFound(answer *reply) {
	c.say(answer, http.StatusNotFound, messageView{Heading: "Not found", Text: "Nothing is here."})
}

// say answers, under the status, a page that only says something.
func (c *console) say(answer *reply, status int, message messageView) {
	c.render(answer, status, &content{file: pageMessage, title: message.Heading, body: message})
}

// failed answers the failure page, in fixed words. Should that page itself not render, the same
// words are sent bare.
func (c *console) failed(answer *reply) {
	body, err := c.execute(answer, &content{
		file:  pageMessage,
		title: failedHeading,
		body:  messageView{Heading: failedHeading, Text: failedText},
	})
	if err != nil {
		c.logger.Error("unable to render the failure page of the console", "error", err.Error())
		answer.send(http.StatusInternalServerError, contentTypeText, []byte(failedText+"\n"))
		return
	}
	answer.send(http.StatusInternalServerError, contentTypeHTML, body)
}

func (c *console) attention(r *http.Request) (*content, error) {
	attention, err := c.store.Attention(r.Context(), c.now())
	if err != nil {
		return nil, err
	}
	return &content{file: pageAttention, title: "Needs attention", nav: navAttention, body: newAttentionView(attention)}, nil
}

func (c *console) customers(r *http.Request) (*content, error) {
	customers, err := c.store.Customers(r.Context(), c.now())
	if err != nil {
		return nil, err
	}
	return &content{file: pageCustomers, title: "Customers", nav: navCustomers, body: newCustomersView(customers)}, nil
}

func (c *console) customer(r *http.Request) (*content, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, cpstore.ErrNotFound
	}
	customer, err := c.store.Customer(r.Context(), id, c.now())
	if err != nil {
		return nil, err
	}
	return &content{file: pageCustomer, title: customer.Name, nav: navCustomers, body: newCustomerView(customer, c.signer != nil)}, nil
}

// storable says whether the store can hold every one of values. A text of PostgreSQL is valid
// UTF-8 without a zero byte: asked for another one, it answers an error where there is simply
// nothing under that name.
func storable(values ...string) bool {
	for _, value := range values {
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return false
		}
	}
	return true
}

func (c *console) license(r *http.Request) (*content, error) {
	id := r.PathValue("id")
	if !storable(id) {
		return nil, cpstore.ErrNotFound
	}
	detail, err := c.store.LicenseDetail(r.Context(), id, c.now())
	if err != nil {
		return nil, err
	}
	return &content{
		file: pageLicense, title: "License " + detail.ID, nav: navCustomers, body: newLicenseView(detail, c.signer != nil),
	}, nil
}

func (c *console) instance(r *http.Request) (*content, error) {
	licenseID, instanceID := r.PathValue("license"), r.PathValue("instance")
	if !storable(licenseID, instanceID) {
		return nil, cpstore.ErrNotFound
	}
	instance, err := c.store.Instance(r.Context(), licenseID, instanceID)
	if err != nil {
		return nil, err
	}
	return &content{
		file: pageInstance, title: "Instance " + instance.InstanceID, nav: navCustomers, body: newInstanceView(instance),
	}, nil
}

func (c *console) report(r *http.Request) (*content, error) {
	day, err := time.Parse(time.DateOnly, r.PathValue("day"))
	licenseID, instanceID := r.PathValue("license"), r.PathValue("instance")
	if err != nil || !storable(licenseID, instanceID) {
		return nil, cpstore.ErrNotFound
	}
	report, err := c.store.Report(r.Context(), licenseID, instanceID, day)
	if err != nil {
		return nil, err
	}
	view := newReportView(report)
	return &content{file: pageReport, title: "Report of " + view.Day, nav: navCustomers, body: view}, nil
}

func (c *console) pending(r *http.Request) (*content, error) {
	groups, err := c.store.PendingByFingerprint(r.Context(), c.now())
	if err != nil {
		return nil, err
	}
	return &content{file: pagePending, title: "Pending", nav: navPending, body: newPendingView(groups)}, nil
}

func (c *console) journal(r *http.Request) (*content, error) {
	// One line more than the page shows tells whether the journal holds older ones.
	actions, err := c.store.Journal(r.Context(), journalLines+1)
	if err != nil {
		return nil, err
	}
	return &content{file: pageJournal, title: "Journal", nav: navJournal, body: newJournalView(actions)}, nil
}
