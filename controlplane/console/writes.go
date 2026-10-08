package console

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/issuing"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/google/uuid"
)

// The acts of the operator and the forms that lead to them. An act is a POST; the store journals
// it in the transaction of its write. Nothing of a form is logged: a failure is logged in our own
// words, with the text of an error only where that text is ours.

const (
	// maxFormBytes caps the body of a POST.
	maxFormBytes = 64 << 10

	// customerChanged is said of a form whose customer is not, or no longer, the one of the store.
	customerChanged = "The form does not say of the customer what is recorded: it may have been changed since. " +
		"Nothing was done; open the form again."
	// elsewhere names the command that issues what a form of the console cannot.
	elsewhere = "husonym-license, on the command line, can issue its successor."
)

func (c *console) routeWrites() {
	c.route("GET /customers/new", c.newCustomerForm)
	c.act("POST /customers", c.createCustomer)
	c.route("GET /customers/{id}/edit", c.editCustomerForm)
	c.act("POST /customers/{id}", c.updateCustomer)

	c.route("GET /customers/{id}/licenses/new", c.newLicenseForm)
	c.routeAnswer("GET /licenses/{id}/renew", c.renewLicenseForm)
	c.act("POST /licenses/confirm", c.confirmLicense)
	c.act("POST /licenses", c.issueLicense)
	c.act("POST /licenses/{id}/key", c.showKeyAgain)
}

// routeAnswer serves pattern with a handler that answers by itself.
func (c *console) routeAnswer(pattern string, do func(answer *reply, r *http.Request)) {
	c.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		answer := replyOf(w)
		answer.route = pattern
		do(answer, r)
	})
}

// act serves pattern, a POST, with do, which is given the form of the request. A request another
// origin made the browser send is refused before anything is read, and so is a body over
// maxFormBytes or one that is not a form.
func (c *console) act(pattern string, do func(answer *reply, r *http.Request, form url.Values)) {
	c.routeAnswer(pattern, func(answer *reply, r *http.Request) {
		if err := c.sameOrigin.Check(r); err != nil {
			c.say(answer, http.StatusForbidden, messageView{
				Heading: "Refused",
				Text:    "The request did not come from a page of this console. Nothing was done.",
			})
			return
		}
		r.Body = http.MaxBytesReader(answer, r.Body, maxFormBytes)
		if err := r.ParseForm(); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				c.say(answer, http.StatusRequestEntityTooLarge, messageView{
					Heading: "Too large", Text: "The form is larger than the console takes. Nothing was done.",
				})
				return
			}
			c.badRequest(answer, "The form could not be read. Nothing was done.")
			return
		}
		do(answer, r, r.PostForm)
	})
}

func (c *console) badRequest(answer *reply, text string) {
	c.say(answer, http.StatusBadRequest, messageView{Heading: "Bad request", Text: text})
}

func (c *console) conflict(answer *reply, text string, back link) {
	c.say(answer, http.StatusConflict, messageView{Heading: "Not done", Text: text, Back: back})
}

// broke answers the failure page for an act that failed, and logs err, an error of the store: its
// text is ours and the one of the database, and holds no value of a form.
func (c *console) broke(answer *reply, what string, err error) {
	c.logger.Error(what, "route", answer.route, "error", err.Error())
	c.failed(answer)
}

// redirect leads to a page of the console. to is a path made here, "/customers/" or "/licenses/"
// followed by an id escaped as one segment: it names no other host, whatever the id holds.
func (c *console) redirect(answer *reply, r *http.Request, to string) {
	http.Redirect(answer, r, to, http.StatusSeeOther) //nolint:gosec // see above: a path of the console, never a host
}

func (c *console) newCustomerForm(*http.Request) (*content, error) {
	return customerFormPage(nil, url.Values{}, nil), nil
}

func customerFormPage(customer *cpstore.CustomerDetail, form url.Values, problems []string) *content {
	view := newCustomerFormView(customer, form, problems)
	return &content{file: pageCustomerForm, title: view.Heading, nav: navCustomers, body: view}
}

