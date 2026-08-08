// Renders the marketing site into public/: index.html for the default locale,
// <code>/index.html for the others, a 404 page per locale, plus robots.txt,
// sitemap.xml and _headers.
//
// No framework, no bundler, no dependencies. src/ is split into focused files:
// page.html and not-found.html are the shells, {{> path.html}} pulls in a partial
// (relative to src/, recursive), and {{__styles__}} / {{__script__}} inline the
// stylesheets and scripts — so each generated page is self-contained and costs a
// single HTTP request.
//
// Runs with plain node and no node_modules, because the `build` block in
// wrangler.jsonc re-runs it during `wrangler deploy` where nothing is installed.
//
// Adding a locale: translate content/<code>.json, then add an entry to LOCALES.
// The language switcher, hreflang links and sitemap all follow automatically.
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(__dirname, '..');
const SRC = path.join(ROOT, 'src');
const CONTENT = path.join(ROOT, 'content');
const ASSETS = path.join(ROOT, 'assets');
const OUT = path.join(ROOT, 'public');

// Public origin of the site, used for canonical, hreflang, og:url, the sitemap and the
// JSON-LD. The apex is the canonical host; www redirects to it at the zone level.
// Changing this one constant moves every published URL.
const SITE_URL = 'https://husonym.com';

// Public site key of the Cloudflare Turnstile widget (the matching secret lives in
// the Worker as TURNSTILE_SECRET_KEY). Left unset, the widget is not rendered at all
// and its script is not loaded: the form stays usable locally and on previews, and
// the Worker only verifies when ITS secret is set. The two are configured together
// or not at all.
const TURNSTILE_SITE_KEY = process.env.HUSONYM_TURNSTILE_SITE_KEY || '';

const LOCALES = [
  { code: 'fr', name: 'Français', urlPath: '/', ogLocale: 'fr_FR', default: true },
  { code: 'en', name: 'English', urlPath: '/en/', ogLocale: 'en_US' },
];
const BASE_LOCALE = 'fr';

// Inlining order of the stylesheets, following the page. `mask` comes last: it
// overrides the solid section backgrounds so the depth gradient shows through.
const PAGE_STYLES = [
  'base', 'nav', 'hero', 'problem', 'steps', 'features', 'connectors',
  'trust', 'sovereignty', 'pricing', 'cta', 'contact', 'footer', 'mask',
];
const NOT_FOUND_STYLES = ['base', 'not-found'];

// Scripts inlined into the page, concatenated in order. `main` drives the scroll
// reveal, `mask` the anonymizing canvas, `contact` the form.
const PAGE_SCRIPTS = ['main', 'nav', 'mask', 'contact'];

const STRICT = process.argv.includes('--strict') || process.env.CI === 'true';
const CHECK_ONLY = process.argv.includes('--check-locales');

// ── content catalogues ──────────────────────────────────────────────────────

// Flattens a nested object into dotted keys: { hero: { title: "…" } } becomes
// { "hero.title": "…" }. Catalogues stay readable and grouped by section while
// render() only ever sees a flat lookup table. An already-flat file passes through
// unchanged, so both shapes can coexist — even in the same file.
function flatten(node, prefix = '', out = {}) {
  for (const [key, value] of Object.entries(node)) {
    const full = prefix ? `${prefix}.${key}` : key;
    if (value && typeof value === 'object' && !Array.isArray(value)) flatten(value, full, out);
    else if (typeof value === 'string') out[full] = value;
    else throw new Error(`[build] non-string value for "${full}" — catalogues hold strings only`);
  }
  return out;
}

function loadCatalog(code) {
  const file = path.join(CONTENT, `${code}.json`);
  return flatten(JSON.parse(fs.readFileSync(file, 'utf8')));
}

const CATALOGS = Object.fromEntries(LOCALES.map((l) => [l.code, loadCatalog(l.code)]));
const BASE = CATALOGS[BASE_LOCALE];

// ── templating ──────────────────────────────────────────────────────────────

function readSrc(rel) {
  return fs.readFileSync(path.join(SRC, rel), 'utf8');
}

// Resolves {{> path.html}} until exhausted: a partial may itself include others
// (features.html pulls in features/*.html). The depth bound breaks any cycle.
function resolveIncludes(html) {
  for (let depth = 0; html.includes('{{>'); depth++) {
    if (depth > 10) throw new Error('[build] includes nested too deep — probable cycle');
    html = html.replace(/\{\{>\s*([\w./-]+)\s*\}\}/g, (_, rel) => readSrc(rel).trimEnd());
  }
  return html;
}

