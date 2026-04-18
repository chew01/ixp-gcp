import { useEffect, useRef, useCallback } from "react";
import type { WSEvent, EventType } from "../types";

type Handler<P = unknown> = (event: WSEvent<P>) => void;
type Handlers = Partial<Record<EventType, Handler<never>>>;

/**
 * Opens a WebSocket to /ws and dispatches events by type to registered
 * handlers. Automatically reconnects after a brief delay on close/error.
 */
export function useWebSocket(handlers: Handlers) {
  const handlersRef = useRef(handlers);
  handlersRef.current = handlers;

  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const wsRef = useRef<WebSocket | null>(null);

  const connect = useCallback(() => {
    const proto = window.location.protocol === "https:" ? "wss" : "ws";
    const base = import.meta.env.BASE_URL;
    const url = `${proto}://${window.location.host}${base}ws`;
    const ws = new WebSocket(url);
    wsRef.current = ws;

    ws.onmessage = (ev) => {
      try {
        const event = JSON.parse(ev.data) as WSEvent<never>;
        const handler = handlersRef.current[event.type];
        if (handler) handler(event);
      } catch {
        // ignore malformed frames
      }
    };

    ws.onclose = () => {
      reconnectTimer.current = setTimeout(connect, 2000);
    };

    ws.onerror = () => {
      ws.close();
    };
  }, []);

  useEffect(() => {
    connect();
    return () => {
      if (reconnectTimer.current) clearTimeout(reconnectTimer.current);
      wsRef.current?.close();
    };
  }, [connect]);
}
