// Composes and sends the lead notification through Cloudflare Email Sending.
//
// Prerequisite, once per account: `npx wrangler email sending enable husonym.com`,
// plus the SPF/DKIM/DMARC records it asks for. Without it every send fails with
// E_SENDER_NOT_VERIFIED.

export async function sendContactMail(env, input, meta) {
  const subject = `[husonym.com] Contact — ${input.name}${input.company ? ` (${input.company})` : ''}`;

  const fields = [
    ['Name', input.name],
    ['Email', input.email],
    ['Company', input.company || '—'],
    ['Locale', meta.locale],
    ['Page', meta.page],
    ['Country', meta.country || '—'],
    ['IP', meta.ip],
    ['Received', new Date().toISOString()],
  ];

  const text = [
    ...fields.map(([label, value]) => `${label}: ${value}`),
    '',
    '---',
    '',
    input.message,
  ].join('\n');

  const html = [
    '<table style="font:14px system-ui,sans-serif;border-collapse:collapse">',
    ...fields.map(
      ([label, value]) =>
        `<tr><td style="padding:2px 12px 2px 0;color:#666">${esc(label)}</td>` +
        `<td style="padding:2px 0"><strong>${esc(value)}</strong></td></tr>`
    ),
    '</table>',
    '<hr style="margin:16px 0;border:0;border-top:1px solid #ddd" />',
    `<div style="font:14px system-ui,sans-serif;white-space:pre-wrap">${esc(input.message)}</div>`,
  ].join('');

  await env.CONTACT_MAILER.send({
    to: env.CONTACT_TO,
    // Must be an address on the verified sending domain. Putting the visitor's address
    // here instead would fail DMARC and get the mail silently dropped — their address
    // goes in Reply-To, and only there.
    from: { email: env.CONTACT_FROM, name: env.CONTACT_FROM_NAME },
    replyTo: input.email,
    subject,
    text,
    html,
  });
}

function esc(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;');
}