// Strips HTML comments from the markup. Applied to the shell AFTER includes are
// resolved but BEFORE {{__styles__}} and {{__script__}} are substituted, so CSS and JS
// comments survive untouched — only the markup ones go.
//
// This is not about saving bytes. Source comments explain the machinery to whoever
// views the page source, and at least one of them gives away the contact form's
// honeypot field, which is precisely the sort of note that should not ship.
function stripHtmlComments(html) {
  return html.replace(/<!--[\s\S]*?-->\n?/g, '');
}

function concatStyles(names) {
  return names.map((n) => readSrc(path.join('styles', `${n}.css`)).trimEnd()).join('\n\n');
}

function concatScripts(names) {
  return names.map((n) => readSrc(`${n}.js`).trimEnd()).join('\n\n');
}

// Single quotes are escaped too: catalogue values also land inside attributes
// (alt, aria-label, content, title) and French copy is full of apostrophes.
// Attributes must stay double-quoted for this to be sufficient.
function escapeHtml(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

let problems = 0;
const used = new Set();

// Assembly tokens ({{__x__}}) are substituted before content tokens, otherwise the
// content pass would mistake them for missing catalogue keys.
function render(template, locale, assemblyTokens) {
  let html = template;
  for (const [token, value] of Object.entries(assemblyTokens)) {
    html = html.replaceAll(`{{${token}}}`, value);
  }

  return html.replace(/\{\{([\w.]+)\}\}/g, (match, key) => {
    let value = CATALOGS[locale.code][key];
    if (value === undefined) {
      value = BASE[key];
      if (value === undefined) {
        // Wording kept verbatim: the CI greps for this string.
        console.warn(`[build] clé manquante "${key}" (locale "${locale.code}")`);
        problems++;
        return match; // left visible in the output, so it is caught by eye too
      }
      console.warn(`[build] missing translation "${key}" (locale "${locale.code}") — falling back to ${BASE_LOCALE}`);
      problems++;
    }
    used.add(key);
    // A key ending in Html carries markup on purpose (<br>, <em>) and is injected raw.
    return key.endsWith('Html') ? value : escapeHtml(value);
  });
}

// ── head fragments ──────────────────────────────────────────────────────────

function renderTurnstileWidget() {
  if (!TURNSTILE_SITE_KEY) return '';
  return `<div class="cf-turnstile" data-sitekey="${escapeHtml(TURNSTILE_SITE_KEY)}" data-theme="dark"></div>`;
}

function renderTurnstileScript() {
  if (!TURNSTILE_SITE_KEY) return '';
  return '<script src="https://challenges.cloudflare.com/turnstile/v0/api.js" async defer></script>';
}

// Segmented FR | EN switcher: the current language is a non-clickable marker, the
// others are links. Both languages stay visible at a glance.
function renderLangSwitcher(currentCode) {
  return LOCALES.map((l) => {
    const label = l.code.toUpperCase();
    if (l.code === currentCode) {
      return `<span class="is-current" aria-current="true">${label}</span>`;
    }
    return `<a href="${l.urlPath}" hreflang="${l.code}" lang="${l.code}" title="${escapeHtml(l.name)}">${label}</a>`;
  }).join('');
}

function renderHreflangLinks() {
  const links = LOCALES.map(
    (l) => `<link rel="alternate" hreflang="${l.code}" href="${SITE_URL}${l.urlPath}" />`
  );
  const def = LOCALES.find((l) => l.default);
  links.push(`<link rel="alternate" hreflang="x-default" href="${SITE_URL}${def.urlPath}" />`);
  return links.join('\n');
}

// Built from an object and serialised, never hand-assembled — the escaping of `</`
// is what keeps a stray sequence in the copy from closing the script element early.
function renderJsonLd(locale) {
  const t = (key) => CATALOGS[locale.code][key] ?? BASE[key] ?? '';
  const orgId = `${SITE_URL}/#organization`;
  const graph = {
    '@context': 'https://schema.org',
    '@graph': [
      {
        '@type': 'Organization',
        '@id': orgId,
        name: 'Fishtre Compagnie',
        url: SITE_URL,
        // assetMap is populated before this runs (renderJsonLd is only called from the
        // render loop, which comes after emitAssets).
        logo: SITE_URL + rewriteAssetUrls('/assets/logo.png', assetMap),
        contactPoint: {
          '@type': 'ContactPoint',
          email: 'contact@husonym.com',
          contactType: 'sales',
        },
      },
      {
        '@type': 'SoftwareApplication',
        name: 'Husonym',
        applicationCategory: 'DeveloperApplication',
        description: t('meta.description'),
        url: `${SITE_URL}${locale.urlPath}`,
        publisher: { '@id': orgId },
        // No `offers` node: the product is sold on quote and no price is published.
        // Never reintroduce a price of 0 here — it would advertise the software as
        // free in search results.
      },
      {
        '@type': 'WebSite',
        url: `${SITE_URL}${locale.urlPath}`,
        name: 'Husonym',
        inLanguage: locale.code,
        publisher: { '@id': orgId },
      },
    ],
  };
  return JSON.stringify(graph).replaceAll('</', '<\\/');
}

// ── content-hashed assets ───────────────────────────────────────────────────

// Copies assets/ into public/assets/ under content-hashed names and returns a map
// from the authored path to the emitted one:
//   '/assets/mascot.png' → '/assets/mascot.3f2a1c8e.png'
//
// Authors keep writing the plain path in markup and CSS; rewriteAssetUrls() swaps it
// at build time. The point is that the URL changes whenever the bytes change, which
// is the only thing that makes a long cache safe: a stale copy can never be served
// under a name that now means something else, and a redesign propagates instantly
// instead of waiting out someone's cache.
function emitAssets() {
  const map = new Map();
  const walk = (dir, rel = '') => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const abs = path.join(dir, entry.name);
      const relPath = rel ? `${rel}/${entry.name}` : entry.name;
      if (entry.isDirectory()) { walk(abs, relPath); continue; }
      const bytes = fs.readFileSync(abs);
      const hash = crypto.createHash('sha256').update(bytes).digest('hex').slice(0, 8);
      const ext = path.extname(relPath);
      const hashed = `${relPath.slice(0, -ext.length)}.${hash}${ext}`;
      const dest = path.join(OUT, 'assets', hashed);
      fs.mkdirSync(path.dirname(dest), { recursive: true });
      fs.writeFileSync(dest, bytes);
      map.set(`/assets/${relPath}`, `/assets/${hashed}`);
    }
  };
  fs.rmSync(path.join(OUT, 'assets'), { recursive: true, force: true });
  walk(ASSETS);
  console.log(`[build] ${map.size} asset(s) → public/assets/ (content-hashed)`);
  return map;
}

