// Package console is the operator console of the control plane: pages that show the customers,
// the licenses, the instances and the reports it holds. It only reads.
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
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/accessgate"
	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
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

	navAttention = "attention"
	navCustomers = "customers"
	navPending   = "pending"

	contentTypeHTML = "text/html; charset=utf-8"
	contentTypeCSS  = "text/css; charset=utf-8"
	contentTypeText = "text/plain; charset=utf-8"

	contentSecurityPolicy = "default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

	// localOperator is who the operator is said to be when no gate tells.
	localOperator = "local"
	// noValue is logged where there is no operator or no route to name.
	noValue = "-"

	failedHeading = "Something went wrong"
	failedText    = "The page could not be shown. The error is in the log."
)

var pageFiles = []string{
	pageAttention, pageCustomers, pageCustomer, pageLicense, pageInstance, pageReport, pagePending, pageMessage,
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
}

// New returns the console over store. It is to be served behind the Access gate: the operator of
// a request is the one the gate let through, and a request that did not pass it gets no page.
//
// Every route is a GET; anything else is answered the not found page. Every answer forbids
// caching and carries a content security policy that allows the stylesheet of the console and
// nothing else. One line is logged per request: the operator, the method, the pattern of the
// route and the status, never the path as it was written.
//
// The templates are read here, and an error is returned when one of them is not sound.
func New(store Reader, now func() time.Time, logger *slog.Logger) (http.Handler, error) {
	return newConsole(embedded, store, now, logger, false)
}

// NewUnguarded returns the console as New does, to be served without any gate, on one's own
// machine: the operator is shown as "local" and every page says the gates are off.
func NewUnguarded(store Reader, now func() time.Time, logger *slog.Logger) (http.Handler, error) {
	return newConsole(embedded, store, now, logger, true)
}

type console struct {
	store      Reader
	now        func() time.Time
	logger     *slog.Logger
	unguarded  bool
	pages      map[string]*template.Template
	stylesheet []byte
	mux        *http.ServeMux
}

func newConsole(files fs.FS, store Reader, now func() time.Time, logger *slog.Logger, unguarded bool) (http.Handler, error) {
	c := &console{
		store:     store,
		now:       now,
		logger:    logger,
		unguarded: unguarded,
		pages:     make(map[string]*template.Template, len(pageFiles)),
		mux:       http.NewServeMux(),
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
	// The caller may be gone: there is nobody to tell.
	_, _ = a.Write(body)
}

func (c *console) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("Cache-Control", "no-store")
	header.Set("Content-Security-Policy", contentSecurityPolicy)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")

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
	c.render(answer, http.StatusNotFound, &content{
		file:  pageMessage,
		title: "Not found",
		body:  messageView{Heading: "Not found", Text: "Nothing is here."},
	})
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
	return &content{file: pageCustomer, title: customer.Name, nav: navCustomers, body: newCustomerView(customer)}, nil
}

func (c *console) license(r *http.Request) (*content, error) {
	license, err := c.store.LicenseDetail(r.Context(), r.PathValue("id"), c.now())
	if err != nil {
		return nil, err
	}
	return &content{file: pageLicense, title: "License " + license.ID, nav: navCustomers, body: newLicenseView(license)}, nil
}

func (c *console) instance(r *http.Request) (*content, error) {
	instance, err := c.store.Instance(r.Context(), r.PathValue("license"), r.PathValue("instance"))
	if err != nil {
		return nil, err
	}
	return &content{
		file: pageInstance, title: "Instance " + instance.InstanceID, nav: navCustomers, body: newInstanceView(instance),
	}, nil
}

func (c *console) report(r *http.Request) (*content, error) {
	day, err := time.Parse(time.DateOnly, r.PathValue("day"))
	if err != nil {
		return nil, cpstore.ErrNotFound
	}
	report, err := c.store.Report(r.Context(), r.PathValue("license"), r.PathValue("instance"), day)
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
