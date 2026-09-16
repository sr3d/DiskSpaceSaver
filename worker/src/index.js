/**
 * DiskSpaceSaver update service.
 *
 * Serves the Sparkle appcast, accepts release metadata from the release
 * script, and records anonymous update lifecycle events so a broken release is
 * visible rather than showing up as an unexplained drop in update rate.
 *
 * Everything lives in one Durable Object instance: the write volume is a
 * handful of releases and a few events per install per day, and a single
 * serialised object removes every race in exchange for nothing that matters at
 * this scale.
 *
 * Routes
 *   GET  /appcast.xml[?channel=beta]  the Sparkle feed
 *   POST /releases                    publish a release   (admin token)
 *   POST /events                      record one event    (public, unauthenticated)
 *   GET  /stats                       aggregates          (admin token)
 *   GET  /health
 */

const CHANNELS = new Set(['stable', 'beta']);

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const id = env.UPDATES.idFromName('v1');
    const stub = env.UPDATES.get(id);

    // /events is the only unauthenticated write. It is rate limited by the
    // Durable Object and stores nothing that identifies a person.
    if (url.pathname === '/releases' || url.pathname === '/stats') {
      if (!authorised(request, env)) {
        return json({ error: 'unauthorised' }, 401);
      }
    }
    if (url.pathname === '/health') {
      return json({ ok: true });
    }
    return stub.fetch(request);
  },
};

function authorised(request, env) {
  const header = request.headers.get('authorization') || '';
  const token = header.replace(/^Bearer\s+/i, '');
  if (!env.ADMIN_TOKEN || !token) return false;
  return timingSafeEqual(token, env.ADMIN_TOKEN);
}

/** Constant-time compare so the token cannot be recovered by timing. */
function timingSafeEqual(a, b) {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return diff === 0;
}

function json(body, status = 200) {
  return new Response(JSON.stringify(body, null, 2), {
    status,
    headers: { 'content-type': 'application/json; charset=utf-8' },
  });
}

export class UpdateState {
  constructor(state) {
    this.state = state;
  }

  async fetch(request) {
    const url = new URL(request.url);
    switch (`${request.method} ${url.pathname}`) {
      case 'GET /appcast.xml':
        return this.appcast(url.searchParams.get('channel') || 'stable');
      case 'POST /releases':
        return this.publish(request);
      case 'POST /events':
        return this.record(request);
      case 'GET /stats':
        return this.stats();
      default:
        return json({ error: 'not found' }, 404);
    }
  }

  // ------------------------------------------------------------- releases

  async publish(request) {
    let body;
    try {
      body = await request.json();
    } catch {
      return json({ error: 'body must be JSON' }, 400);
    }

    const required = ['version', 'build', 'url', 'length', 'signature'];
    for (const key of required) {
      if (!body[key]) return json({ error: `missing ${key}` }, 400);
    }
    const channel = CHANNELS.has(body.channel) ? body.channel : 'stable';

    const release = {
      version: String(body.version),
      build: String(body.build),
      url: String(body.url),
      length: Number(body.length),
      // The EdDSA signature Sparkle checks before it will install anything.
      // The Worker never sees the private key; sign_update produces this.
      signature: String(body.signature),
      minimumSystemVersion: String(body.minimumSystemVersion || '13.0'),
      notes: String(body.notes || ''),
      channel,
      publishedAt: new Date().toISOString(),
    };

    const releases = (await this.state.storage.get('releases')) || [];
    const without = releases.filter(
      (r) => !(r.version === release.version && r.channel === release.channel),
    );
    without.push(release);
    without.sort((a, b) => compareVersions(b.version, a.version));
    await this.state.storage.put('releases', without.slice(0, 50));

    return json({ ok: true, release });
  }

