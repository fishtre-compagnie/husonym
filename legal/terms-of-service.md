# Husonym Subscription Terms — DRAFT

> **This is a draft prepared for legal review, not a contract.** It is written to match
> exactly what the software does today, so that a lawyer can work from something factually
> accurate rather than from a template. It has not been reviewed by a qualified
> professional and must not be published or signed as-is.
>
> Points requiring a decision are marked **[DECIDE]**. Points where the wording carries
> real legal risk and needs professional drafting are marked **[LEGAL]**.

**Publication target:** `https://husonym.com/terms-of-service` — referenced by the
Enterprise License in every `ee/` directory and by fifteen OpenAPI specifications. That URL
currently returns 404, which weakens every clause that depends on it.

---

## 1. Parties and scope

These terms govern the supply of the Husonym software (the "Software") by **Fishtre
Compagnie** ("we", "us") to the subscribing organisation ("you", "Customer").

**[DECIDE]** Legal identity to state: full company name, legal form, registered address,
registration number, VAT number, and share capital. French law requires these on
commercial terms.

**[DECIDE]** Governing language. Drafted here in English because the Software, its
documentation and the existing Enterprise License are in English. If you sell primarily to
French customers, a French version prevailing may be preferable — and for consumers it
would be mandatory, though B2B gives latitude.

## 2. Definitions

- **Software** — the Husonym application supplied as container images and Helm charts.
- **License Key** — the signed value set as `EE_LICENSE`, which enables the Software.
- **Subscription Term** — the period stated in the Order, matching the expiry date encoded
  in the License Key.
- **Grace Period** — the period after expiry during which the Software continues to
  operate in full. Fourteen days unless the Order states otherwise.
- **Order** — the quotation or order form referencing these terms.

## 3. Licence granted

Subject to payment and compliance, we grant a **non-exclusive, non-transferable,
non-sublicensable** right to install and use the Software, for the Subscription Term, for
your own internal business purposes and those of your affiliates.

The Software is supplied **as container images and Helm charts**. Source code is not part
of the supply.

**[DECIDE]** Affiliates: included or not. Groups will ask.

## 4. Usage limits

Your Order may state limits, which are encoded in the License Key and enforced by the
Software:

| Limit | Effect when reached |
| --- | --- |
| Maximum number of jobs | new jobs cannot be created |
| Maximum number of connections | new connections cannot be created |
| Permitted connection types | connections of other types cannot be created |

Reaching a limit never affects resources already created or runs already in progress.
Contact us to raise a limit; we will issue a replacement License Key.

## 5. Restrictions

You may not:

- remove, disable, circumvent or tamper with the licensing mechanism, or use the Software
  beyond the limits encoded in your License Key;
- share, publish or transfer your License Key;
- redistribute, resell, sublicense or make the Software available to third parties as a
  service;
- reverse engineer the Software, except to the extent that restriction is unenforceable
  under applicable law.

**[LEGAL]** The reverse engineering carve-out is not optional in the EU: Directive
2009/24/EC preserves decompilation rights for interoperability, and a clause purporting to
remove them is void. Wording must be drafted, not copied from a US template.

## 6. Term, renewal and expiry

The Subscription Term is stated in the Order. **[DECIDE]** Renewal: automatic with notice
of non-renewal, or express. Automatic renewal on B2B contracts in France carries
information obligations.

What happens as a subscription ends — this matches the Software's actual behaviour:

| Stage | Software behaviour |
| --- | --- |
| 30 days before expiry | fully operational; a notice is displayed |
| Expiry | fully operational; a blocking notice is displayed (Grace Period) |
| End of Grace Period | creating, configuring and running jobs stops |

After the Grace Period the Software **does not delete anything and does not lock you out**.
You retain access to all configuration and run history, and can still pause schedules,
cancel running work, and delete jobs and connections. Runs already in progress at that
moment are allowed to complete. Installing a valid License Key restores full operation
immediately.

## 7. Fees

**[DECIDE]** Price, currency, invoicing frequency, payment terms.

**[LEGAL]** French B2B rules impose a maximum payment term (60 days from invoice, or 45
days end-of-month), mandatory late payment interest, and a fixed €40 recovery indemnity.
These must appear and cannot be waived.

## 8. Support

**[DECIDE]** What is included: channels, hours, response targets, and whether they are
commitments or objectives. Do not state a target you cannot measure — a customer will hold
you to it.

## 9. Intellectual property

The Software, and all intellectual property in it, remains ours or our licensors'. Nothing
transfers to you beyond the licence in clause 3.

