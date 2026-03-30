import { memo } from "react";
import type { FeedEntry } from "../types";

interface EventFeedProps {
  entries: FeedEntry[];
}

export const EventFeed = memo(({ entries }: EventFeedProps) => {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        overflow: "hidden",
        fontFamily: "'JetBrains Mono', monospace",
      }}
    >
      <div
        style={{
          padding: "8px 12px",
          borderBottom: "1px solid #30363d",
          color: "#8b949e",
          fontSize: 11,
          fontWeight: 600,
          letterSpacing: "0.05em",
          textTransform: "uppercase",
        }}
      >
        Event Feed
      </div>
      <div
        style={{
          flex: 1,
          overflowY: "auto",
          padding: "4px 0",
        }}
      >
        {entries.length === 0 && (
          <div
            style={{
              color: "#484f58",
              fontSize: 11,
              padding: "12px",
              textAlign: "center",
            }}
          >
            Waiting for events…
          </div>
        )}
        {entries.map((entry) => (
          <div
            key={entry.id}
            style={{
              display: "flex",
              alignItems: "flex-start",
              gap: 8,
              padding: "4px 12px",
              borderBottom: "1px solid #21262d",
            }}
          >
            <span style={{ fontSize: 12, flexShrink: 0, marginTop: 1 }}>
              {entry.icon}
            </span>
            <span
              style={{
                color: "#484f58",
                fontSize: 9,
                flexShrink: 0,
                marginTop: 2,
                whiteSpace: "nowrap",
              }}
            >
              {entry.time}
            </span>
            <span style={{ color: entry.color, fontSize: 10, wordBreak: "break-all" }}>
              {entry.text}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
});

EventFeed.displayName = "EventFeed";