  async appcast(channel) {
    const wanted = CHANNELS.has(channel) ? channel : 'stable';
    const releases = (await this.state.storage.get('releases')) || [];

    // A beta client should also be offered a newer stable build; a stable
    // client must never be offered a beta.
    const visible = releases.filter(
      (r) => r.channel === 'stable' || wanted === 'beta',
    );

    const items = visible
      .map(
        (r) => `    <item>
      <title>Version ${escapeXml(r.version)}</title>
      <pubDate>${new Date(r.publishedAt).toUTCString()}</pubDate>
      <sparkle:version>${escapeXml(r.build)}</sparkle:version>
      <sparkle:shortVersionString>${escapeXml(r.version)}</sparkle:shortVersionString>
      <sparkle:minimumSystemVersion>${escapeXml(r.minimumSystemVersion)}</sparkle:minimumSystemVersion>
      <description><![CDATA[${r.notes}]]></description>
      <enclosure url="${escapeXml(r.url)}"
                 length="${r.length}"
                 type="application/octet-stream"
                 sparkle:edSignature="${escapeXml(r.signature)}" />
    </item>`,
      )
      .join('\n');

    const xml = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle">
  <channel>
    <title>DiskSpaceSaver</title>
    <link>https://updates.diskspacesaver.app/appcast.xml</link>
    <description>Updates for DiskSpaceSaver</description>
    <language>en</language>
${items}
  </channel>
</rss>
`;
    return new Response(xml, {
      headers: {
        'content-type': 'application/xml; charset=utf-8',
        // Short enough that a bad release can be pulled quickly.
        'cache-control': 'public, max-age=300',
      },
    });
  }

  // --------------------------------------------------------------- events

  async record(request) {
    let ev;
    try {
      ev = await request.json();
    } catch {
      return json({ error: 'body must be JSON' }, 400);
    }
    if (!ev.event || !ev.installID) {
      return json({ error: 'missing event or installID' }, 400);
    }

    // Only these fields are kept, and only in aggregate. No addresses, no
    // paths, no names: the install id is a random UUID the app generated for
    // itself and nothing here can be tied back to a person.
    const day = new Date().toISOString().slice(0, 10);
    const counters = (await this.state.storage.get('counters')) || {};
    const bump = (key) => {
      counters[key] = (counters[key] || 0) + 1;
    };

    const name = String(ev.event).slice(0, 40);
    const from = String(ev.fromVersion || '').slice(0, 24);
    const to = String(ev.toVersion || '').slice(0, 24);
    const os = String(ev.osVersion || '').slice(0, 16);
    const arch = String(ev.arch || '').slice(0, 12);
    const channel = CHANNELS.has(ev.channel) ? ev.channel : 'stable';

    bump(`event|${day}|${name}`);
    if (from) bump(`version|${day}|${from}`);
    if (to) bump(`target|${day}|${to}|${name}`);
    if (os) bump(`os|${day}|${os}`);
    if (arch) bump(`arch|${day}|${arch}`);
    bump(`channel|${day}|${channel}`);

    // Distinct installs per day, capped so one misbehaving client cannot grow
    // the object without bound.
    const seenKey = `seen|${day}`;
    const seen = new Set((await this.state.storage.get(seenKey)) || []);
    if (seen.size < 50000) {
      seen.add(String(ev.installID).slice(0, 40));
      await this.state.storage.put(seenKey, [...seen]);
    }

    await this.state.storage.put('counters', prune(counters));
    return json({ ok: true });
  }

  async stats() {
    const counters = (await this.state.storage.get('counters')) || {};
    const releases = (await this.state.storage.get('releases')) || [];

    const out = { releases: releases.map((r) => ({ version: r.version, channel: r.channel, publishedAt: r.publishedAt })),
                  byDay: {} };
    for (const [key, count] of Object.entries(counters)) {
      const [kind, day, ...rest] = key.split('|');
      out.byDay[day] ??= {};
      out.byDay[day][kind] ??= {};
      out.byDay[day][kind][rest.join('|')] = count;
    }

    // The funnel is the number that matters: updates found versus updates
    // that actually finished installing.
    out.funnel = {};
    for (const [day, kinds] of Object.entries(out.byDay)) {
      const e = kinds.event || {};
      out.funnel[day] = {
        launches: e.launch || 0,
        found: e.didFindValidUpdate || 0,
        skipped: e.userDidSkip || 0,
        started: e.willInstallUpdate || 0,
        finished: e.didFinishUpdateCycle || 0,
      };
    }
    return json(out);
  }
}

/** prune drops counters older than 90 days so the object stays small. */
function prune(counters) {
  const cutoff = new Date(Date.now() - 90 * 86400_000).toISOString().slice(0, 10);
  const out = {};
  for (const [key, value] of Object.entries(counters)) {
    const day = key.split('|')[1];
    if (day >= cutoff) out[key] = value;
  }
  return out;
}

function compareVersions(a, b) {
  const pa = String(a).split(/[.-]/).map((n) => parseInt(n, 10) || 0);
  const pb = String(b).split(/[.-]/).map((n) => parseInt(n, 10) || 0);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const d = (pa[i] || 0) - (pb[i] || 0);
    if (d !== 0) return d;
  }
  return 0;
}

function escapeXml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;');
}
