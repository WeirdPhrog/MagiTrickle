export function isValidWildcard(pattern: string): boolean {
  return /^(?!\.)(?!.*\.$)(?!.*\.\.)(?!.*\*\*)[a-zA-Z0-9\-.*?]+$/.test(pattern);
}

export function isValidDomain(pattern: string): boolean {
  return /^(?!\.)(?!.*\.$)(?!.*\.\.)[a-zA-Z0-9\-.]+$/.test(pattern);
}

export function isValidNamespace(pattern: string): boolean {
  return isValidDomain(pattern);
}

export function isValidSubnet(pattern: string): boolean {
  const parts = pattern.split("/");
  return pattern === pattern.trim() && validPrefix(parts, 32) && isValidIPv4(parts[0]);
}

function isValidIPv4(ip: string): boolean {
  const octets = ip.split(".");
  return (
    octets.length === 4 &&
    octets.every((octet) => /^(0|[1-9]\d{0,2})$/.test(octet) && Number(octet) <= 255)
  );
}

function validPrefix(parts: string[], max: number): boolean {
  return (
    parts.length === 1 ||
    (parts.length === 2 && /^(0|[1-9]\d{0,2})$/.test(parts[1]) && Number(parts[1]) <= max)
  );
}

function isValidIPv6(ip: string): boolean {
  // An embedded IPv4 tail occupies two 16-bit groups.
  if (ip.includes(".")) {
    const tail = ip.slice(ip.lastIndexOf(":") + 1);
    if (!isValidIPv4(tail)) return false;
    ip = ip.slice(0, ip.lastIndexOf(":") + 1) + "0:0";
  }
  const halves = ip.split("::");
  if (halves.length > 2) return false;
  const groups = halves.flatMap((half) => (half === "" ? [] : half.split(":")));
  if (!groups.every((group) => /^[0-9a-fA-F]{1,4}$/.test(group))) return false;
  return halves.length === 2 ? groups.length < 8 : groups.length === 8;
}

export function isValidSubnet6(pattern: string): boolean {
  const parts = pattern.split("/");
  return pattern === pattern.trim() && validPrefix(parts, 128) && isValidIPv6(parts[0]);
}

export function isValidRegex(pattern: string): boolean {
  try {
    new RegExp(pattern);
    return true;
  } catch (e) {
    if (e instanceof SyntaxError) {
      return false;
    }
    return false;
  }
}

export const VALIDATOP_MAP: Record<string, (pattern: string) => boolean> = {
  regex: isValidRegex,
  wildcard: isValidWildcard,
  domain: isValidDomain,
  namespace: isValidNamespace,
  subnet: isValidSubnet,
  subnet6: isValidSubnet6,
};

export function isValidRule(rule: { type: string; rule: string }): boolean {
  return (
    rule.rule.length > 0 &&
    Object.hasOwn(VALIDATOP_MAP, rule.type) &&
    VALIDATOP_MAP[rule.type](rule.rule)
  );
}
