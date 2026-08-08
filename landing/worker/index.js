// Entry point for the husonym.com Worker.
//
// Almost every request is served straight from Workers Static Assets without ever
// reaching this code. Only /api/* is routed here first (assets.run_worker_first in
// wrangler.jsonc), which is what makes the contact endpoint possible on an otherwise
// static site.
//
// The final env.ASSETS.fetch() matters more than it looks: declaring run_worker_first
// as an array opts out of Cloudflare's Sec-Fetch-Mode handling, so a navigation to an
// unknown path reaches the Worker instead of going straight to not_found_handling.
// Handing the request back to the asset server is what still produces the localised
// 404 page.
import { handleContact } from './contact.js';
import { json } from './http.js';

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);

    if (url.pathname === '/api/contact') return handleContact(request, env, ctx);

    // Any other /api/* path is an API miss, not a missing page: answer in JSON rather
    // than serving the marketing 404 to something that asked for data.
    if (url.pathname.startsWith('/api/')) return json({ error: 'not_found' }, 404);

    return env.ASSETS.fetch(request);
  },
};
