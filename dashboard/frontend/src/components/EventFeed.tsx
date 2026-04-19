import { memo, useState, useEffect, useRef } from "react";
import type { FeedEntry } from "../types";

interface EventFeedProps {
  entries: FeedEntry[];
}

type Tab = "auction" | "atomix";

const TAB_CATEGORIES: Record<Tab, Array<FeedEntry["category"]>> = {
  auction: ["auction", "bid"],
  atomix: ["atomix"],
};

const TAB_LABELS: Record<Tab, string> = {
  auction: "Auctions & Bids",
  atomix: "Atomix",
};

export const EventFeed = memo(({ entries }: EventFeedProps) => {
  const [activeTab, setActiveTab] = useState<Tab>("auction");
  const [unread, setUnread] = useState<Record<Tab, number>>({ auction: 0, atomix: 0 });
  const prevLengthRef = useRef<Record<Tab, number>>({ auction: 0, atomix: 0 });

  // Count new entries on inactive tabs.
  useEffect(() => {
    const tabs: Tab[] = ["auction", "atomix"];
    tabs.forEach((tab) => {
      const cats = TAB_CATEGORIES[tab];
      const count = entries.filter((e) => cats.includes(e.category)).length;
      const prev = prevLengthRef.current[tab];
      if (tab !== activeTab && count > prev) {
        setUnread((u) => ({ ...u, [tab]: u[tab] + (count - prev) }));
      }
      prevLengthRef.current[tab] = count;
    });
  }, [entries, activeTab]);

  function switchTab(tab: Tab) {
    setActiveTab(tab);
    setUnread((u) => ({ ...u, [tab]: 0 }));
    // Sync the current count baseline when switching.
    const cats = TAB_CATEGORIES[tab];
    prevLengthRef.current[tab] = entries.filter((e) => cats.includes(e.category)).length;
  }

  const filtered = entries.filter((e) => TAB_CATEGORIES[activeTab].includes(e.category));

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
      {/* Tab bar */}
      <div
        style={{
          display: "flex",
          borderBottom: "1px solid #30363d",
          background: "#161b22",
          flexShrink: 0,
        }}
      >
        {(["auction", "atomix"] as Tab[]).map((tab) => {
          const isActive = tab === activeTab;
          const badge = unread[tab];
          return (
            <button
              key={tab}
              onClick={() => switchTab(tab)}
              style={{
                flex: 1,
                padding: "8px 10px",
                background: "none",
                border: "none",
                borderBottom: `2px solid ${isActive ? "#58a6ff" : "transparent"}`,
                color: isActive ? "#e6edf3" : "#8b949e",
                fontSize: 11,
                fontFamily: "'JetBrains Mono', monospace",
                fontWeight: isActive ? 700 : 400,
                letterSpacing: "0.04em",
                cursor: "pointer",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                gap: 6,
                transition: "color 0.15s",
              }}
            >
              {TAB_LABELS[tab]}
              {badge > 0 && (
                <span
                  style={{
                    background: "#f85149",
                    color: "#fff",
                    borderRadius: 10,
                    fontSize: 9,
                    fontWeight: 700,
                    padding: "1px 5px",
                    lineHeight: 1.4,
                  }}
                >
                  {badge > 99 ? "99+" : badge}
                </span>
              )}
            </button>
          );
        })}
      </div>

      {/* Entry list */}
      <div
        style={{
          flex: 1,
          overflowY: "auto",
          padding: "4px 0",
        }}
      >
        {filtered.length === 0 && (
          <div
            style={{
              color: "#484f58",
              fontSize: 12,
              padding: "16px",
              textAlign: "center",
            }}
          >
            Waiting for events…
          </div>
        )}
        {filtered.map((entry) => (
          <div
            key={entry.id}
            style={{
              display: "flex",
              alignItems: "flex-start",
              gap: 8,
              padding: "6px 14px",
              borderBottom: "1px solid #21262d",
            }}
          >
            <span style={{ fontSize: 14, flexShrink: 0, marginTop: 1 }}>
              {entry.icon}
            </span>
            <span
              style={{
                color: "#484f58",
                fontSize: 11,
                flexShrink: 0,
                marginTop: 2,
                whiteSpace: "nowrap",
              }}
            >
              {entry.time}
            </span>
            <span style={{ color: entry.color, fontSize: 13, wordBreak: "break-word", whiteSpace: "pre-wrap" }}>
              {entry.text}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
});

EventFeed.displayName = "EventFeed";
