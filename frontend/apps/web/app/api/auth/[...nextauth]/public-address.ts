import { isIP } from 'node:net';

/**
 * Whether an address is one the internet routes to: a deny-by-default list. IPv4 written
 * in one of IPv6's forms -- mapped, translated, NAT64, 6to4 -- is unwrapped first, or
 * ::ffff:127.0.0.1 would walk straight past a check written for IPv4.
 *
 * The backend holds the same table (safehttp.IsPublicAddress); a change here is a change
 * there.
 */
export function isPublicAddress(address: string): boolean {
  let bytes = toBytes(address);
  if (!bytes) {
    return false;
  }
  if (bytes.length === 16) {
    const v4 = embeddedIPv4(bytes);
    if (v4) {
      bytes = v4;
    }
  }
  const blocked = bytes.length === 4 ? BLOCKED_V4 : BLOCKED_V6;
  return !blocked.some(([prefix, bits]) => matches(bytes!, prefix, bits));
}

// The ranges that are not the public internet: this host, its networks, the cloud's
// metadata service, and what no one routes to.
const BLOCKED_V4: [number[], number][] = [
  [[0, 0, 0, 0], 8], // "this network"
  [[10, 0, 0, 0], 8], // private
  [[100, 64, 0, 0], 10], // carrier-grade NAT, where cluster addressing ends up
  [[127, 0, 0, 0], 8], // loopback
  [[169, 254, 0, 0], 16], // link-local -- cloud metadata lives here
  [[172, 16, 0, 0], 12], // private
  [[192, 0, 0, 0], 24], // IETF protocol assignments
  [[192, 168, 0, 0], 16], // private
  [[198, 18, 0, 0], 15], // benchmarking
  [[224, 0, 0, 0], 4], // multicast
  [[240, 0, 0, 0], 4], // reserved, broadcast
];

const BLOCKED_V6: [number[], number][] = [
  [v6('::'), 96], // unspecified, loopback, IPv4-compatible
  [v6('64:ff9b:1::'), 48], // local-use NAT64
  [v6('fc00::'), 7], // unique local
  [v6('fe80::'), 10], // link-local
  [v6('fec0::'), 10], // site-local
  [v6('ff00::'), 8], // multicast
];

// The IPv4 address an IPv6 one carries: mapped (::ffff:a.b.c.d), SIIT (::ffff:0:a.b.c.d),
// NAT64 (64:ff9b::/96) and 6to4 (2002::/16).
function embeddedIPv4(bytes: number[]): number[] | null {
  if (
    matches(bytes, v6('::ffff:0:0'), 96) ||
    matches(bytes, v6('::ffff:0:0:0'), 96) ||
    matches(bytes, v6('64:ff9b::'), 96)
  ) {
    return bytes.slice(12);
  }
  if (matches(bytes, v6('2002::'), 16)) {
    return bytes.slice(2, 6);
  }
  return null;
}

function matches(bytes: number[], prefix: number[], bits: number): boolean {
  for (let i = 0; i < bits; i++) {
    const bit = (b: number[]) => (b[i >> 3] >> (7 - (i & 7))) & 1;
    if (bit(bytes) !== bit(prefix)) {
      return false;
    }
  }
  return true;
}

function v6(address: string): number[] {
  return toBytes(address)!;
}

// The bytes of an address: 4 for IPv4, 16 for IPv6; null for anything else.
function toBytes(address: string): number[] | null {
  const kind = isIP(address);
  if (kind === 4) {
    return address.split('.').map(Number);
  }
  if (kind !== 6) {
    return null;
  }
  let text = address.toLowerCase();
  const dotted = text.match(/(\d+\.\d+\.\d+\.\d+)$/);
  if (dotted) {
    const [a, b, c, d] = dotted[1].split('.').map(Number);
    text = text.replace(
      dotted[1],
      `${((a << 8) | b).toString(16)}:${((c << 8) | d).toString(16)}`
    );
  }
  const [head, tail] = text.split('::');
  const headGroups = head ? head.split(':') : [];
  const tailGroups = tail ? tail.split(':') : [];
  const groups =
    tail === undefined
      ? headGroups
      : [
          ...headGroups,
          ...Array(8 - headGroups.length - tailGroups.length).fill('0'),
          ...tailGroups,
        ];
  if (groups.length !== 8) {
    return null;
  }
  return groups.flatMap((g) => {
    const n = parseInt(g, 16);
    return [n >> 8, n & 0xff];
  });
}