// Longest path first, so '/assets/fonts/x.woff2' is never partially matched by a
// shorter entry that happens to be its prefix.
function rewriteAssetUrls(text, map) {
  let out = text;
  for (const from of [...map.keys()].sort((a, b) => b.length - a.length)) {
    out = out.replaceAll(from, map.get(from));
  }
  return out;
}

// ── emitters ────────────────────────────────────────────────────────────────

function writeOut(locale, filename, html) {
  const outDir = path.join(OUT, locale.default ? '.' : locale.code);
  fs.mkdirSync(outDir, { recursive: true });
  const outFile = path.join(outDir, filename);
  fs.writeFileSync(outFile, html, 'utf8');
  console.log(`[build] ${locale.code} → ${path.relative(ROOT, outFile)}`);
}

function writeRoot(filename, body) {
  fs.mkdirSync(OUT, { recursive: true });
  fs.writeFileSync(path.join(OUT, filename), body, 'utf8');
  console.log(`[build] → public/${filename}`);
}

function writeRobots() {
  writeRoot('robots.txt', `User-agent: *\nAllow: /\n\nSitemap: ${SITE_URL}/sitemap.xml\n`);
}

function writeSitemap() {
  const lastmod = new Date().toISOString().slice(0, 10);
  const alternates = LOCALES.map(
    (l) => `    <xhtml:link rel="alternate" hreflang="${l.code}" href="${SITE_URL}${l.urlPath}" />`
  );
  alternates.push(
    `    <xhtml:link rel="alternate" hreflang="x-default" href="${SITE_URL}${LOCALES.find((l) => l.default).urlPath}" />`
  );
  const urls = LOCALES.map((l) => [
    '  <url>',
    `    <loc>${SITE_URL}${l.urlPath}</loc>`,
    ...alternates,
    `    <lastmod>${lastmod}</lastmod>`,
    '  </url>',
  ].join('\n'));
  writeRoot('sitemap.xml', [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">',
    ...urls,
    '</urlset>',
    '',
  ].join('\n'));
}

function sha256(value) {
  return `'sha256-${crypto.createHash('sha256').update(value, 'utf8').digest('base64')}'`;
}