func (c *console) createCustomer(answer *reply, r *http.Request, form url.Values) {
	problems := customerProblems(form, true)
	if len(problems) > 0 {
		c.render(answer, http.StatusBadRequest, customerFormPage(nil, form, problems))
		return
	}
	id, err := c.writer.CreateCustomer(r.Context(), answer.operator, cpstore.NewCustomer{
		ExternalID: typed(form, fieldExternalID),
		Name:       typed(form, fieldName),
		Note:       typed(form, fieldNote),
	}, c.now())
	switch {
	case errors.Is(err, cpstore.ErrCustomerExists):
		c.render(answer, http.StatusConflict, customerFormPage(nil, form, []string{"A customer already has this external id."}))
	case errors.Is(err, cpstore.ErrCustomerIncomplete):
		c.render(answer, http.StatusBadRequest, customerFormPage(nil, form, []string{"A customer needs an external id and a name."}))
	case err != nil:
		c.broke(answer, "unable to record a customer", err)
	default:
		c.redirect(answer, r, customerLink(id, "").Href)
	}
}

func (c *console) editCustomerForm(r *http.Request) (*content, error) {
	customer, err := c.customerOfPath(r)
	if err != nil {
		return nil, err
	}
	return customerFormPage(customer, customerForm(customer), nil), nil
}

// customerOfPath reads the customer the path names; cpstore.ErrNotFound when it names none.
func (c *console) customerOfPath(r *http.Request) (*cpstore.CustomerDetail, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return nil, cpstore.ErrNotFound
	}
	return c.store.Customer(r.Context(), id, c.now())
}

func (c *console) updateCustomer(answer *reply, r *http.Request, form url.Values) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		c.notFound(answer)
		return
	}
	problems := customerProblems(form, false)
	if len(problems) == 0 {
		err = c.writer.UpdateCustomer(r.Context(), answer.operator, id, typed(form, fieldName), typed(form, fieldNote), c.now())
		switch {
		case errors.Is(err, cpstore.ErrCustomerIncomplete):
			problems = []string{"A customer needs a name."}
		case errors.Is(err, cpstore.ErrNotFound):
			c.notFound(answer)
			return
		case err != nil:
			c.broke(answer, "unable to change a customer", err)
			return
		default:
			c.redirect(answer, r, customerLink(id, "").Href)
			return
		}
	}
	// The form again: its external id is the one of the store, never one that was sent.
	customer, err := c.store.Customer(r.Context(), id, c.now())
	switch {
	case errors.Is(err, cpstore.ErrNotFound):
		c.notFound(answer)
	case err != nil:
		c.broke(answer, "unable to read a customer", err)
	default:
		c.render(answer, http.StatusBadRequest, customerFormPage(customer, form, problems))
	}
}

func licenseFormPage(customer *cpstore.CustomerDetail, form url.Values, problems []string) *content {
	view := newLicenseFormView(customer, form, problems)
	return &content{file: pageLicenseForm, title: view.Heading, nav: navCustomers, body: view}
}

func (c *console) newLicenseForm(r *http.Request) (*content, error) {
	if c.signer == nil {
		return nil, cpstore.ErrNotFound
	}
	customer, err := c.customerOfPath(r)
	if err != nil {
		return nil, err
	}
	now := c.now()
	// No feature, and the expiry a year ahead. The id of the license is drawn at the confirmation.
	draft := &issuing.Draft{
		CustomerExternalID: customer.ExternalID,
		CustomerName:       customer.Name,
		Features:           []string{},
		ExpiresAt:          now.UTC().AddDate(1, 0, 0),
	}
	if r.URL.Query().Get(queryTrial) == "1" {
		if draft = issuing.TrialDraft(customer, now); draft == nil {
			return nil, errors.New("no trial could be drafted for a customer that is there")
		}
	}
	return licenseFormPage(customer, draft.Form(), nil), nil
}

