import { isIP } from 'node:net';

/**
 * Whether an address is one the internet routes to: a deny-by-default list, the same as
 * the backend's (safehttp.IsPublicAddress). IPv4 written as IPv6 is unwrapped first.
 */
export function isPublicAddress(address: string): boolean {
  const v4 = toIPv4(address);
  if (v4) {
    const [a, b] = v4;
    return !(
      a === 0 || // unspecified, "this network"
      a === 10 || // private
      a === 127 || // loopback
      (a === 169 && b === 254) || // link-local -- cloud metadata lives here
      (a === 172 && b >= 16 && b <= 31) || // private
      (a === 192 && b === 168) || // private
      (a === 100 && b >= 64 && b <= 127) || // carrier-grade NAT
      a >= 224 // multicast and reserved
    );
  }
  const v6 = toIPv6Groups(address);
  if (!v6) {
    return false;
  }
  if (v6.every((g) => g === 0)) {
    return false; // unspecified
  }
  if (v6.slice(0, 7).every((g) => g === 0) && v6[7] === 1) {
    return false; // loopback
  }
  const first = v6[0];
  if ((first & 0xfe00) === 0xfc00) {
    return false; // unique local
  }
  if ((first & 0xffc0) === 0xfe80) {
    return false; // link-local
  }
  if ((first & 0xff00) === 0xff00) {
    return false; // multicast
  }
  // 2002::/16 (6to4) and 64:ff9b::/96 (NAT64) carry an IPv4 address.
  if (first === 0x2002) {
    return isPublicAddress(groupsToIPv4(v6[1], v6[2]));
  }
  if (
    first === 0x64 &&
    v6[1] === 0xff9b &&
    v6.slice(2, 6).every((g) => g === 0)
  ) {
    return isPublicAddress(groupsToIPv4(v6[6], v6[7]));
  }
  return true;
}

function groupsToIPv4(high: number, low: number): string {
  return `${high >> 8}.${high & 0xff}.${low >> 8}.${low & 0xff}`;
}

// The four bytes of an IPv4 address, also when written as IPv4-mapped IPv6.
function toIPv4(address: string): number[] | null {
  const mapped = address.toLowerCase().match(/^::ffff:(\d+\.\d+\.\d+\.\d+)$/);
  const candidate = mapped ? mapped[1] : address;
  if (isIP(candidate) === 4) {
    return candidate.split('.').map(Number);
  }
  const groups = toIPv6Groups(address);
  // ::ffff:a.b.c.d written in hexadecimal groups.
  if (
    groups &&
    groups.slice(0, 5).every((g) => g === 0) &&
    groups[5] === 0xffff
  ) {
    return groupsToIPv4(groups[6], groups[7]).split('.').map(Number);
  }
  return null;
}

// The eight 16-bit groups of an IPv6 address.
function toIPv6Groups(address: string): number[] | null {
  if (isIP(address) !== 6) {
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
  const tailGroups = tail !== undefined && tail ? tail.split(':') : [];
  const missing = 8 - headGroups.length - tailGroups.length;
  const all =
    tail === undefined
      ? headGroups
      : [...headGroups, ...Array(missing).fill('0'), ...tailGroups];
  return all.length === 8 ? all.map((g) => parseInt(g, 16)) : null;
}