**[LEGAL]** Husonym derives from Neosync (Nucleus Cloud Corp), whose code is under the MIT
licence and whose copyright notices are retained in this repository. The interaction
between that inherited licence and these terms needs professional review — see clause 14.

## 10. Your data

**The Software runs in your infrastructure. We do not access, receive, host or process any
data you process with it.** We have no access to your databases, your configuration or your
runs.

Consequently, in respect of the personal data you process using the Software, **you are the
data controller and we are not a processor** within the meaning of the GDPR. No data
processing agreement is required for the supply of the Software itself.

**[LEGAL]** Confirm this analysis before relying on it. It should hold for a pure
on-premise supply with no telemetry — and the Software does no phone-home, license
verification included. It would change the moment we offer a hosted version, provide
support involving access to your systems, or collect diagnostics.

**[LEGAL]** Anonymisation claims need care. The Software helps you anonymise data; whether
a given output is anonymous in law — and therefore outside the GDPR — depends on your data
and your configuration, and remains your assessment. The marketing site already states
this; the contract should too, and should not warrant a compliance outcome.

## 11. Warranties

**[LEGAL]** The existing Enterprise License uses a blanket US-style "AS IS" disclaimer.
Under French law, the *garantie des vices cachés* and *garantie de conformité* cannot be
excluded entirely, and a clause emptying the contract of its substance is void
(Article 1170 of the Civil Code). This needs proper drafting rather than translation.

## 12. Limitation of liability

**[DECIDE]** Liability cap — commonly the fees paid over the preceding twelve months.

**[LEGAL]** Exclusions of gross negligence (*faute lourde*) and wilful misconduct (*dol*)
are unenforceable in France, and a cap disproportionate to the essential obligation may be
struck down. Requires drafting.

## 13. Termination

**[DECIDE]** Termination for material breach with a cure period; termination for
insolvency; effect of termination (the Software ceases to operate at the end of the Grace
Period, as described in clause 6).

## 14. Relationship with the open source licences

The repository carries two licences: the MIT Expat licence for most of the code, and the
Enterprise License for the `ee/` directories.

**[LEGAL] This is the most important item on this list.** Three facts a lawyer needs:

1. Husonym is a fork of Neosync, published under MIT. **Versions already published under
   MIT remain MIT**; that cannot be withdrawn retroactively, and a fork of them stays
   lawful.
2. **The code enforcing the licence is itself under MIT** — it lives in
   `backend/services/.../job-service` and `backend/internal/userdata`, not under any `ee/`
   directory. A recipient of the source could lawfully remove the check under MIT terms.
   The contractual restriction in clause 5 is what makes that a breach for a customer, but
   it does not bind someone who obtained the source elsewhere.
3. The repository is currently **public**, a deliberate decision (CI minutes on private
   repositories). So the source is available to anyone today.

Questions for the lawyer: whether to relicense our own future contributions; whether to
move the enforcement code under `ee/`; and how clause 5 should be worded so it binds
customers effectively given that the code they could obtain is MIT.

## 15. Miscellaneous

**[DECIDE]** Governing law and jurisdiction; assignment; force majeure; entire agreement;
notices; use of the customer's name as a reference.

---

## What must be fixed regardless of the drafting

Findings from reviewing the repository against what is actually sold:

1. **`https://husonym.com/terms-of-service` returns 404**, while being the stated condition
   of production use in the Enterprise License and the declared `termsOfService` in fifteen
   OpenAPI specifications. A condition pointing at nothing is weak. Publishing this page is
   the single highest-value fix. *(The URLs themselves were corrected from `www.` to the
   apex.)*

2. **The Enterprise License describes a different product.** It conditions use on agreeing
   to the *"Husonym Cloud Terms of Service"* — there is no Cloud offering — and on holding a
   licence *"for the correct number of user seats"*, when the implemented licence counts
   jobs, connections and connector types, never seats. Text and mechanism must agree.

3. **It grants rights the distribution model does not.** It says you *"are free to modify
   this Software and publish patches"*, which assumes the customer has the source. They
   receive images.

4. **Copyright attribution** carries both Nucleus Cloud Corp and Fishtre Compagnie. Correct
   for inherited code, but the document presents itself as the *Husonym* Enterprise
   License; a lawyer should confirm the attribution is right. *(A stray 2025 vs 2024
   mismatch between copies was corrected; the three files are now identical.)*

5. **The docs still invite readers to "sign up for free"** in two guides — a leftover from
   Neosync that contradicts the commercial positioning and could be read as an offer.
