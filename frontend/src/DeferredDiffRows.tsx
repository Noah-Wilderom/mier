import { useEffect, useRef, useState, type ReactNode } from "react";

// Keep comment anchors mounted after first exposure. Large files initially
// build only nearby rows; native content visibility skips offscreen layout.
export function DeferredDiffRows({
  children,
  rows,
  force = false,
}: {
  children: () => ReactNode;
  rows: number;
  force?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null),
    [seen, setSeen] = useState(force);
  useEffect(() => {
    if (seen || force || !ref.current) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setSeen(true);
          observer.disconnect();
        }
      },
      { root: ref.current.closest(".review-diff-scroll"), rootMargin: "900px" },
    );
    observer.observe(ref.current);
    return () => observer.disconnect();
  }, [seen, force]);
  return (
    <div
      ref={ref}
      className="review-diff-chunk"
      style={seen || force ? undefined : { height: rows * 26 }}
    >
      {seen || force ? children() : null}
    </div>
  );
}
export function diffChunks<T>(rows: T[], size = 60): T[][] {
  const chunks: T[][] = [];
  for (let i = 0; i < rows.length; i += size)
    chunks.push(rows.slice(i, i + size));
  return chunks;
}
