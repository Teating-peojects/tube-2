import { env } from "@/config/env";
import { useEffect, useRef } from "react";

type Handler = (data: unknown) => void;

/**
 * Single shared WebSocket. It only connects while something is subscribed, re-subscribes after reconnects
 * (exponential backoff + jitter) and does nothing when NEXT_PUBLIC_WS_URL is empty (polling still works).
 * Protocol: client sends { action: "subscribe" | "unsubscribe", topic }, server sends { topic, data }.
 */
class Realtime {
  private ws: WebSocket | null = null;
  private topics = new Map<string, Set<Handler>>();
  private attempt = 0;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private listening = false;

  subscribe(topic: string, handler: Handler): () => void {
    if (!env.wsUrl || typeof WebSocket === "undefined") return () => {};
    let set = this.topics.get(topic);
    if (!set) { set = new Set(); this.topics.set(topic, set); this.send({ action: "subscribe", topic }); }
    set.add(handler);
    this.connect();
    return () => {
      const s = this.topics.get(topic);
      if (!s) return;
      s.delete(handler);
      if (s.size === 0) { this.topics.delete(topic); this.send({ action: "unsubscribe", topic }); }
      if (this.topics.size === 0) this.close();
    };
  }

  private send(msg: object) {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(msg));
  }

  private connect() {
    if (this.ws || !this.topics.size) return;
    if (!this.listening) { window.addEventListener("online", () => this.connect()); this.listening = true; }
    const ws = new WebSocket(env.wsUrl);
    this.ws = ws;
    ws.onopen = () => { this.attempt = 0; this.topics.forEach((_, topic) => this.send({ action: "subscribe", topic })); };
    ws.onmessage = (ev) => {
      try {
        const m = JSON.parse(String(ev.data)) as { topic?: unknown; data?: unknown };
        if (typeof m.topic === "string") this.topics.get(m.topic)?.forEach((h) => h(m.data));
      } catch { /* ignore malformed frames */ }
    };
    ws.onclose = () => {
      this.ws = null;
      if (!this.topics.size) return;
      const delay = Math.min(30_000, 1000 * 2 ** this.attempt++) + Math.random() * 500;
      this.timer = setTimeout(() => this.connect(), delay);
    };
    ws.onerror = () => ws.close();
  }

  private close() {
    clearTimeout(this.timer);
    const ws = this.ws;
    this.ws = null;
    if (ws) { ws.onclose = null; ws.close(); }
  }
}

export const realtime = new Realtime();

export function useRealtime(topic: string | null, handler: Handler) {
  const ref = useRef(handler);
  ref.current = handler;
  useEffect(() => {
    if (!topic) return;
    return realtime.subscribe(topic, (d) => ref.current(d));
  }, [topic]);
}
