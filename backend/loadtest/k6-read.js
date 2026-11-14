// Read-path load test for the REDXTUBE API (k6: https://k6.io).
//
//   k6 run -e BASE_URL=http://localhost:8080 -e ORIGIN=http://localhost:3000 loadtest/k6-read.js
//   k6 run -e BASE_URL=https://api.example.com -e ORIGIN=https://example.com -e VUS=200 -e DURATION=5m loadtest/k6-read.js
//
// Each virtual user passes the age gate once, then browses like a viewer: feed, trending, search,
// video detail and the first playlist + segment of the signed stream. Run it against a staging
// environment with realistic data (an empty database makes every query look fast). Thresholds fail
// the run (non-zero exit) so it can gate a release.
import http from "k6/http";
import { check, group, sleep } from "k6";
import { Trend } from "k6/metrics";

const BASE = __ENV.BASE_URL || "http://localhost:8080";
const ORIGIN = __ENV.ORIGIN || "http://localhost:3000";
const API = `${BASE}/api/v1`;
const streamStart = new Trend("stream_start_ms", true); // detail -> first segment, what a viewer feels

export const options = {
  scenarios: {
    browse: {
      executor: "ramping-vus",
      startVUs: 1,
      stages: [
        { duration: "30s", target: Number(__ENV.VUS || 50) },
        { duration: __ENV.DURATION || "2m", target: Number(__ENV.VUS || 50) },
        { duration: "20s", target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    "http_req_duration{group:::feed}": ["p(95)<300"],
    "http_req_duration{group:::detail}": ["p(95)<400"],
    "http_req_duration{group:::search}": ["p(95)<600"],
    stream_start_ms: ["p(95)<1500"],
  },
};

const json = { headers: { "Content-Type": "application/json", Origin: ORIGIN } };

export default function () {
  // the age gate: a signed cookie, kept in the VU's cookie jar
  const age = http.post(`${API}/age/verify`, JSON.stringify({ confirmed: true }), json);
  check(age, { "age gate passed": (r) => r.status === 204 });

  let ids = [];
  group("feed", () => {
    const r = http.get(`${API}/videos`, { tags: { name: "GET /videos" } });
    check(r, { "feed 200": (x) => x.status === 200 });
    try { ids = (r.json("items") || []).map((v) => v.id); } catch (_) {}
    check(http.get(`${API}/videos/trending`, { tags: { name: "GET /videos/trending" } }), { "trending 200": (x) => x.status === 200 });
  });

  group("search", () => {
    const r = http.get(`${API}/search?q=a`, { tags: { name: "GET /search" } });
    check(r, { "search 200": (x) => x.status === 200 });
  });

  if (ids.length > 0) {
    group("detail", () => {
      const id = ids[Math.floor(Math.random() * ids.length)];
      const t0 = Date.now();
      const r = http.get(`${API}/videos/${id}`, { tags: { name: "GET /videos/{id}" } });
      check(r, { "detail 200": (x) => x.status === 200 });
      const url = r.status === 200 ? r.json("streamUrl") : null;
      if (url) {
        const master = http.get(url, { tags: { name: "GET stream master" } });
        check(master, { "master 200": (x) => x.status === 200 });
        const rend = (master.body || "").split("\n").find((l) => l.endsWith(".m3u8"));
        if (rend) {
          const base = url.substring(0, url.lastIndexOf("/") + 1);
          const pl = http.get(base + rend, { tags: { name: "GET stream rendition" } });
          const seg = (pl.body || "").split("\n").find((l) => l.endsWith(".ts"));
          if (seg) {
            const s = http.get(base + rend.substring(0, rend.lastIndexOf("/") + 1) + seg, { tags: { name: "GET stream segment" } });
            check(s, { "segment 200": (x) => x.status === 200 });
            streamStart.add(Date.now() - t0);
          }
        }
      }
    });
  }
  sleep(1 + Math.random() * 2);
}