func (c *console) renewLicenseForm(answer *reply, r *http.Request) {
	id := r.PathValue("id")
	if c.signer == nil || !storable(id) {
		c.notFound(answer)
		return
	}
	now := c.now()
	previous, err := c.store.LicenseDetail(r.Context(), id, now)
	if err == nil && len(previous.SuccessorIDs) > 0 {
		c.cannotRenew(answer, previous, "It already has a successor: a license is renewed once.")
		return
	}
	var customer *cpstore.CustomerDetail
	if err == nil {
		customer, err = c.store.Customer(r.Context(), previous.CustomerID, now)
	}
	var draft *issuing.Draft
	if err == nil {
		draft, err = issuing.RenewalDraft(previous, customer, now)
	}
	switch {
	case errors.Is(err, cpstore.ErrNotFound):
		c.notFound(answer)
	case errors.Is(err, issuing.ErrLimitsNotCarried):
		c.cannotRenew(answer, previous, "Its key carries a limit the form of this console has no field for. "+elsewhere)
	case errors.Is(err, issuing.ErrLicenseIDNotRenewable):
		c.cannotRenew(answer, previous, "Its id is not of the form this console names a license by. "+elsewhere)
	case err != nil:
		// The errors of a renewal that cannot be drafted are fixed words.
		c.broke(answer, "unable to draft the renewal of a license", err)
	case draft == nil:
		c.broke(answer, "unable to draft the renewal of a license", errors.New("no draft was made"))
	default:
		c.render(answer, http.StatusOK, licenseFormPage(customer, draft.Form(), nil))
	}
}

func (c *console) cannotRenew(answer *reply, previous *cpstore.LicenseDetail, why string) {
	back := licenseLink(previous.ID)
	back.Text = "Back to the license"
	c.say(answer, http.StatusOK, messageView{
		Heading: "This license cannot be renewed here",
		Text:    why,
		Back:    back,
	})
}

// customerOfForm reads from the store the customer a form of a license is for, and holds what the
// form says of it to what the store says: the hidden fields are never trusted. Without such a
// customer it answers by itself, and ok is false.
func (c *console) customerOfForm(answer *reply, r *http.Request, form url.Values) (customer *cpstore.CustomerDetail, ok bool) {
	id, err := uuid.Parse(typed(form, fieldCustomer))
	if err != nil {
		c.badRequest(answer, "The form names no customer. Nothing was done.")
		return nil, false
	}
	customer, err = c.store.Customer(r.Context(), id, c.now())
	switch {
	case errors.Is(err, cpstore.ErrNotFound):
		c.conflict(answer, "The customer of the form is not recorded. Nothing was done.", link{Text: "The customers", Href: "/customers"})
		return nil, false
	case err != nil:
		c.broke(answer, "unable to read the customer of a license", err)
		return nil, false
	}
	// Trimmed on both sides, as a draft trims what it reads.
	if typed(form, issuing.FieldCustomerExternalID) != strings.TrimSpace(customer.ExternalID) ||
		typed(form, issuing.FieldCustomerName) != strings.TrimSpace(customer.Name) {
		c.conflict(answer, customerChanged, customerLink(customer.ID, "Back to the customer"))
		return nil, false
	}
	return customer, true
}

func (c *console) confirmLicense(answer *reply, r *http.Request, form url.Values) {
	if c.signer == nil {
		c.notFound(answer)
		return
	}
	customer, ok := c.customerOfForm(answer, r, form)
	if !ok {
		return
	}
	draft, problems := issuing.ParseDraft(form, c.now())
	if draft == nil {
		c.render(answer, http.StatusBadRequest, licenseFormPage(customer, form, problems))
		return
	}
	c.render(answer, http.StatusOK, &content{
		file: pageLicenseConfirm, title: "Issue this license?", nav: navCustomers, body: newLicenseConfirmView(customer, draft),
	})
}

