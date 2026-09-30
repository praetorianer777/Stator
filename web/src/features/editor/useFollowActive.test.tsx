import { afterEach, describe, expect, it } from "vitest";
import { cleanup, createEvent, fireEvent, render, screen } from "@testing-library/react";
import { useFollowActive } from "./useFollowActive";

afterEach(cleanup);

function List() {
  const follow = useFollowActive(0);
  return (
    <div ref={follow} role="listbox" aria-label="Choices" className="overflow-y-hidden">
      <div role="option" aria-selected tabIndex={-1}>
        One
      </div>
      <div role="option" aria-selected={false} tabIndex={-1}>
        Two
      </div>
    </div>
  );
}

// jsdom lays nothing out, so scrollTop is given a plain value to move.
function list(): HTMLElement {
  const el = screen.getByRole("listbox");
  Object.defineProperty(el, "scrollTop", { value: 0, writable: true });
  return el;
}

function touch(kind: "touchStart" | "touchMove", el: HTMLElement, ...ys: number[]): Event {
  const event = createEvent[kind](el);
  Object.defineProperty(event, "touches", { value: ys.map((clientY) => ({ clientY })) });
  fireEvent(el, event);
  return event;
}

describe("useFollowActive", () => {
  it("scrolls a clipped list with a swipe and keeps the page behind still", () => {
    render(<List />);
    const el = list();
    expect(touch("touchStart", el, 200).defaultPrevented).toBe(false);
    expect(touch("touchMove", el, 170).defaultPrevented).toBe(true);
    expect(el.scrollTop).toBe(30);
    touch("touchMove", el, 150);
    expect(el.scrollTop).toBe(50);
    touch("touchMove", el, 190);
    expect(el.scrollTop).toBe(10);
  });

  it("leaves a two-finger gesture to the browser", () => {
    render(<List />);
    const el = list();
    touch("touchStart", el, 200, 300);
    expect(touch("touchMove", el, 150, 350).defaultPrevented).toBe(false);
    expect(el.scrollTop).toBe(0);
  });

  it("scrolls with the wheel and keeps the page behind still", () => {
    render(<List />);
    const el = list();
    const wheel = createEvent.wheel(el, { deltaY: 40 });
    fireEvent(el, wheel);
    expect(wheel.defaultPrevented).toBe(true);
    expect(el.scrollTop).toBe(40);
  });
});
