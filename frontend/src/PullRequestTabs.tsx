import { useEffect, useRef } from "react";
import type { PullRequest } from "./data";
import type { PullRequestTab } from "./pullRequestTabs";
import { Icon } from "./Icon";
import "./PullRequestTabs.css";

export function PullRequestTabs({
  tabs,
  active,
  prs,
  onSelect,
  onClose,
}: {
  tabs: PullRequestTab[];
  active: number;
  prs: PullRequest[];
  onSelect: (number: number) => void;
  onClose: (number: number) => void;
}) {
  const list = useRef<HTMLDivElement>(null);
  const returnFocus = useRef(false);
  useEffect(() => {
    const selected = list.current?.querySelector<HTMLElement>(
      '[role="tab"][aria-selected="true"]',
    );
    selected
      ?.closest(".pr-open-tab")
      ?.scrollIntoView({ block: "nearest", inline: "nearest" });
    if (returnFocus.current)
      (
        selected ??
        document.querySelector<HTMLElement>(".inbox-pane .pr-list-item")
      )?.focus({ preventScroll: true });
    returnFocus.current = false;
  }, [active, tabs.length]);
  if (!tabs.length) return null;
  const close = (number: number) => {
    returnFocus.current = !!list.current?.contains(document.activeElement);
    onClose(number);
  };
  return (
    <div
      className="pr-open-tabs"
      ref={list}
      role="tablist"
      aria-label="Open pull requests"
    >
      {tabs.map((tab, index) => {
        const pr = prs.find((item) => item.number === tab.number);
        const title = pr?.title ?? "Loading pull request…";
        return (
          <div
            className={`pr-open-tab${active === tab.number ? " is-active" : ""}`}
            role="presentation"
            key={tab.number}
          >
            <button
              type="button"
              role="tab"
              id={`open-pr-tab-${tab.number}`}
              aria-controls="open-pr-panel"
              aria-selected={active === tab.number}
              tabIndex={active === tab.number ? 0 : -1}
              aria-label={`Pull request #${tab.number}: ${title}`}
              title={`#${tab.number} · ${title}`}
              onClick={() => {
                returnFocus.current = true;
                onSelect(tab.number);
              }}
              onAuxClick={(event) => {
                if (event.button === 1) {
                  event.preventDefault();
                  close(tab.number);
                }
              }}
              onKeyDown={(event) => {
                if (event.key === "Delete") {
                  event.preventDefault();
                  close(tab.number);
                  return;
                }
                const next =
                  event.key === "ArrowRight"
                    ? (index + 1) % tabs.length
                    : event.key === "ArrowLeft"
                      ? (index + tabs.length - 1) % tabs.length
                      : event.key === "Home"
                        ? 0
                        : event.key === "End"
                          ? tabs.length - 1
                          : undefined;
                if (
                  next === undefined ||
                  event.altKey ||
                  event.ctrlKey ||
                  event.metaKey
                )
                  return;
                event.preventDefault();
                const nextTab = list.current?.querySelector<HTMLElement>(
                  `#open-pr-tab-${tabs[next].number}`,
                );
                nextTab
                  ?.closest(".pr-open-tab")
                  ?.scrollIntoView({ block: "nearest", inline: "nearest" });
                nextTab?.focus({ preventScroll: true });
                if (tabs[next].number === active) return;
                returnFocus.current = true;
                onSelect(tabs[next].number);
              }}
            >
              <Icon
                name={
                  pr?.status === "merged"
                    ? "git-merge"
                    : pr?.status === "closed"
                      ? "close"
                      : "pull-request"
                }
                size={13}
                className={`state-${pr?.status ?? "open"}`}
              />
              <span className="pr-open-tab-number">#{tab.number}</span>
              <span className="pr-open-tab-title">{title}</span>
              {!!pr?.pendingComments.length && (
                <span
                  className="pr-open-tab-drafts"
                  title={`${pr.pendingComments.length} pending comments`}
                >
                  {pr.pendingComments.length}
                </span>
              )}
            </button>
            <button
              type="button"
              className="pr-open-tab-close"
              aria-label={`Close pull request #${tab.number}`}
              title="Close tab (Delete)"
              tabIndex={active === tab.number ? 0 : -1}
              onClick={() => close(tab.number)}
            >
              <Icon name="close" size={12} />
            </button>
          </div>
        );
      })}
    </div>
  );
}
