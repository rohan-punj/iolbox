/** Format a lowercase colon-separated MAC for Cisco's dotted-triplet display. */
export function colonToDotted(mac: string): string {
  const hex = mac.replaceAll(":", "").toLowerCase();
  if (hex.length !== 12 || !/^[0-9a-f]+$/.test(hex)) return mac;
  return `${hex.slice(0, 4)}.${hex.slice(4, 8)}.${hex.slice(8, 12)}`;
}