func (c *console) issueLicense(answer *reply, r *http.Request, form url.Values) {
	if c.signer == nil {
		c.notFound(answer)
		return
	}
	// The confirmation carries the id: a draft without one would be given a new id at each
	// sending, and one sent twice would issue two licenses.
	if typed(form, issuing.FieldLicenseID) == "" {
		c.badRequest(answer, "The form is not the confirmation of a license. Nothing was done.")
		return
	}
	customer, ok := c.customerOfForm(answer, r, form)
	if !ok {
		return
	}
	now := c.now()
	draft, problems := issuing.ParseDraft(form, now)
	if draft == nil {
		c.render(answer, http.StatusBadRequest, licenseFormPage(customer, form, problems))
		return
	}
	if c.idTaken(answer, r, draft, customer) {
		return
	}

	issued, key, err := c.signer.Issue(draft, now)
	if err != nil {
		// What the signer says may quote the draft: only that it refused is logged.
		c.logger.Error("the signer refused a draft the form accepted", "route", answer.route)
		c.failed(answer)
		return
	}
	encoded := strings.TrimSpace(issued.Encoded)
	added, err := c.writer.RecordIssuedLicense(
		r.Context(), answer.operator, key, issued, c.signer.PublicKeyFingerprint(), draft.Succeeds, draft.Note, now)
	back := customerLink(customer.ID, "Back to the customer")
	switch {
	case errors.Is(err, cpstore.ErrAlreadySucceeded):
		c.conflict(answer, "The license to renew already has a successor: a license is renewed once. Nothing was issued.", back)
		return
	case errors.Is(err, cpstore.ErrOtherCustomer):
		c.conflict(answer, "The license to renew is a license of another customer. Nothing was issued.", back)
		return
	case errors.Is(err, cpstore.ErrNotFound):
		c.conflict(answer, "The customer, or the license to renew, is not recorded any more. Nothing was issued.", back)
		return
	case err != nil:
		c.broke(answer, "unable to record an issued license", err)
		return
	}
	if !added {
		// Another request recorded a license of that id first: the key just signed is dropped, and
		// the answer is the one of a confirmation that finds its id taken.
		if !c.idTaken(answer, r, draft, customer) {
			c.broke(answer, "unable to record an issued license", errors.New("its id is taken by a license that cannot be read"))
		}
		return
	}

	if c.promoter != nil {
		if _, _, err := c.promoter.PromotePending(r.Context(), telemetry.KeyFingerprint(encoded)); err != nil {
			// The issue stands: the hourly pass of the public server promotes later.
			c.logger.Error("unable to promote the pending reports of a license just issued", "error", err.Error())
		}
	}
	c.keyPage(answer, "The license is issued", draft.LicenseID, encoded)
}

// idTaken answers a confirmation whose license id is the one of a license already recorded, and
// tells it did. Nothing is signed or written for it, and no key is shown: a key is shown by the
// first issue and by asking for it again, which is journaled.
//
// When the license recorded says, field by field, what the draft says, the confirmation was sent
// again: the answer leads to the page of that license. When it says anything else, the draft is
// another one under the same id, and it is refused: a confirmation never answers for another draft.
func (c *console) idTaken(answer *reply, r *http.Request, draft *issuing.Draft, customer *cpstore.CustomerDetail) bool {
	stored, err := c.store.LicenseDetail(r.Context(), draft.LicenseID, c.now())
	if errors.Is(err, cpstore.ErrNotFound) {
		return false
	}
	if err != nil {
		c.broke(answer, "unable to read a license", err)
		return true
	}
	if !draft.IsTheContentOf(stored, customer.ID) {
		c.conflict(answer, "A license with this id already exists, with other content. Nothing was issued; open the form again.",
			customerLink(customer.ID, "Back to the customer"))
		return true
	}
	c.redirect(answer, r, licenseHref(stored.ID))
	return true
}

func (c *console) showKeyAgain(answer *reply, r *http.Request, _ url.Values) {
	id := r.PathValue("id")
	if !storable(id) {
		c.notFound(answer)
		return
	}
	encoded, err := c.writer.ShowLicenseKey(r.Context(), answer.operator, id, c.now())
	switch {
	case errors.Is(err, cpstore.ErrNotFound):
		c.notFound(answer)
	case err != nil:
		c.broke(answer, "unable to show the key of a license", err)
	default:
		c.keyPage(answer, "The key of the license", id, encoded)
	}
}

// keyPage answers the one page that shows a key. It is the answer to a POST only.
func (c *console) keyPage(answer *reply, heading, licenseID, encoded string) {
	c.render(answer, http.StatusOK, &content{
		file: pageLicenseKey, title: heading, nav: navCustomers,
		body: licenseKeyView{Heading: heading, License: licenseLink(licenseID), Key: encoded},
	})
}
