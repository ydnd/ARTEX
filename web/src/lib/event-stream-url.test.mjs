import assert from "node:assert/strict";
import test from "node:test";
import { eventStreamUrl } from "./event-stream-url.ts";

test("same-origin stream URLs never expose the session token", () => {
  assert.equal(eventStreamUrl("/api/logs/stream", "", "https://history.example.org/logs", "private-sentinel"), "/api/logs/stream");
  assert.equal(eventStreamUrl("/api/logs/stream?since=1", "https://history.example.org", "https://history.example.org/logs", "private-sentinel"), "https://history.example.org/api/logs/stream?since=1");
});

test("cross-origin development keeps the existing encoded token fallback", () => {
  const actual = eventStreamUrl("/api/logs/stream?since=1", "http://localhost:8787", "http://localhost:5173/logs", "synthetic+token");
  const parsed = new URL(actual);
  assert.equal(parsed.searchParams.get("since"), "1");
  assert.equal(parsed.searchParams.get("token"), "synthetic+token");
});

test("unauthenticated and server-rendered stream URLs do not invent credentials", () => {
  assert.equal(eventStreamUrl("/api/logs/stream", "", undefined, null), "/api/logs/stream");
});
