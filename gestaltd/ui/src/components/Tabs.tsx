"use client";

import { useState } from "react";

interface TabItem {
  key: string;
  label: string;
  content: React.ReactNode;
}

export default function Tabs({ items }: { items: TabItem[] }) {
  const [active, setActive] = useState(items[0]?.key ?? "");

  return (
    <div>
      <div className="flex gap-1 border-b border-alpha">
        {items.map((item) => (
          <button
            key={item.key}
            onClick={() => setActive(item.key)}
            className={`px-4 py-2 text-sm font-medium transition-colors duration-150 border-b-2 -mb-px ${
              active === item.key
                ? "border-gold-600 text-primary dark:border-gold-300"
                : "border-transparent text-muted hover:text-primary"
            }`}
          >
            {item.label}
          </button>
        ))}
      </div>
      <div className="pt-5">
        {items.find((item) => item.key === active)?.content}
      </div>
    </div>
  );
}