// Everything is inlined, so the build already holds the exact bytes of every
// <style> and <script> block and can hash them — a CSP without 'unsafe-inline',
// which a bundler-based site cannot get this cheaply. The shells keep the tokens
// flush against their tags ( <style>{{__styles__}}</style> ) precisely so that the
// hashed string is the block content, byte for byte.
//
// style-src-attr stays 'unsafe-inline': a handful of style="" attributes carry data
// (distribution bar widths). Inline style attributes cannot execute script, so this
// is a far smaller concession than 'unsafe-inline' on script-src would be.
function writeHeaders(hashes) {
  const turnstile = 'https://challenges.cloudflare.com';
  const csp = [
    "default-src 'self'",
    `script-src 'self' ${hashes.scripts.join(' ')} ${turnstile}`,
    `style-src 'self' ${hashes.styles.join(' ')}`,
    "style-src-attr 'unsafe-inline'",
    `frame-src ${turnstile}`,
    "img-src 'self' data:",
    `connect-src 'self' ${turnstile}`,
    "font-src 'self'",
    "base-uri 'none'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ].join('; ');

  // Safe to cache hard: every filename under /assets/ carries a hash of its own
  // bytes (see emitAssets), so a changed file is a changed URL and nothing stale can
  // ever be served under a name that now means something else.
  writeRoot('_headers', [
    '/assets/*',
    '  Cache-Control: public, max-age=31536000, immutable',
    '/*',
    '  X-Content-Type-Options: nosniff',
    '  Referrer-Policy: strict-origin-when-cross-origin',
    '  Permissions-Policy: geolocation=(), microphone=(), camera=()',
    `  Content-Security-Policy: ${csp}`,
    '',
  ].join('\n'));
}

// ── locale parity check ─────────────────────────────────────────────────────

function checkLocales() {
  const baseKeys = new Set(Object.keys(BASE));
  let failed = false;
  for (const locale of LOCALES) {
    if (locale.code === BASE_LOCALE) continue;
    const keys = new Set(Object.keys(CATALOGS[locale.code]));
    const missing = [...baseKeys].filter((k) => !keys.has(k));
    const extra = [...keys].filter((k) => !baseKeys.has(k));
    if (missing.length) {
      failed = true;
      console.error(`[build] ${locale.code}: ${missing.length} key(s) missing vs ${BASE_LOCALE}:\n  ${missing.join('\n  ')}`);
    }
    if (extra.length) {
      failed = true;
      console.error(`[build] ${locale.code}: ${extra.length} key(s) absent from ${BASE_LOCALE}:\n  ${extra.join('\n  ')}`);
    }
  }
  if (!failed) console.log(`[build] locales in sync (${baseKeys.size} keys)`);
  return failed;
}

if (CHECK_ONLY) {
  process.exit(checkLocales() ? 1 : 0);
}

// ── build ───────────────────────────────────────────────────────────────────

const pageTemplate = stripHtmlComments(resolveIncludes(readSrc('page.html')));
const notFoundTemplate = stripHtmlComments(resolveIncludes(readSrc('not-found.html')));

// Assets are emitted first: everything downstream — markup, the inlined CSS, the OG
// URL, the JSON-LD logo — has to reference the hashed names, and the CSP style hash
// must be computed on the CSS *after* its font URL has been rewritten.
const assetMap = emitAssets();
const rewrite = (text) => rewriteAssetUrls(text, assetMap);

const pageTemplate2 = rewrite(pageTemplate);
const notFoundTemplate2 = rewrite(notFoundTemplate);
const pageStyles = rewrite(concatStyles(PAGE_STYLES));
const notFoundStyles = rewrite(concatStyles(NOT_FOUND_STYLES));
const script = rewrite(concatScripts(PAGE_SCRIPTS));
const year = String(new Date().getFullYear());
const ogImage = SITE_URL + rewrite('/assets/og.png');

for (const locale of LOCALES) {
  const alt = LOCALES.filter((l) => l.code !== locale.code).map((l) => l.ogLocale).join(',');
  writeOut(locale, 'index.html', render(pageTemplate2, locale, {
    __lang__: locale.code,
    __year__: year,
    __canonical__: `${SITE_URL}${locale.urlPath}`,
    __hreflangLinks__: renderHreflangLinks(),
    __langSwitcher__: renderLangSwitcher(locale.code),
    __ogImage__: ogImage,
    __ogLocale__: locale.ogLocale,
    __ogLocaleAlt__: alt,
    __jsonLd__: renderJsonLd(locale),
    __turnstileScript__: renderTurnstileScript(),
    __turnstileWidget__: renderTurnstileWidget(),
    __styles__: pageStyles,
    __script__: script,
  }));

  // Served by Cloudflare (not_found_handling = "404-page"): the closest 404.html to
  // the requested path, so /en/* lands on the English one.
  writeOut(locale, '404.html', render(notFoundTemplate2, locale, {
    __lang__: locale.code,
    __homePath__: locale.urlPath,
    __styles__: notFoundStyles,
  }));
}

writeRobots();
writeSitemap();
writeHeaders({ styles: [sha256(pageStyles), sha256(notFoundStyles)], scripts: [sha256(script)] });

const unused = Object.keys(BASE).filter((k) => !used.has(k));
if (unused.length) {
  console.warn(`[build] keys never referenced by a template: ${unused.join(', ')}`);
}
if (problems) {
  console.warn(`[build] ${problems} content problem(s)`);
  // Doubles the CI grep with an exit code: greps over a log are brittle, this is not.
  if (STRICT) process.exitCode = 1;
}
