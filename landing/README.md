# Husonym landing

The marketing site served at [www.husonym.com](https://www.husonym.com), in French
(`/`) and English (`/en/`).

No framework, no bundler, no TypeScript, no CSS framework. `scripts/build.mjs` renders
`src/` into `public/` with a small templating pass, inlining every stylesheet and script
so each page costs a **single HTTP request**. The only dependency is `wrangler`, and only
to deploy.

```bash
npm ci
npm run build     # render public/
npm run check     # compare the fr/en catalogues, write nothing
npm run dev       # wrangler dev: serves public/ and runs /api/contact
npm run deploy    # wrangler deploy
```

`npm run build -- --strict` (what CI runs) turns any content problem into a non-zero
exit instead of a warning.

## Layout

```
content/{fr,en}.json    the copy, one nested JSON per locale
assets/                 source images and the brand font — tracked
src/page.html           the page shell
src/not-found.html      the 404 shell
src/sections/*.html     one file per section, in page order
src/styles/*.css        one file per section, mirroring the markup
src/*.js                main (scroll reveal), nav (burger), mask (canvas), contact (form)
worker/                 the Worker: routing plus POST /api/contact
public/                 GENERATED, entirely gitignored
```

### Assets are content-hashed

`assets/` holds the sources; the build copies each file into `public/assets/` under a
name carrying a hash of its own bytes (`mascot.png` → `mascot.ba610d33.png`) and
rewrites every reference — in the markup, in the inlined CSS, in the OG tag and in the
JSON-LD. Write the plain `/assets/mascot.png` path when authoring; never the hashed one.

This is what makes the one-year `immutable` cache header safe. Stable filenames plus a
long cache is the combination to avoid: it pins outdated files on returning visitors,
and it can freeze a 404 for a year on anyone who requested a file during the seconds a
new deploy takes to reach their edge.

Regenerating the images (mascot crop, OG card) is a manual step done with Pillow; the
build only hashes and copies whatever is in `assets/`.

## Templating

Three token forms, resolved in this order:

| Form | Meaning |
| --- | --- |
| `{{> sections/hero.html}}` | include a partial, relative to `src/`, recursive |
| `{{__styles__}}`, `{{__year__}}`, `{{__canonical__}}`, … | assembly tokens, substituted first |
| `{{hero.title}}` | a key from the locale catalogue |

**A key ending in `Html` is injected raw** (so `hero.titleHtml` can carry `<br>` and
`<em>`); every other key is HTML-escaped. Attributes must stay double-quoted — the
escaper covers `"` and `'` but assumes nothing else.

A key missing from both catalogues prints `[build] clé manquante "…"`, leaves the token
visible in the output, and fails the build under `--strict`. A key present in French but
not in the other locale falls back to French and is reported separately.

### Editing copy

Edit `content/fr.json` and `content/en.json`. Both must hold exactly the same keys —
`npm run check` is what enforces it, and CI runs it on every pull request.

### Positioning: this is a commercial product

Husonym is **sold**, not given away. The site must not describe it as open source, free,
MIT-licensed or freely self-hostable, and it links to neither the GitHub repository nor
the docs site. Deployment specifics are deliberately left vague: the sovereignty section
claims that data stays inside the customer's perimeter, and stops there — the *how* is a
conversation for the demo, not a public commitment.

Two consequences worth keeping in mind when editing:

- The JSON-LD carries **no `offers` node and no `license`**. Never reintroduce a price of
  `0`: it would advertise the software as free in search results.
- `github.com/fishtre-compagnie/husonym` is public today and `docs.husonym.com` still
  says "Open source Data Anonymization and Synthetic Data". Until the repository is made
  private and that tagline is reworded, the site's positioning and what a prospect finds
  by searching contradict each other.

### Adding a locale

1. Translate `content/<code>.json`.
2. Add an entry to `LOCALES` in `scripts/build.mjs`.

The language switcher, the `hreflang` links, the sitemap and the per-locale 404 all
follow automatically.

## Brand tokens

Every colour, font and radius lives in the `:root` block at the top of
`src/styles/base.css`, and **nowhere else**. If you need a literal colour somewhere, a
token is missing — add it there. `src/mask.js` reads its colours back out of those
custom properties at runtime, so no hex is hardcoded in the JavaScript either.

The current values are placeholders derived from the two colours attested in the repo
(`#272F30`, the logo ink; `#0B0F14`, the OG card background). Fonts are system stacks:
to adopt the brand faces, drop woff2 files into `public/assets/fonts/`, add `@font-face`
rules under that block, and put the family names at the front of the stacks.

Assets in `public/assets/` are generated from `docs/static/img/` — only
`logo_and_text_dark_mode.png` is already white, so `mark.png` is the icon recoloured for
dark surfaces, and `og.png` is composed for the 1200×630 social card.

## The scroll effect

`src/styles/mask.css` and `src/mask.js` render a field of drifting data fragments that
start as legible PII and turn into solid blocks as you scroll: the page anonymizes
itself on the way down, and the gutter readout counts the real proportion.

Both files are self-contained. Deleting them, their two entries in the `PAGE_STYLES` /
`PAGE_SCRIPTS` lists, and the `.mask-field` / `.mask-probe` elements in `page.html`
leaves a complete working page.

## The Worker

Nearly every request is served straight from Workers Static Assets. Only `/api/*` runs
the script first (`assets.run_worker_first`), which is what allows a contact endpoint on
an otherwise static site.

`POST /api/contact` layers its defences cheapest-first: method and size checks →
honeypot → field validation → per-IP ceiling → Turnstile → per-sender ceiling → send.
Each stage costs more than the one before, so the cheap ones shed load first. A failed
send returns **503**, never a hopeful 200.

Note that `run_worker_first` as an array opts out of Cloudflare's `Sec-Fetch-Mode`
handling, so `worker/index.js` ends with `env.ASSETS.fetch(request)` — that fallback is
what still produces the localised 404 page.

## Deployment

Pushing to `main` with changes under `landing/` triggers
`.github/workflows/landing-deploy.yml`. Pull requests run `.github/workflows/landing.yml`
(build, locale parity, Worker dry-run, page-weight budget).

Repository secrets: `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID`, and
`TURNSTILE_SECRET_KEY`. Repository variable: `HUSONYM_TURNSTILE_SITE_KEY`.

### One-time setup

Account-level steps, not repository ones.

1. **Mail** — ✅ *working, with one constraint worth knowing.*

   Email Routing is enabled on `husonym.com`: MX and SPF resolve, and
   `contact@husonym.com` routes to a real inbox. That address stays the one advertised
   on the site.

   The Worker, however, sends to `contact@fishtre.co`. The `send_email` binding may
   only target an address **verified as an Email Routing destination**; a routed
   address is not one. Sending to `contact@husonym.com` fails with
   `E_RECIPIENT_NOT_ALLOWED — destination address is not a verified address`, which
   surfaces to the visitor as a 503 and "sending failed", even though a human writing
   to that same address gets through fine. The distinction is easy to miss.

   To send to arbitrary recipients — an acknowledgement back to the visitor, say —
   the domain has to be onboarded onto the Email Sending product
   (`wrangler email sending enable husonym.com`, which needs the `email_sending:write`
   OAuth scope). Not required for the contact form as it stands.

2. **Turnstile** — ✅ *done.* Widget `HUSONYM` (managed mode, `husonym.com` +
   `www.husonym.com`), site key `0x4AAAAAAEKBtepC4BVG82Ek`. The site key is the
   `HUSONYM_TURNSTILE_SITE_KEY` repository variable and the secret is the
   `TURNSTILE_SECRET_KEY` repository secret, so CI deploys pick both up. For a manual
   `npm run deploy` from a laptop, the Worker also needs
   `npx wrangler secret put TURNSTILE_SECRET_KEY`.

3. **Apex redirect** — ⛔ *not done*, and it needs two pieces, not one:
   - a proxied DNS record on the apex — there is currently **none**, so a redirect rule
     alone would never fire. Use the same IPv6 black hole as `docs`: `AAAA husonym.com`
     → `100::`, proxied.
   - a Redirect Rule in the `http_request_dynamic_redirect` phase: when
     `http.host eq "husonym.com"`, 301 to
     `concat("https://www.husonym.com", http.request.uri.path)` with the query string
     preserved.

   Create the rule *before* the DNS record: with the record in place and no rule, the
   apex answers a 5xx instead of redirecting. And do **not** add the apex as a second
   `custom_domain` in `wrangler.jsonc` — the site would then be served on both hostnames
   and compete with itself for indexing.

### Testing mail locally

Add `"remote": true` to the `send_email` binding and run `npm run dev` — sends are then
proxied to the real service. Keep it out of the committed config, or every local run
mails someone. Without it, miniflare writes each message to
`.wrangler/tmp/email/` instead, which is enough to check the formatting.
